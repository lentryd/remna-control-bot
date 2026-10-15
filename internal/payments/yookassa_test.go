package payments

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ykServer(t *testing.T, h http.HandlerFunc) *YooKassa {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &YooKassa{ShopID: "shop", Secret: "key", ReturnURL: "https://app/bot/", VAT: 1, BaseURL: srv.URL}
}

func TestYooKassaInvoice(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != "POST" || r.URL.Path != "/payments" || !ok || user != "shop" || pass != "key" {
			t.Errorf("request %s %s basic-auth=%v", r.Method, r.URL.Path, ok)
		}
		if r.Header.Get("Idempotence-Key") != "ord-1" {
			t.Errorf("idempotence key %q", r.Header.Get("Idempotence-Key"))
		}
		var b struct {
			Amount       struct{ Value, Currency string }
			Capture      bool
			Confirmation struct {
				Type      string
				ReturnURL string `json:"return_url"`
			}
			Description string
			Metadata    map[string]string
			Receipt     *struct {
				Customer struct{ Email string }
				Items    []struct {
					VATCode int `json:"vat_code"`
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if b.Amount.Value != "800.00" || b.Amount.Currency != "RUB" || !b.Capture || b.Confirmation.Type != "redirect" ||
			b.Confirmation.ReturnURL != "https://app/bot/" || b.Metadata["order_id"] != "ord-1" || b.Description != "Продление" {
			t.Errorf("body %+v", b)
		}
		if b.Receipt != nil {
			t.Errorf("receipt without an e-mail: %+v", b.Receipt)
		}
		_, _ = w.Write([]byte(`{"id":"p1","status":"pending","confirmation":{"type":"redirect","confirmation_url":"https://yoomoney.ru/pay/p1"}}`))
	})
	inv, err := y.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "ord-1", Title: "Продление", Amount: 800})
	if err != nil || inv.ExternalID != "p1" || inv.URL != "https://yoomoney.ru/pay/p1" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestYooKassaInvoiceReceipt(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Receipt struct {
				Customer struct{ Email string }
				Items    []struct {
					VATCode        int    `json:"vat_code"`
					PaymentMode    string `json:"payment_mode"`
					PaymentSubject string `json:"payment_subject"`
					Quantity       string
				}
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b.Receipt.Customer.Email != "a@b.c" || len(b.Receipt.Items) != 1 || b.Receipt.Items[0].VATCode != 4 ||
			b.Receipt.Items[0].PaymentMode != "full_payment" || b.Receipt.Items[0].PaymentSubject != "service" || b.Receipt.Items[0].Quantity != "1.00" {
			t.Errorf("receipt %+v", b.Receipt)
		}
		_, _ = w.Write([]byte(`{"id":"p1","confirmation":{"confirmation_url":"https://x"}}`))
	})
	y.ReceiptEmail, y.VAT = "a@b.c", 4
	if _, err := y.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestYooKassaInvoiceErrors(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","code":"invalid_request"}`))
	})
	if _, err := y.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error for HTTP 400")
	}
	y2 := ykServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"id":"p1"}`)) })
	if _, err := y2.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error for a response without a confirmation url")
	}
}

const ykPaid = `{"id":"p1","status":"succeeded","paid":true,"amount":{"value":"800.00","currency":"RUB"},"metadata":{"order_id":"ord-1"}}`

func TestYooKassaWebhook(t *testing.T) {
	paid := true
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/payments/p1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if paid {
			_, _ = w.Write([]byte(ykPaid))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p1","status":"pending","paid":false,"amount":{"value":"800.00","currency":"RUB"},"metadata":{"order_id":"ord-1"}}`))
	})
	ctx := t.Context()
	body := []byte(`{"type":"notification","event":"payment.succeeded","object":{"id":"p1","status":"succeeded"}}`)

	ev, relevant, err := y.Webhook(ctx, nil, body)
	if err != nil || !relevant || !ev.Paid || ev.OrderID != "ord-1" || ev.ExternalID != "p1" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "yookassa" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}

	// Forged body: it claims success, the API says pending → nothing to do.
	paid = false
	if _, relevant, err := y.Webhook(ctx, nil, body); err != nil || relevant {
		t.Errorf("forged: relevant=%v err=%v", relevant, err)
	}
	// Other events and a missing id are not relevant / not valid.
	if _, relevant, err := y.Webhook(ctx, nil, []byte(`{"event":"payment.canceled","object":{"id":"p1"}}`)); err != nil || relevant {
		t.Errorf("canceled: relevant=%v err=%v", relevant, err)
	}
	if _, _, err := y.Webhook(ctx, nil, []byte(`{"event":"payment.succeeded","object":{}}`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("no id: %v", err)
	}
	if _, _, err := y.Webhook(ctx, nil, []byte(`not json`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("garbage: %v", err)
	}
}

func TestYooKassaWebhookAPIDown(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, relevant, err := y.Webhook(t.Context(), nil, []byte(`{"event":"payment.succeeded","object":{"id":"p1"}}`))
	if err == nil || relevant {
		t.Fatalf("an unverifiable payment must fail (so the provider retries): relevant=%v err=%v", relevant, err)
	}
}

func TestYooKassaStatus(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ykPaid)) })
	ev, err := y.Status(t.Context(), "p1")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" {
		t.Fatalf("%+v %v", ev, err)
	}
}

// The webhook body is unauthenticated, yet its payment id goes into a request
// signed with the shop credentials: it must stay ONE escaped path segment.
func TestYooKassaHostileID(t *testing.T) {
	cases := []struct{ id, want string }{
		{"p1/../x", "/payments/p1%2F..%2Fx"},
		{"../refunds/x", "/payments/..%2Frefunds%2Fx"},
		{"p1?x=y", "/payments/p1%3Fx=y"},
	}
	for _, c := range cases {
		var gotPath, gotQuery string
		y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
			_, _ = w.Write([]byte(`{"id":"p1","status":"pending","paid":false}`))
		})
		body, _ := json.Marshal(map[string]any{"event": "payment.succeeded", "object": map[string]string{"id": c.id}})
		ev, relevant, err := y.Webhook(t.Context(), nil, body)
		if err != nil || relevant || ev.Paid {
			t.Errorf("%q: nothing must be applied: %+v relevant=%v err=%v", c.id, ev, relevant, err)
		}
		if gotPath != c.want || gotQuery != "" {
			t.Errorf("%q: server saw path %q query %q, want path %q and no query", c.id, gotPath, gotQuery, c.want)
		}
		gotPath, gotQuery = "", ""
		if _, err := y.Status(t.Context(), c.id); err != nil {
			t.Errorf("%q: status: %v", c.id, err)
		}
		if gotPath != c.want || gotQuery != "" {
			t.Errorf("%q: status saw path %q query %q, want path %q and no query", c.id, gotPath, gotQuery, c.want)
		}
	}
}

func TestYooKassaInvoiceCurrency(t *testing.T) {
	var got struct {
		Amount  struct{ Value, Currency string }
		Receipt *struct {
			Items []struct {
				Amount struct{ Value, Currency string }
			}
		}
	}
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"p1","confirmation":{"confirmation_url":"https://x"}}`))
	})
	y.ReceiptEmail = "a@b.c"
	inv, err := y.Invoice(t.Context(), InvoiceRequest{Currency: "USD", OrderID: "o", Title: "t", Amount: 9.5})
	if err != nil || inv.Currency != "USD" || inv.Amount != 9.5 {
		t.Fatalf("%+v %v", inv, err)
	}
	if got.Amount.Currency != "USD" || got.Amount.Value != "9.50" || got.Receipt == nil || len(got.Receipt.Items) != 1 || got.Receipt.Items[0].Amount.Currency != "USD" {
		t.Errorf("body %+v", got)
	}
}

func TestYooKassaInvoiceRejectsBadCurrency(t *testing.T) {
	calls := 0
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, cur := range []string{"", "usd", "US", "RUBL"} {
		if _, err := y.Invoice(t.Context(), InvoiceRequest{Currency: cur, OrderID: "o", Title: "t", Amount: 1}); err == nil {
			t.Errorf("currency %q: want an error", cur)
		}
	}
	if calls != 0 {
		t.Errorf("a bad currency must not reach the provider: %d requests", calls)
	}
}
