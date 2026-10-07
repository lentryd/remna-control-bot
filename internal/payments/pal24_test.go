package payments

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const p24Order = "0b5f1c1e-6a53-4f0e-9b6e-0d9a3b7c1a11"

func p24Server(t *testing.T, h http.HandlerFunc) *Pal24 {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Pal24{APIToken: "api-token", ShopID: "shop42", BaseURL: srv.URL}
}

// p24Sig is the documented postback signature: UPPER(hex(md5("OutSum:InvId:token"))).
func p24Sig(outSum, invID, token string) string {
	sum := md5.Sum([]byte(outSum + ":" + invID + ":" + token))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func p24Form(kv map[string]string) []byte {
	v := url.Values{}
	for k, s := range kv {
		v.Set(k, s)
	}
	return []byte(v.Encode())
}

func p24Post(t *testing.T, p *Pal24, kv map[string]string) (Event, bool, error) {
	t.Helper()
	return p.Webhook(context.Background(), http.Header{}, p24Form(kv))
}

func p24Fields(status, token string) map[string]string {
	return map[string]string{
		"InvId": p24Order, "OutSum": "800.00", "Status": status, "CurrencyIn": "usd", "TrsId": "trs-1", "bill_id": "BILL1",
		"SignatureValue": p24Sig("800.00", p24Order, token),
	}
}

func TestPal24Identity(t *testing.T) {
	p := &Pal24{}
	if p.ID() != "pal24" || p.Title() == "" {
		t.Errorf("id=%q title=%q", p.ID(), p.Title())
	}
}

func TestPal24Invoice(t *testing.T) {
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/bill/create" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer api-token" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers auth=%q accept=%q", r.Header.Get("Authorization"), r.Header.Get("Accept"))
		}
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if b["amount"] != "800.00" || b["shop_id"] != "shop42" || b["order_id"] != p24Order ||
			b["type"] != "normal" || b["currency_in"] != "USD" || b["ttl"] != float64(3600) {
			t.Errorf("body %v", b)
		}
		if d, _ := b["description"].(string); len([]rune(d)) != 255 {
			t.Errorf("description has %d runes", len([]rune(d)))
		}
		w.Write([]byte(`{"success":true,"bill_id":"BILL1","link_url":"https://pal24.pro/pay/1","link_page_url":"https://pal24.pro/page/1"}`))
	})
	inv, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Title: strings.Repeat("я", 400),
		Amount: 800, Currency: "USD", ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExternalID != "BILL1" || inv.URL != "https://pal24.pro/page/1" || inv.Amount != 800 || inv.Currency != "USD" {
		t.Errorf("invoice %+v", inv)
	}
}

func TestPal24InvoiceTTLOmittedAndCapped(t *testing.T) {
	var ttl any
	var has bool
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		ttl, has = b["ttl"]
		w.Write([]byte(`{"success":true,"bill_id":"B","link_url":"https://x/y"}`))
	})
	if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 1, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	if has {
		t.Errorf("ttl sent without ExpiresIn: %v", ttl)
	}
	if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 1, Currency: "RUB", ExpiresIn: 90 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if ttl != float64(30*24*3600) {
		t.Errorf("ttl %v not capped at 30 days", ttl)
	}
}

func TestPal24InvoiceURLAndSuccessForms(t *testing.T) {
	for name, tc := range map[string]struct{ body, url string }{
		"string true": {`{"success":"true","bill_id":"B","link_url":"https://pal24.pro/pay/1"}`, "https://pal24.pro/pay/1"},
		"bool true":   {`{"success":true,"bill_id":"B","link_url":"https://a","link_page_url":"https://b"}`, "https://b"},
	} {
		p := p24Server(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) })
		inv, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 5, Currency: "RUB"})
		if err != nil || inv.URL != tc.url {
			t.Errorf("%s: %+v %v", name, inv, err)
		}
	}
}

func TestPal24InvoiceBaseURLSlashes(t *testing.T) {
	var path string
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(`{"success":true,"bill_id":"B","link_url":"https://x/y"}`))
	})
	p.BaseURL += "/api/v1/"
	if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 1, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/bill/create" {
		t.Errorf("path %q", path)
	}
}

func TestPal24InvoiceErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"success false":   {200, `{"success":false,"message":"nope"}`},
		"success false s": {200, `{"success":"false","bill_id":"B","link_url":"https://x"}`},
		"no bill id":      {200, `{"success":true,"link_url":"https://x"}`},
		"no link":         {200, `{"success":true,"bill_id":"B"}`},
		"http 500":        {500, `boom`},
		"garbage":         {200, `<html>`},
	} {
		p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 5, Currency: "RUB"}); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if strings.Contains(err.Error(), "api-token") {
			t.Errorf("%s: error leaks the token: %v", name, err)
		}
	}
	var ae *apiError
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 5, Currency: "RUB"}); !errors.As(err, &ae) || ae.Status != 500 {
		t.Errorf("want apiError 500, got %v", err)
	}
}

func TestPal24InvoiceBadCurrencyNoRequest(t *testing.T) {
	called := false
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	for _, c := range []string{"", "usd", "US", "USDT", "U$D"} {
		if _, err := p.Invoice(context.Background(), InvoiceRequest{OrderID: p24Order, Amount: 5, Currency: c}); err == nil {
			t.Errorf("currency %q accepted", c)
		}
	}
	if called {
		t.Error("a request was made for an invalid currency")
	}
}

// p24BillServer is a bill/status endpoint that answers body and counts (and remembers) the calls.
type p24BillAPI struct {
	hits atomic.Int32
	id   atomic.Value // last queried bill id
}

func p24BillServer(t *testing.T, body string) (*Pal24, *p24BillAPI) {
	t.Helper()
	api := &p24BillAPI{}
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
		api.hits.Add(1)
		api.id.Store(r.URL.Query().Get("id"))
		if r.URL.Path != "/bill/status" {
			t.Errorf("path %q", r.URL.Path)
		}
		w.Write([]byte(body))
	})
	return p, api
}

func (a *p24BillAPI) queried() string {
	s, _ := a.id.Load().(string)
	return s
}

const p24PaidBill = `{"success":true,"status":"SUCCESS","amount":"800.00","currency_in":"USD","order_id":"` + p24Order + `"}`

func TestPal24WebhookForm(t *testing.T) {
	p, api := p24BillServer(t, p24PaidBill)
	ev, rel, err := p24Post(t, p, p24Fields("SUCCESS", "api-token"))
	if err != nil || !rel {
		t.Fatalf("rel=%v err=%v", rel, err)
	}
	want := Event{Provider: "pal24", OrderID: p24Order, ExternalID: "BILL1", Paid: true, Amount: 800, Currency: "USD"}
	if ev != want {
		t.Errorf("event %+v, want %+v", ev, want)
	}
	if api.hits.Load() != 1 || api.queried() != "BILL1" {
		t.Errorf("hits=%d id=%q", api.hits.Load(), api.queried())
	}
}

func TestPal24WebhookJSON(t *testing.T) {
	for name, body := range map[string]string{
		"string sum": `{"InvId":"` + p24Order + `","OutSum":"800.00","Status":"SUCCESS","CurrencyIn":"USD","TrsId":"t","bill_id":"BILL1","SignatureValue":"` + p24Sig("800.00", p24Order, "api-token") + `"}`,
		"number sum": `  {"InvId":"` + p24Order + `","OutSum":800.00,"Status":"SUCCESS","CurrencyIn":"USD","bill_id":"BILL1","SignatureValue":"` + p24Sig("800.00", p24Order, "api-token") + `"}`,
	} {
		p, _ := p24BillServer(t, p24PaidBill)
		ev, rel, err := p.Webhook(context.Background(), http.Header{}, []byte(body))
		if err != nil || !rel || !ev.Paid || ev.OrderID != p24Order || ev.ExternalID != "BILL1" || ev.Amount != 800 || ev.Currency != "USD" || ev.Fixed {
			t.Errorf("%s: %+v rel=%v err=%v", name, ev, rel, err)
		}
	}
}

func TestPal24WebhookLowerCaseSignature(t *testing.T) {
	p, _ := p24BillServer(t, p24PaidBill)
	f := p24Fields("SUCCESS", "api-token")
	f["SignatureValue"] = strings.ToLower(f["SignatureValue"])
	if _, rel, err := p24Post(t, p, f); err != nil || !rel {
		t.Errorf("rel=%v err=%v", rel, err)
	}
}

func TestPal24WebhookSignatureToken(t *testing.T) {
	p, _ := p24BillServer(t, p24PaidBill)
	p.SignatureToken = "sig-token"
	if _, rel, err := p24Post(t, p, p24Fields("SUCCESS", "sig-token")); err != nil || !rel {
		t.Errorf("signature token: rel=%v err=%v", rel, err)
	}
	if _, _, err := p24Post(t, p, p24Fields("SUCCESS", "api-token")); !errors.Is(err, ErrBadSignature) {
		t.Errorf("api token must not verify when a signature token is set: %v", err)
	}
}

func TestPal24WebhookBadSignature(t *testing.T) {
	p, api := p24BillServer(t, p24PaidBill)
	wrong := p24Fields("SUCCESS", "other")
	empty := p24Fields("SUCCESS", "api-token")
	empty["SignatureValue"] = ""
	tampered := p24Fields("SUCCESS", "api-token")
	tampered["OutSum"] = "1.00" // old signature for 800.00
	otherOrder := p24Fields("SUCCESS", "api-token")
	otherOrder["InvId"] = "11111111-2222-3333-4444-555555555555"
	for name, f := range map[string]map[string]string{"wrong": wrong, "tampered sum": tampered, "other order": otherOrder} {
		if _, rel, err := p24Post(t, p, f); !errors.Is(err, ErrBadSignature) || rel {
			t.Errorf("%s: rel=%v err=%v", name, rel, err)
		}
	}
	if _, rel, err := p24Post(t, p, empty); !errors.Is(err, ErrBadSignature) || rel {
		t.Errorf("empty signature accepted: rel=%v err=%v", rel, err)
	}
	// No token configured: nothing verifies, not even a signature computed with an empty token.
	none := &Pal24{BaseURL: p.BaseURL}
	if _, rel, err := p24Post(t, none, p24Fields("SUCCESS", "")); !errors.Is(err, ErrBadSignature) || rel {
		t.Errorf("empty token: rel=%v err=%v", rel, err)
	}
	if api.hits.Load() != 0 {
		t.Errorf("an unsigned postback reached the API %d times", api.hits.Load())
	}
}

// The status is not interpreted before the signature is verified: an unsigned FAIL or garbage status
// is a bad signature, not "irrelevant".
func TestPal24WebhookSignatureBeforeStatus(t *testing.T) {
	p := &Pal24{APIToken: "api-token"}
	for _, st := range []string{"SUCCESS", "FAIL", "NEW", "whatever"} {
		f := p24Fields(st, "bad")
		if _, rel, err := p24Post(t, p, f); !errors.Is(err, ErrBadSignature) || rel {
			t.Errorf("status %s: rel=%v err=%v", st, rel, err)
		}
	}
}

func TestPal24WebhookBadBody(t *testing.T) {
	p := &Pal24{APIToken: "api-token"}
	for _, drop := range []string{"InvId", "OutSum", "Status", "SignatureValue"} {
		f := p24Fields("SUCCESS", "api-token")
		delete(f, drop)
		if _, rel, err := p24Post(t, p, f); !errors.Is(err, ErrBadBody) || rel {
			t.Errorf("missing %s: rel=%v err=%v", drop, rel, err)
		}
	}
	f := p24Fields("SUCCESS", "api-token")
	f["OutSum"] = "lots"
	if _, rel, err := p24Post(t, p, f); !errors.Is(err, ErrBadBody) || rel {
		t.Errorf("bad OutSum: rel=%v err=%v", rel, err)
	}
	for _, body := range []string{"", "{not json", "%zz", `{"InvId":[1],"OutSum":{},"Status":null}`} {
		if _, rel, err := p.Webhook(context.Background(), http.Header{}, []byte(body)); !errors.Is(err, ErrBadBody) || rel {
			t.Errorf("body %q: rel=%v err=%v", body, rel, err)
		}
	}
}

// A repeated key makes the body ambiguous (a parser upstream may read another value than this one):
// refused, even when the signature of the first value holds.
func TestPal24WebhookDuplicateKeys(t *testing.T) {
	p, api := p24BillServer(t, p24PaidBill)
	sig := p24Sig("800.00", p24Order, "api-token")
	form := "InvId=" + p24Order + "&OutSum=800.00&Status=SUCCESS&bill_id=BILL1&SignatureValue=" + sig
	for key, value := range map[string]string{
		"InvId": "other", "OutSum": "1.00", "SignatureValue": "AAAA", "Status": "FAIL", "bill_id": "BILL2",
	} {
		if _, rel, err := p.Webhook(context.Background(), http.Header{}, []byte(form+"&"+key+"="+value)); !errors.Is(err, ErrBadBody) || rel {
			t.Errorf("form, duplicate %s: rel=%v err=%v", key, rel, err)
		}
		// The same key twice with the same value is still a duplicate.
		same := map[string]string{"InvId": p24Order, "OutSum": "800.00", "SignatureValue": sig, "Status": "SUCCESS", "bill_id": "BILL1"}[key]
		if _, rel, err := p.Webhook(context.Background(), http.Header{}, []byte(form+"&"+key+"="+same)); !errors.Is(err, ErrBadBody) || rel {
			t.Errorf("form, repeated %s: rel=%v err=%v", key, rel, err)
		}
		js := `{"InvId":"` + p24Order + `","OutSum":"800.00","Status":"SUCCESS","bill_id":"BILL1","SignatureValue":"` + sig + `","` + key + `":"` + value + `"}`
		if _, rel, err := p.Webhook(context.Background(), http.Header{}, []byte(js)); !errors.Is(err, ErrBadBody) || rel {
			t.Errorf("json, duplicate %s: rel=%v err=%v", key, rel, err)
		}
	}
	if api.hits.Load() != 0 {
		t.Errorf("a duplicate-key postback reached the API %d times", api.hits.Load())
	}
}

// The signature does not cover Status, bill_id or CurrencyIn, so the postback's own status decides
// nothing: whoever holds one valid (OutSum, InvId, SignatureValue) triple can rewrite the rest.
func TestPal24WebhookForgedStatus(t *testing.T) {
	for name, apiBody := range map[string]string{
		"bill is NEW":  `{"success":true,"status":"NEW","amount":"800.00","currency_in":"USD","order_id":"` + p24Order + `"}`,
		"bill is FAIL": `{"success":true,"status":"FAIL","order_id":"` + p24Order + `"}`,
	} {
		p, api := p24BillServer(t, apiBody)
		// A signature belonging to a FAIL postback, replayed with the status rewritten.
		f := p24Fields("SUCCESS", "api-token")
		ev, rel, err := p24Post(t, p, f)
		if err != nil || rel || ev.Paid || ev.Review {
			t.Errorf("%s: forged SUCCESS applied: %+v rel=%v err=%v", name, ev, rel, err)
		}
		if api.hits.Load() != 1 {
			t.Errorf("%s: %d API calls", name, api.hits.Load())
		}
	}
}

// The API vouches for the bill, not for the postback: the event carries the API's bill id and order id,
// so the orders layer can reject an event whose external id is not the order's stored bill.
func TestPal24WebhookForgedOtherBill(t *testing.T) {
	const other = "11111111-2222-3333-4444-555555555555"
	p, api := p24BillServer(t, `{"success":true,"status":"SUCCESS","amount":"5.00","currency_in":"RUB","order_id":"`+other+`"}`)
	f := p24Fields("SUCCESS", "api-token")
	f["bill_id"] = "BILL-ATTACKER"
	f["CurrencyIn"] = "USD"
	ev, rel, err := p24Post(t, p, f)
	if err != nil || !rel || !ev.Paid {
		t.Fatalf("%+v rel=%v err=%v", ev, rel, err)
	}
	want := Event{Provider: "pal24", OrderID: other, ExternalID: "BILL-ATTACKER", Paid: true, Amount: 5, Currency: "RUB"}
	if ev != want {
		t.Errorf("event %+v, want %+v (the postback's InvId/OutSum/CurrencyIn must not leak in)", ev, want)
	}
	if api.queried() != "BILL-ATTACKER" {
		t.Errorf("asked about %q", api.queried())
	}
}

func TestPal24WebhookOrderFromPostbackWhenAPIHasNone(t *testing.T) {
	p, _ := p24BillServer(t, `{"success":true,"status":"SUCCESS","amount":"800.00","currency_in":"USD"}`)
	ev, rel, err := p24Post(t, p, p24Fields("SUCCESS", "api-token"))
	if err != nil || !rel || !ev.Paid || ev.OrderID != p24Order || ev.ExternalID != "BILL1" {
		t.Errorf("%+v rel=%v err=%v", ev, rel, err)
	}
}

func TestPal24WebhookFixedFromTheAPIReport(t *testing.T) {
	// The API reports neither amount nor currency: the Status rule applies, the postback's CurrencyIn does not.
	p, _ := p24BillServer(t, `{"success":true,"status":"SUCCESS","order_id":"`+p24Order+`"}`)
	f := p24Fields("SUCCESS", "api-token")
	ev, rel, err := p24Post(t, p, f)
	if err != nil || !rel || !ev.Paid || !ev.Fixed || ev.Currency != "" || ev.Amount != 0 {
		t.Errorf("%+v rel=%v err=%v", ev, rel, err)
	}
	// Omitting CurrencyIn from the postback no longer sets Fixed.
	p, _ = p24BillServer(t, p24PaidBill)
	delete(f, "CurrencyIn")
	ev, rel, err = p24Post(t, p, f)
	if err != nil || !rel || !ev.Paid || ev.Fixed || ev.Currency != "USD" || ev.Amount != 800 {
		t.Errorf("no CurrencyIn: %+v rel=%v err=%v", ev, rel, err)
	}
}

func TestPal24WebhookStatusesViaAPI(t *testing.T) {
	// OVERPAID / UNDERPAID in the postback are only reasons to ask; the API's word is what counts.
	p, _ := p24BillServer(t, `{"success":true,"status":"OVERPAID","amount":"900.00","currency_in":"USD","order_id":"`+p24Order+`"}`)
	ev, rel, err := p24Post(t, p, p24Fields("OVERPAID", "api-token"))
	if err != nil || !rel || !ev.Paid || ev.Review || ev.Amount != 900 {
		t.Errorf("OVERPAID: %+v rel=%v err=%v", ev, rel, err)
	}
	p, _ = p24BillServer(t, `{"success":true,"status":"UNDERPAID","amount":"500.00","currency_in":"USD","order_id":"`+p24Order+`"}`)
	ev, rel, err = p24Post(t, p, p24Fields("UNDERPAID", "api-token"))
	if err != nil || !rel || ev.Paid || !ev.Review || ev.Fixed || ev.Amount != 500 {
		t.Errorf("UNDERPAID: %+v rel=%v err=%v", ev, rel, err)
	}
	// The postback says UNDERPAID, the bill is SUCCESS: the bill wins.
	p, _ = p24BillServer(t, p24PaidBill)
	ev, rel, err = p24Post(t, p, p24Fields("UNDERPAID", "api-token"))
	if err != nil || !rel || !ev.Paid || ev.Review {
		t.Errorf("postback UNDERPAID, bill SUCCESS: %+v rel=%v err=%v", ev, rel, err)
	}
}

func TestPal24WebhookAPIError(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"http 500":      func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"success false": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":false,"message":"x"}`)) },
	} {
		p := p24Server(t, h)
		ev, rel, err := p24Post(t, p, p24Fields("SUCCESS", "api-token"))
		if err == nil || rel || ev.Paid || ev.Review {
			t.Errorf("%s: %+v rel=%v err=%v", name, ev, rel, err)
		}
	}
}

// Without a bill id the postback cannot be checked here; it is ignored and the background sweep
// confirms the payment through the stored bill id.
func TestPal24WebhookNoBillIDIsIgnored(t *testing.T) {
	p, api := p24BillServer(t, p24PaidBill)
	for _, st := range []string{"SUCCESS", "OVERPAID", "UNDERPAID"} {
		f := p24Fields(st, "api-token")
		delete(f, "bill_id")
		ev, rel, err := p24Post(t, p, f)
		if err != nil || rel || ev.Paid || ev.Review {
			t.Errorf("%s: %+v rel=%v err=%v", st, ev, rel, err)
		}
		f["bill_id"] = "  "
		if ev, rel, err = p24Post(t, p, f); err != nil || rel || ev.Paid || ev.Review {
			t.Errorf("%s blank bill_id: %+v rel=%v err=%v", st, ev, rel, err)
		}
	}
	if api.hits.Load() != 0 {
		t.Errorf("%d API calls without a bill id", api.hits.Load())
	}
}

func TestPal24WebhookOtherStatusesMakeNoAPICall(t *testing.T) {
	p, api := p24BillServer(t, p24PaidBill)
	for _, st := range []string{"FAIL", "NEW", "PROCESS", "CANCELLED", "SOMETHING_NEW"} {
		if ev, rel, err := p24Post(t, p, p24Fields(st, "api-token")); err != nil || rel || ev.Paid {
			t.Errorf("%s: %+v rel=%v err=%v", st, ev, rel, err)
		}
	}
	if api.hits.Load() != 0 {
		t.Errorf("%d API calls for non-paid statuses", api.hits.Load())
	}
}

func TestPal24Status(t *testing.T) {
	var gotPath, gotQuery string
	serve := func(body string) *Pal24 {
		return p24Server(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer api-token" {
				t.Errorf("request %s auth=%q", r.Method, r.Header.Get("Authorization"))
			}
			w.Write([]byte(body))
		})
	}
	ctx := context.Background()

	p := serve(`{"success":true,"status":"SUCCESS","amount":"800.00","currency_in":"USD","order_id":"` + p24Order + `"}`)
	ev, err := p.Status(ctx, "B/1 &x")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bill/status" || gotQuery != "id=B%2F1+%26x" {
		t.Errorf("path %q query %q", gotPath, gotQuery)
	}
	want := Event{Provider: "pal24", OrderID: p24Order, ExternalID: "B/1 &x", Paid: true, Amount: 800, Currency: "USD"}
	if ev != want {
		t.Errorf("event %+v, want %+v", ev, want)
	}

	p = serve(`{"success":true,"status":"OVERPAID","amount":850.5,"currency_in":"usd"}`)
	if ev, err = p.Status(ctx, "B"); err != nil || !ev.Paid || ev.Amount != 850.5 || ev.Currency != "USD" || ev.Fixed || ev.OrderID != "" {
		t.Errorf("number amount: %+v %v", ev, err)
	}

	p = serve(`{"success":true,"status":"SUCCESS"}`)
	if ev, err = p.Status(ctx, "B"); err != nil || !ev.Paid || !ev.Fixed || ev.Amount != 0 || ev.Currency != "" {
		t.Errorf("paid, nothing reported: %+v %v", ev, err)
	}

	p = serve(`{"success":true,"status":"UNDERPAID"}`)
	if ev, err = p.Status(ctx, "B"); err != nil || ev.Paid || !ev.Review || ev.Fixed {
		t.Errorf("UNDERPAID: %+v %v", ev, err)
	}

	p = serve(`{"success":"true","bill":{"status":"SUCCESS","amount":"100.00","currency_in":"RUB","order_id":"` + p24Order + `"}}`)
	if ev, err = p.Status(ctx, "B"); err != nil || !ev.Paid || ev.Amount != 100 || ev.Currency != "RUB" || ev.OrderID != p24Order || ev.Fixed {
		t.Errorf("nested: %+v %v", ev, err)
	}

	for _, st := range []string{"NEW", "PROCESS", "FAIL", "CANCELLED"} {
		p = serve(`{"success":true,"status":"` + st + `","amount":"800.00","currency_in":"USD"}`)
		if ev, err = p.Status(ctx, "B"); err != nil || ev.Paid || ev.Review || ev.Fixed {
			t.Errorf("%s: %+v %v", st, ev, err)
		}
	}
	// An unpaid bill with no amount is not "fixed".
	p = serve(`{"success":true,"status":"NEW"}`)
	if ev, err = p.Status(ctx, "B"); err != nil || ev.Paid || ev.Fixed {
		t.Errorf("NEW bare: %+v %v", ev, err)
	}
}

func TestPal24StatusErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"http 404":      {404, `no such bill`},
		"success false": {200, `{"success":false,"message":"not found"}`},
		"garbage":       {200, `<html>`},
	} {
		p := p24Server(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		if _, err := p.Status(context.Background(), "B"); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if strings.Contains(err.Error(), "api-token") {
			t.Errorf("%s: error leaks the token: %v", name, err)
		}
	}
	var ae *apiError
	p := p24Server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
	if _, err := p.Status(context.Background(), "B"); !errors.As(err, &ae) || ae.Status != 502 {
		t.Errorf("want apiError 502, got %v", err)
	}
}
