package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

type nopNotify struct{ pay int }

func (n *nopNotify) PayScreen(context.Context, orders.Order, string) error { n.pay++; return nil }
func (*nopNotify) Card(context.Context, orders.Order) error                { return nil }
func (*nopNotify) Customer(context.Context, orders.Order) error            { return nil }

func withOrders(t *testing.T, f *vpnctl.Fake, d *Deps) *nopNotify {
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	n := &nopNotify{}
	d.Orders = &orders.Service{DB: st.DB(), Chats: st, API: f, Notify: n, Now: time.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.Orders.SetLive(orders.Live{PayMode: config.PayMixed, Manual: true, Details: "Карта", TTL: time.Hour})
	return n
}

func do(t *testing.T, d *Deps, method, path, auth, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// newOrder creates an order for customer 77 and returns its id.
func newOrder(t *testing.T, d *Deps, body string) string {
	t.Helper()
	code, out := do(t, d, "POST", "/bot/api/orders", tma(77), body)
	var r struct {
		Order struct{ ID string } `json:"order"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || code != 201 || r.Order.ID == "" {
		t.Fatalf("create %d %s (%v)", code, out, err)
	}
	return r.Order.ID
}

func ordersFake() *vpnctl.Fake {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Subscriptions: []vpnctl.Subscription{{ID: 3, Title: "Дом"}}})
	f.Add(vpnctl.Customer{ID: 6, Name: "Bob", TelegramID: ptr(int64(88))})
	f.Quotes = map[string]vpnctl.ExtendQuote{"subscription:3": {CustomerID: 5, Amount: 300}}
	return f
}

func TestOrdersFlow(t *testing.T) {
	f := ordersFake()
	d := app(f)
	n := withOrders(t, f, d)
	body := `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1}`
	code, out := do(t, d, "POST", "/bot/api/orders/quote", tma(77), body)
	const title = `"title":"Renewal of “Дом” · 1 mo."` // the web API reads stored titles in English
	if code != 200 || !strings.Contains(out, `"to_pay":300`) || !strings.Contains(out, title) {
		t.Fatalf("quote %d %s", code, out)
	}
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), body)
	var created struct {
		Order struct {
			ID     string
			Status string
		} `json:"order"`
		Details string `json:"details"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create body %q: %v", out, err)
	}
	if code != 201 || created.Order.Status != "awaiting_receipt" || created.Details != "Карта" || !strings.Contains(out, title) {
		t.Fatalf("create %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders/"+created.Order.ID+"/receipt", tma(77), ""); code != 204 || n.pay != 1 {
		t.Fatalf("receipt %d pay screens %d", code, n.pay)
	}
	code, out = do(t, d, "GET", "/bot/api/orders", tma(77), "")
	if code != 200 || !strings.Contains(out, `"total":1`) || !strings.Contains(out, title) {
		t.Fatalf("list %d %s", code, out)
	}
}

func TestOrdersOwnership(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	id := newOrder(t, d, `{"kind":"topup","amount":100}`)
	for _, p := range []string{"/bot/api/orders/" + id + "/receipt", "/bot/api/orders/" + id + "/cancel", "/bot/api/orders/" + id + "/pay-balance", "/bot/api/orders/1/cancel"} {
		if code, _ := do(t, d, "POST", p, tma(88), ""); code != 404 {
			t.Errorf("%s by another customer: %d", p, code)
		}
	}
	if code, out := do(t, d, "GET", "/bot/api/orders", tma(88), ""); !strings.Contains(out, `"total":0`) {
		t.Errorf("foreign list %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":99,"months":1}`); code != 400 {
		t.Errorf("foreign item: %d", code)
	}
}

func TestOrdersUnavailableWithoutService(t *testing.T) {
	d := app(ordersFake())
	if code, _ := do(t, d, "GET", "/bot/api/orders", tma(77), ""); code != 503 {
		t.Errorf("no service: %d", code)
	}
}

func balanceFake() *vpnctl.Fake {
	f := ordersFake()
	f.Add(vpnctl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Balance: 400, BalanceOwn: 400,
		Subscriptions: []vpnctl.Subscription{{ID: 3, Title: "Дом"}}})
	return f
}

const renewBody = `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1}`

func TestOrdersPayBalance(t *testing.T) {
	f := balanceFake()
	d := app(f)
	withOrders(t, f, d)
	code, out := do(t, d, "POST", "/bot/api/orders", tma(77), renewBody)
	if code != 201 || !strings.Contains(out, `"method":"balance"`) {
		t.Fatalf("create %d %s", code, out)
	}
	var created struct {
		Order struct{ ID string } `json:"order"`
	}
	json.Unmarshal([]byte(out), &created)
	id := created.Order.ID
	code, out = do(t, d, "POST", "/bot/api/orders/"+id+"/pay-balance", tma(77), "")
	if code != 200 || !strings.Contains(out, `"status":"approved"`) {
		t.Fatalf("pay %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders/"+id+"/pay-balance", tma(77), ""); code != 409 {
		t.Errorf("second pay: %d", code)
	}
}

func TestOrdersPayBalanceFailureHidesPanelError(t *testing.T) {
	f := balanceFake()
	f.FailExtend = "remnawave exploded: secret-host"
	d := app(f)
	withOrders(t, f, d)
	id := newOrder(t, d, renewBody)
	code, out := do(t, d, "POST", "/bot/api/orders/"+id+"/pay-balance", tma(77), "")
	if code != 200 || !strings.Contains(out, `"status":"failed"`) {
		t.Fatalf("pay %d %s", code, out)
	}
	if strings.Contains(out, "remnawave") || strings.Contains(out, "secret-host") || strings.Contains(out, `"error"`) {
		t.Errorf("panel error leaked: %s", out)
	}
}

func TestOrdersCancel(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	id := newOrder(t, d, renewBody)
	if code, out := do(t, d, "POST", "/bot/api/orders/"+id+"/cancel", tma(77), ""); code != 204 {
		t.Fatalf("cancel %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders/"+id+"/cancel", tma(77), ""); code != 409 {
		t.Errorf("second cancel: %d", code)
	}
}

func TestOrdersCreateManualOff(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	d.Orders.UpdateLive(func(l *orders.Live) { l.Manual = false })
	code, out := do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100}`)
	if code != 409 || !strings.Contains(out, `"code":"manual_off"`) {
		t.Fatalf("manual off: %d %s", code, out)
	}
}

func TestOrdersMalformedBody(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	for _, p := range []string{"/bot/api/orders", "/bot/api/orders/quote"} {
		if code, out := do(t, d, "POST", p, tma(77), `{"kind":`); code != 400 || !strings.Contains(out, `"code":"bad_request"`) {
			t.Errorf("%s malformed: %d %s", p, code, out)
		}
	}
}

func TestOrdersListPagination(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	var ids []string
	for i := 0; i < 21; i++ {
		id := newOrder(t, d, `{"kind":"topup","amount":100}`)
		ids = append(ids, id)
		if code, _ := do(t, d, "POST", "/bot/api/orders/"+id+"/receipt", tma(77), ""); code != 204 {
			t.Fatalf("receipt %d: %d", i, code)
		}
	}
	list := func(q string) (int, []struct{ ID string }) {
		t.Helper()
		code, out := do(t, d, "GET", "/bot/api/orders"+q, tma(77), "")
		var r struct {
			Orders []struct{ ID string } `json:"orders"`
			Total  int                   `json:"total"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil || code != 200 || r.Total != 21 || r.Orders == nil {
			t.Fatalf("list %q: %d %s (%v)", q, code, out, err)
		}
		return code, r.Orders
	}
	if _, got := list(""); len(got) != 20 || got[0].ID != ids[20] {
		t.Errorf("page 0: %+v", got)
	}
	if _, got := list("?page=1"); len(got) != 1 || got[0].ID != ids[0] {
		t.Errorf("page 1: %+v", got)
	}
	if _, got := list("?page=5"); len(got) != 0 {
		t.Errorf("page past the end: %+v", got)
	}
}

// withProvider attaches a fake provider to the service.
func withProvider(d *Deps) *payments.Fake {
	fp := &payments.Fake{Name: "fake"}
	d.Orders.UpdateLive(func(l *orders.Live) { l.Providers = payments.NewRegistry(fp) })
	return fp
}

func hook(t *testing.T, d *Deps, provider, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/pay/"+provider, strings.NewReader(body))
	req.Header.Set("X-Test-Signature", "ok")
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestCreateOrderWithProvider(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)

	code, out := do(t, d, "POST", "/bot/api/orders/quote", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1}`)
	if code != 200 || !strings.Contains(out, `"methods":["manual","fake"]`) || !strings.Contains(out, `"titles":{"fake":"Fake pay"}`) {
		t.Fatalf("quote %d %s", code, out)
	}
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1,"method":"fake"}`)
	if code != 201 || !strings.Contains(out, `"status":"awaiting_payment"`) || !strings.Contains(out, `"method":"fake"`) || !strings.Contains(out, `"pay_url":"https://pay.test/`) {
		t.Fatalf("create %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100,"method":"nope"}`); code != 400 {
		t.Errorf("unknown method: %d", code)
	}
	fp.Err = errors.New("provider is down")
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100,"method":"fake"}`)
	if code != 424 || !strings.Contains(out, `"provider_failed"`) || strings.Contains(out, "down") {
		t.Errorf("provider failure: %d %s", code, out)
	}
}

func TestPayWebhookStatusCodes(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)
	id := newOrder(t, d, `{"kind":"topup","amount":100,"method":"fake"}`)
	ev := payments.Event{Provider: "fake", OrderID: id, ExternalID: "ext-1", Paid: true, Amount: 100, Currency: "RUB"}
	fp.Hook = func(h http.Header, body []byte) (payments.Event, bool, error) {
		switch string(body) {
		case "forged":
			return payments.Event{}, false, payments.ErrBadSignature
		case "garbage":
			return payments.Event{}, false, payments.ErrBadBody
		case "ignore":
			return payments.Event{}, false, nil
		case "boom":
			return payments.Event{}, false, errors.New("provider api down")
		case "stranger":
			e := ev
			e.OrderID = "00000000-0000-4000-8000-000000000000"
			return e, true, nil
		}
		if h.Get("X-Test-Signature") != "ok" {
			t.Errorf("headers were not passed on: %v", h)
		}
		return ev, true, nil
	}
	for body, want := range map[string]int{"forged": 401, "garbage": 400, "ignore": 200, "boom": 500, "stranger": 200} {
		if code, _ := hook(t, d, "fake", body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	if code, _ := hook(t, d, "nope", "x"); code != 404 {
		t.Errorf("unconfigured provider: %d", code)
	}
	if code, _ := hook(t, d, "fake", strings.Repeat("x", 70<<10)); code != 413 {
		t.Errorf("oversized body: %d", code)
	}
	if code, _ := hook(t, d, "fake", "paid"); code != 200 {
		t.Fatalf("paid: %d", code)
	}
	if cur, _ := d.Orders.Get(t.Context(), id); cur.Status != orders.StatusApproved {
		t.Fatalf("order %s", cur.Status)
	}
}

// A body over Fiber's BodyLimit is refused before any handler or middleware
// runs: an anonymous caller cannot make the server buffer megabytes.
func TestBodyLimit(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)
	reached := 0
	fp.Hook = func(http.Header, []byte) (payments.Event, bool, error) {
		reached++
		return payments.Event{}, false, nil
	}
	huge := strings.Repeat("x", 2<<20)
	// fasthttp refuses the request while reading it; Fiber's Test reports
	// that as an error, a live server answers 413.
	refused := func(path string) {
		t.Helper()
		resp, err := New(*d).Test(httptest.NewRequest("POST", path, strings.NewReader(huge)), -1)
		switch {
		case err != nil && strings.Contains(err.Error(), "body size exceeds"):
		case err == nil && resp.StatusCode == 413:
		default:
			t.Errorf("%s: 2 MiB body not refused: %v %v", path, resp, err)
		}
	}
	refused("/pay/fake")
	if reached != 0 {
		t.Errorf("the provider saw an oversized webhook")
	}
	// Past the limit even an unauthenticated API call is cut off at the door
	// (without the limit it would get as far as the 401 of the auth check).
	refused("/bot/api/orders")
	if code, _ := hook(t, d, "fake", "small"); code != 200 || reached != 1 {
		t.Errorf("a normal webhook still works: %d, reached %d", code, reached)
	}
}

func TestPayWebhookDuplicate(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)
	id := newOrder(t, d, `{"kind":"topup","amount":100,"method":"fake"}`)
	fp.Hook = func(http.Header, []byte) (payments.Event, bool, error) {
		return payments.Event{Provider: "fake", OrderID: id, ExternalID: "ext-1", Paid: true, Amount: 100, Currency: "RUB"}, true, nil
	}
	for i := range 3 {
		if code, _ := hook(t, d, "fake", "x"); code != 200 {
			t.Fatalf("delivery %d: %d (providers keep retrying anything but 200)", i, code)
		}
	}
	if len(f.Payments) != 1 {
		t.Fatalf("the panel got %d payments for one order", len(f.Payments))
	}
}

func TestPayWebhookNeedsOrders(t *testing.T) {
	d := app(ordersFake()) // no order service
	if code, _ := hook(t, d, "fake", "x"); code != 404 {
		t.Errorf("%d", code)
	}
}

// The same stored order is rendered in the language of whoever asks.
func TestOrderTextFollowsRequestLanguage(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	newOrder(t, d, `{"kind":"topup","amount":100,"method":"manual"}`)
	for lang, want := range map[string]string{"ru": "Пополнение баланса", "en": "Balance top-up", "": "Balance top-up", "xx": "Balance top-up"} {
		code, out := do(t, d, "GET", "/bot/api/orders", tmaLang(77, lang), "")
		if code != 200 || !strings.Contains(out, `"title":"`+want+`"`) {
			t.Errorf("list %q: %d %s", lang, code, out)
		}
		code, out = do(t, d, "POST", "/bot/api/orders/quote", tmaLang(77, lang), `{"kind":"topup","amount":100}`)
		if code != 200 || !strings.Contains(out, `"title":"`+want+`"`) {
			t.Errorf("quote %q: %d %s", lang, code, out)
		}
	}
}

func TestAdminOrderTextFollowsRequestLanguage(t *testing.T) {
	f := ordersFake()
	d := app(f)
	d.IsAdmin = func(int64) bool { return true }
	withOrders(t, f, d)
	newOrder(t, d, `{"kind":"topup","amount":100,"method":"manual"}`)
	_, out := do(t, d, "GET", "/bot/api/admin/orders", tmaLang(1, "ru"), "")
	if !strings.Contains(out, "Пополнение баланса") {
		t.Errorf("ru: %s", out)
	}
	_, out = do(t, d, "GET", "/bot/api/admin/orders", tmaLang(1, "en"), "")
	if !strings.Contains(out, "Balance top-up") {
		t.Errorf("en: %s", out)
	}
}

func TestProviderLabelsFollowRequestLanguage(t *testing.T) {
	for lang, want := range map[string]string{"ru": "Карта / СБП", "en": "Card / bank transfer"} {
		got := quoteTitles(i18n.Default.For(lang), map[string]string{"yookassa": "YooKassa", "custom": "Custom"})
		if got["yookassa"] != want || got["custom"] != "Custom" {
			t.Errorf("%s: %v", lang, got)
		}
	}
}

// The invoice the payer sees is titled in the payer's language, not always English.
func TestInvoiceTitleFollowsPayerLanguage(t *testing.T) {
	for lang, want := range map[string]string{"ru": "Пополнение баланса", "en": "Balance top-up", "": "Balance top-up", "xx": "Balance top-up"} {
		f := ordersFake()
		d := app(f)
		withOrders(t, f, d)
		fp := withProvider(d)
		code, out := do(t, d, "POST", "/bot/api/orders", tmaLang(77, lang), `{"kind":"topup","amount":100,"method":"fake"}`)
		if code != 201 || len(fp.Invoices) != 1 {
			t.Fatalf("%q: create %d %s", lang, code, out)
		}
		if got := fp.Invoices[0].Title; got != want {
			t.Errorf("%q: invoice title %q, want %q", lang, got, want)
		}
	}
}
