package payments

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func pgServer(t *testing.T, h http.HandlerFunc) *Platega {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Platega{MerchantID: "m-1", Secret: "s3cret", PaymentMethod: 2, BaseURL: srv.URL}
}

func pgAuth(h http.Header) http.Header {
	h.Set("X-MerchantId", "m-1")
	h.Set("X-Secret", "s3cret")
	return h
}

func pgHook(id, status, payload string) []byte {
	b, _ := json.Marshal(map[string]any{"id": id, "status": status, "payload": payload})
	return b
}

type pgBody struct {
	PaymentMethod  int `json:"paymentMethod"`
	PaymentDetails struct {
		Amount   json.Number `json:"amount"`
		Currency string      `json:"currency"`
	} `json:"paymentDetails"`
	Description string `json:"description"`
	Return      *string
	FailedURL   *string `json:"failedUrl"`
	Payload     string  `json:"payload"`
	Metadata    map[string]string
}

func TestPlategaInvoiceV1(t *testing.T) {
	var raw map[string]json.RawMessage
	var b pgBody
	var path string
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.Method != "POST" || r.Header.Get("X-MerchantId") != "m-1" || r.Header.Get("X-Secret") != "s3cret" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request %s headers %v", r.Method, r.Header)
		}
		data := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			data.Write(buf[:n])
			if err != nil {
				break
			}
		}
		if err := json.Unmarshal([]byte(data.String()), &raw); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal([]byte(data.String()), &b)
		_, _ = w.Write([]byte(`{"transactionId":"tx-1","status":"PENDING","redirect":"https://pay.platega.io/tx-1"}`))
	})
	title := strings.Repeat("я", 100)
	inv, err := p.Invoice(t.Context(), InvoiceRequest{Currency: "USD", OrderID: "ord-1", Title: title, Amount: 12.5, CustomerID: 42})
	if err != nil || inv.ExternalID != "tx-1" || inv.URL != "https://pay.platega.io/tx-1" || inv.Amount != 12.5 || inv.Currency != "USD" {
		t.Fatalf("%+v %v", inv, err)
	}
	if path != "/transaction/process" {
		t.Errorf("path %q", path)
	}
	if b.PaymentMethod != 2 || b.PaymentDetails.Amount.String() != "12.5" || b.PaymentDetails.Currency != "USD" || b.Payload != "ord-1" {
		t.Errorf("body %+v", b)
	}
	if len([]rune(b.Description)) != 64 {
		t.Errorf("description has %d runes", len([]rune(b.Description)))
	}
	if b.Metadata["userId"] != "c42" || b.Metadata["userName"] != "Customer 42" || len(b.Metadata) != 2 {
		t.Errorf("metadata %v", b.Metadata)
	}
	if _, ok := raw["return"]; ok {
		t.Errorf("return sent without being configured")
	}
	if _, ok := raw["failedUrl"]; ok {
		t.Errorf("failedUrl sent without being configured")
	}
}

func TestPlategaInvoiceV2(t *testing.T) {
	var path string
	var b pgBody
	var raw map[string]json.RawMessage
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var all json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&all)
		_ = json.Unmarshal(all, &raw)
		_ = json.Unmarshal(all, &b)
		_, _ = w.Write([]byte(`{"id":"tx-2","status":"PENDING","url":"https://pay.platega.io/v2/tx-2"}`))
	})
	p.APIVersion = "v2"
	p.PaymentMethod = 11
	p.ReturnURL = "https://t.me/bot"
	p.FailedURL = "https://t.me/bot?start=fail"
	inv, err := p.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "ord-2", Title: "Продление", Amount: 800})
	if err != nil || inv.ExternalID != "tx-2" || inv.URL != "https://pay.platega.io/v2/tx-2" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
	if path != "/v2/transaction/process" {
		t.Errorf("path %q", path)
	}
	if string(raw["paymentDetails"]) == "" || b.PaymentDetails.Amount.String() != "800" || b.PaymentMethod != 11 {
		t.Errorf("body %s", string(raw["paymentDetails"]))
	}
	if b.Return == nil || *b.Return != "https://t.me/bot" || b.FailedURL == nil || *b.FailedURL != "https://t.me/bot?start=fail" {
		t.Errorf("return/failedUrl: %v %v", b.Return, b.FailedURL)
	}
	// Without a customer id the order id stands in for it.
	if b.Metadata["userId"] != "cord-2" {
		t.Errorf("metadata %v", b.Metadata)
	}
}

func TestPlategaInvoiceZeroCustomerUsesOrderID(t *testing.T) {
	var b pgBody
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&b)
		_, _ = w.Write([]byte(`{"transactionId":"tx","redirect":"https://x/y"}`))
	})
	if _, err := p.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "ord-9", Amount: 1}); err != nil {
		t.Fatal(err)
	}
	if b.Metadata["userId"] != "cord-9" || b.Metadata["userName"] != "Customer ord-9" {
		t.Errorf("metadata %v", b.Metadata)
	}
}

func TestPlategaInvoiceErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"no id", 200, `{"redirect":"https://x/y"}`},
		{"no link", 200, `{"transactionId":"tx-1"}`},
		{"400", 400, `{"message":"bad merchant"}`},
		{"500", 500, `oops`},
		{"garbage", 200, `<html>`},
	}
	for _, c := range cases {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		inv, err := p.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Amount: 1})
		if err == nil {
			t.Errorf("%s: no error, got %+v", c.name, inv)
			continue
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("%s: secret leaked into %v", c.name, err)
		}
	}
}

func TestPlategaInvoiceBadCurrencyMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	for _, cur := range []string{"", "rub", "RUBL", "R/B"} {
		if _, err := p.Invoice(t.Context(), InvoiceRequest{Currency: cur, OrderID: "o", Amount: 1}); err == nil {
			t.Errorf("%q accepted", cur)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests made", hits.Load())
	}
}

func TestPlategaWebhookAuth(t *testing.T) {
	var hits atomic.Int32
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	body := pgHook("tx-1", "CONFIRMED", "ord-1")
	cases := map[string]http.Header{
		"wrong merchant": {"X-MerchantId": {"other"}, "X-Secret": {"s3cret"}},
		"wrong secret":   {"X-MerchantId": {"m-1"}, "X-Secret": {"nope"}},
		"both wrong":     {"X-MerchantId": {"other"}, "X-Secret": {"nope"}},
		"no headers":     nil,
		"only merchant":  {"X-MerchantId": {"m-1"}},
		"only secret":    {"X-Secret": {"s3cret"}},
	}
	for name, h := range cases {
		ev, relevant, err := p.Webhook(t.Context(), h, body)
		if !errors.Is(err, ErrBadSignature) || relevant || ev.Paid {
			t.Errorf("%s: %+v %v %v", name, ev, relevant, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("unauthenticated callbacks reached the API %d times", hits.Load())
	}
	// A configured provider with an empty secret/merchant never matches empty headers.
	empty := &Platega{BaseURL: p.BaseURL}
	if _, _, err := empty.Webhook(t.Context(), http.Header{}, body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("empty config accepted a headerless body: %v", err)
	}
	if _, _, err := empty.Webhook(t.Context(), http.Header{"X-Merchantid": {""}, "X-Secret": {""}}, body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("empty config matched empty headers: %v", err)
	}
}

func TestPlategaWebhookPing(t *testing.T) {
	var hits atomic.Int32
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	for _, body := range [][]byte{nil, {}, []byte("  \n\t")} {
		ev, relevant, err := p.Webhook(t.Context(), http.Header{}, body)
		if err != nil || relevant || ev.Paid {
			t.Errorf("ping %q: %+v %v %v", body, ev, relevant, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("ping reached the API")
	}
}

func TestPlategaWebhookBadBody(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("API called") })
	_, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), []byte("not json"))
	if !errors.Is(err, ErrBadBody) || relevant {
		t.Errorf("%v %v", relevant, err)
	}
	// A valid secret with an empty body is not a ping: it is simply nothing to parse.
	if _, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), nil); !errors.Is(err, ErrBadBody) || relevant {
		t.Errorf("authenticated empty body: %v %v", relevant, err)
	}
}

func TestPlategaWebhookConfirmed(t *testing.T) {
	var path string
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.Method != "GET" || r.Header.Get("X-MerchantId") != "m-1" || r.Header.Get("X-Secret") != "s3cret" {
			t.Errorf("request %s headers %v", r.Method, r.Header)
		}
		_, _ = w.Write([]byte(`{"id":"tx-1","status":"CONFIRMED","payload":"ord-api","paymentDetails":{"amount":800,"currency":"RUB"}}`))
	})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "confirmed", "ord-callback"))
	if err != nil || !relevant || !ev.Paid || ev.Fixed {
		t.Fatalf("%+v %v %v", ev, relevant, err)
	}
	if ev.Provider != "platega" || ev.OrderID != "ord-api" || ev.ExternalID != "tx-1" || ev.Amount != 800 || ev.Currency != "RUB" {
		t.Errorf("%+v", ev)
	}
	if path != "/transaction/tx-1" {
		t.Errorf("path %q", path)
	}
}

func TestPlategaWebhookTransactionIDVariantAndTopLevelAmount(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transaction/tx-7" {
			t.Errorf("path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"CONFIRMED","amount":"12.50","currency":"USD"}`))
	})
	body, _ := json.Marshal(map[string]any{"transactionId": "tx-7", "status": "CONFIRMED", "payload": "ord-cb"})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), body)
	if err != nil || !relevant || !ev.Paid || ev.Fixed {
		t.Fatalf("%+v %v %v", ev, relevant, err)
	}
	// The API gave no payload: the callback's stands in; id is the one asked about.
	if ev.OrderID != "ord-cb" || ev.ExternalID != "tx-7" || ev.Amount != 12.5 || ev.Currency != "USD" {
		t.Errorf("%+v", ev)
	}
}

func TestPlategaWebhookFixedWhenAPIReportsNoMoney(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"tx-1","status":"CONFIRMED","payload":"ord-1"}`))
	})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1"))
	if err != nil || !relevant || !ev.Paid || !ev.Fixed || ev.Amount != 0 || ev.Currency != "" || ev.OrderID != "ord-1" {
		t.Fatalf("%+v %v %v", ev, relevant, err)
	}
}

func TestPlategaWebhookNotFixedWhenOnlyOnePartReported(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"tx-1","status":"CONFIRMED","paymentDetails":{"amount":5}}`))
	})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1"))
	if err != nil || !relevant || ev.Fixed || ev.Amount != 5 {
		t.Fatalf("%+v %v %v", ev, relevant, err)
	}
}

func TestPlategaWebhookForged(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"tx-1","status":"PENDING","payload":"ord-1","paymentDetails":{"amount":800,"currency":"RUB"}}`))
	})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1"))
	if err != nil || relevant || ev.Paid {
		t.Fatalf("a forged CONFIRMED was applied: %+v %v %v", ev, relevant, err)
	}
}

func TestPlategaWebhookAPIError(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	})
	ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1"))
	if err == nil || relevant || ev.Paid {
		t.Fatalf("%+v %v %v", ev, relevant, err)
	}
}

func TestPlategaWebhookOtherStatusesMakeNoAPICall(t *testing.T) {
	var hits atomic.Int32
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	for _, st := range []string{"FAILED", "CANCELED", "EXPIRED", "PENDING", "INPROGRESS", ""} {
		ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", st, "ord-1"))
		if err != nil || relevant || ev.Paid {
			t.Errorf("%q: %+v %v %v", st, ev, relevant, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d API calls for non-confirmed callbacks", hits.Load())
	}
}

func TestPlategaWebhookConfirmedWithoutID(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("API called") })
	body, _ := json.Marshal(map[string]any{"status": "CONFIRMED", "payload": "ord-1"})
	if _, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), body); !errors.Is(err, ErrBadBody) || relevant {
		t.Errorf("%v %v", relevant, err)
	}
}

func TestPlategaHostileID(t *testing.T) {
	ids := []string{"p1/../x", "../x", "..", ".", "a?b=c", "a#b", "a b", "a%2Fb", "a\\b", "é", strings.Repeat("a", 200), "tx\n1"}
	var hits atomic.Int32
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		t.Errorf("request reached the server: %s %s", r.Method, r.URL.String())
		_, _ = w.Write([]byte(`{"id":"x","status":"CONFIRMED"}`))
	})
	for _, id := range ids {
		body, _ := json.Marshal(map[string]any{"id": id, "status": "CONFIRMED", "payload": "ord-1"})
		ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), body)
		if relevant || ev.Paid {
			t.Errorf("%q: applied %+v", id, ev)
		}
		if err == nil {
			t.Errorf("%q: no error", id)
		}
		if _, err := p.Status(t.Context(), id); err == nil {
			t.Errorf("%q: status accepted", id)
		}
	}
	if _, err := p.Status(t.Context(), ""); err == nil {
		t.Error("empty id accepted")
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests made", hits.Load())
	}
}

func TestPlategaValidIDsStayOnePathSegment(t *testing.T) {
	for _, id := range []string{"tx-1", "A_b-9", strings.Repeat("a", 100), "3fa85f64-5717-4562-b3fc-2c963f66afa6"} {
		var path string
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.EscapedPath()
			_, _ = w.Write([]byte(`{"status":"PENDING"}`))
		})
		if _, err := p.Status(t.Context(), id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
		if path != "/transaction/"+id {
			t.Errorf("%q: path %q", id, path)
		}
	}
}

func TestPlategaStatus(t *testing.T) {
	var path string
	body := `{"id":"tx-1","status":"CONFIRMED","payload":"ord-1","paymentDetails":{"amount":800,"currency":"RUB"}}`
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(body))
	})
	p.APIVersion = "v2" // the status endpoint is not versioned
	ev, err := p.Status(t.Context(), "tx-1")
	if err != nil || !ev.Paid || ev.Provider != "platega" || ev.OrderID != "ord-1" || ev.ExternalID != "tx-1" ||
		ev.Amount != 800 || ev.Currency != "RUB" || ev.Fixed {
		t.Fatalf("%+v %v", ev, err)
	}
	if path != "/transaction/tx-1" {
		t.Errorf("path %q", path)
	}

	body = `{"id":"tx-1","status":"EXPIRED","payload":"ord-1","paymentDetails":{"amount":800,"currency":"RUB"}}`
	ev, err = p.Status(t.Context(), "tx-1")
	if err != nil || ev.Paid || ev.ExternalID != "tx-1" || ev.OrderID != "ord-1" {
		t.Fatalf("unpaid: %+v %v", ev, err)
	}

	body = `{"status":"CONFIRMED"}`
	ev, err = p.Status(t.Context(), "tx-1")
	if err != nil || !ev.Paid || !ev.Fixed || ev.ExternalID != "tx-1" {
		t.Fatalf("no money reported: %+v %v", ev, err)
	}
}

func TestPlategaStatusAPIError(t *testing.T) {
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte("no such transaction"))
	})
	ev, err := p.Status(t.Context(), "tx-1")
	if err == nil || ev.Paid {
		t.Fatalf("%+v %v", ev, err)
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("%v", err)
	}
}

// A reported amount that cannot be read must never become 0 ("not reported"): the webhook returns an
// error (Platega retries) and Status reports it (the poller logs it).
func TestPlategaUnparsableAmountIsAnError(t *testing.T) {
	for name, body := range map[string]string{
		"details, thousands": `{"id":"tx-1","status":"CONFIRMED","payload":"o","paymentDetails":{"amount":"1 000,00","currency":"RUB"}}`,
		"details, word":      `{"id":"tx-1","status":"CONFIRMED","payload":"o","paymentDetails":{"amount":"abc","currency":"RUB"}}`,
		"top level":          `{"id":"tx-1","status":"CONFIRMED","payload":"o","amount":"abc","currency":"RUB"}`,
		"details win":        `{"id":"tx-1","status":"CONFIRMED","payload":"o","amount":800,"paymentDetails":{"amount":"abc","currency":"RUB"}}`,
		"nan":                `{"id":"tx-1","status":"CONFIRMED","payload":"o","amount":"NaN","currency":"RUB"}`,
		"inf":                `{"id":"tx-1","status":"CONFIRMED","payload":"o","amount":"Inf","currency":"RUB"}`,
		"object":             `{"id":"tx-1","status":"CONFIRMED","payload":"o","amount":{"v":1},"currency":"RUB"}`,
		"unpaid too":         `{"id":"tx-1","status":"PENDING","payload":"o","amount":"abc","currency":"RUB"}`,
	} {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		ev, err := p.Status(t.Context(), "tx-1")
		if err == nil || ev.Paid || ev.Fixed {
			t.Errorf("%s: Status %+v %v", name, ev, err)
		}
		if !strings.Contains(name, "unpaid") {
			ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "o"))
			if err == nil || relevant || ev.Paid || ev.Fixed {
				t.Errorf("%s: Webhook %+v %v %v", name, ev, relevant, err)
			}
		}
	}
	// Only a genuinely absent (or null) field means "not reported".
	for name, body := range map[string]string{
		"absent": `{"id":"tx-1","status":"CONFIRMED"}`,
		"null":   `{"id":"tx-1","status":"CONFIRMED","amount":null,"currency":null,"paymentDetails":null}`,
	} {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		if ev, err := p.Status(t.Context(), "tx-1"); err != nil || !ev.Paid || !ev.Fixed {
			t.Errorf("%s: %+v %v", name, ev, err)
		}
	}
}

// Deliberate: Fixed means "neither amount nor currency reported". A partial report is compared in
// orders.Paid, mismatches there and goes to an admin (safe), instead of being waved through.
func TestPlategaPartialReportIsNotFixed(t *testing.T) {
	for name, tc := range map[string]struct {
		body     string
		amount   float64
		currency string
	}{
		"amount only":   {`{"id":"tx-1","status":"CONFIRMED","paymentDetails":{"amount":5}}`, 5, ""},
		"currency only": {`{"id":"tx-1","status":"CONFIRMED","paymentDetails":{"currency":"RUB"}}`, 0, "RUB"},
	} {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
		ev, err := p.Status(t.Context(), "tx-1")
		if err != nil || !ev.Paid || ev.Fixed || ev.Amount != tc.amount || ev.Currency != tc.currency {
			t.Errorf("%s: Status %+v %v", name, ev, err)
		}
		ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "o"))
		if err != nil || !relevant || ev.Fixed || ev.Amount != tc.amount || ev.Currency != tc.currency {
			t.Errorf("%s: Webhook %+v %v %v", name, ev, relevant, err)
		}
	}
}

// The API names a different transaction than the callback asked about: not our event.
func TestPlategaWebhookIDMismatch(t *testing.T) {
	for name, body := range map[string]string{
		"id":            `{"id":"tx-OTHER","status":"CONFIRMED","payload":"ord-1","paymentDetails":{"amount":800,"currency":"RUB"}}`,
		"transactionId": `{"transactionId":"tx-OTHER","status":"CONFIRMED","payload":"ord-1","paymentDetails":{"amount":800,"currency":"RUB"}}`,
		"one of two":    `{"id":"tx-1","transactionId":"tx-OTHER","status":"CONFIRMED","payload":"ord-1"}`,
	} {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1"))
		if err != nil || relevant || ev.Paid {
			t.Errorf("%s: %+v %v %v", name, ev, relevant, err)
		}
	}
	// The same id under either field name is fine.
	p := pgServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"tx-1","transactionId":"tx-1","status":"CONFIRMED","payload":"ord-1"}`))
	})
	if ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-1")); err != nil || !relevant || !ev.Paid {
		t.Errorf("same id: %+v %v %v", ev, relevant, err)
	}
}

// The order id: the API's payload wins; the callback's stands in only when the API has none.
func TestPlategaWebhookOrderIDSource(t *testing.T) {
	for name, tc := range map[string]struct{ api, want string }{
		"api wins":     {`{"id":"tx-1","status":"CONFIRMED","payload":"ord-api"}`, "ord-api"},
		"api has none": {`{"id":"tx-1","status":"CONFIRMED"}`, "ord-callback"},
	} {
		p := pgServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.api)) })
		ev, relevant, err := p.Webhook(t.Context(), pgAuth(http.Header{}), pgHook("tx-1", "CONFIRMED", "ord-callback"))
		if err != nil || !relevant || ev.OrderID != tc.want || ev.ExternalID != "tx-1" {
			t.Errorf("%s: %+v %v %v", name, ev, relevant, err)
		}
	}
}
