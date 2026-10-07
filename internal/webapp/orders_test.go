package webapp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

type nopNotify struct{ pay int }

func (n *nopNotify) PayScreen(context.Context, orders.Order, string) error { n.pay++; return nil }
func (*nopNotify) PostCard(context.Context, orders.Order) (int, error)     { return 1, nil }
func (*nopNotify) UpdateCard(context.Context, orders.Order) error          { return nil }
func (*nopNotify) Customer(context.Context, orders.Order) error            { return nil }

func withOrders(t *testing.T, f *vpnctl.Fake, d *Deps) *nopNotify {
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	n := &nopNotify{}
	d.Orders = &orders.Service{DB: st.DB(), Chats: st, API: f, PayMode: config.PayMixed, Manual: true, Details: "Карта",
		TTL: time.Hour, Notify: n, Now: time.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
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
	if code != 200 || !strings.Contains(out, `"to_pay":300`) {
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
	if code != 201 || created.Order.Status != "awaiting_receipt" || created.Details != "Карта" {
		t.Fatalf("create %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders/"+created.Order.ID+"/receipt", tma(77), ""); code != 204 || n.pay != 1 {
		t.Fatalf("receipt %d pay screens %d", code, n.pay)
	}
	code, out = do(t, d, "GET", "/bot/api/orders", tma(77), "")
	if code != 200 || !strings.Contains(out, `"total":1`) {
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
	d.Orders.Manual = false
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
