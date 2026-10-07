package payments

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func hkServer(t *testing.T, h http.HandlerFunc) *Heleket {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Heleket{MerchantID: "merch-uuid", APIKey: "secret", BaseURL: srv.URL, CallbackURL: "https://bot.example.com/pay/heleket"}
}

func hkMD5(signed, key string) string {
	sum := md5.Sum([]byte(base64.StdEncoding.EncodeToString([]byte(signed)) + key))
	return hex.EncodeToString(sum[:])
}

func TestHeleketInvoice(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/payment" || r.Header.Get("merchant") != "merch-uuid" {
			t.Errorf("request %s %s merchant=%q", r.Method, r.URL.Path, r.Header.Get("merchant"))
		}
		if r.Header.Get("sign") != hkMD5(string(raw), "secret") {
			t.Errorf("sign %q does not match the body", r.Header.Get("sign"))
		}
		var b map[string]any
		json.Unmarshal(raw, &b)
		if b["amount"] != "800.00" || b["currency"] != "RUB" || b["order_id"] != "ord-1" || b["lifetime"] != float64(43200) ||
			b["url_callback"] != "https://bot.example.com/pay/heleket" || b["to_currency"] != "USDT" || b["network"] != "tron" {
			t.Errorf("body %v", b)
		}
		w.Write([]byte(`{"state":0,"result":{"uuid":"hk-1","order_id":"ord-1","url":"https://pay.heleket.com/pay/hk-1","payment_status":"check"}}`))
	})
	h.ToCurrency, h.Network = "USDT", "tron"
	inv, err := h.Invoice(t.Context(), InvoiceRequest{OrderID: "ord-1", Title: "t", Amount: 800, ExpiresIn: 48 * time.Hour}) // lifetime is clamped to 12 h
	if err != nil || inv.ExternalID != "hk-1" || inv.URL != "https://pay.heleket.com/pay/hk-1" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestHeleketInvoiceErrors(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"state":1,"message":"bad merchant"}`)) })
	if _, err := h.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err == nil || !strings.Contains(err.Error(), "bad merchant") {
		t.Fatalf("want the provider's message, got %v", err)
	}
}

// hookBody is a webhook as PHP would send it: slashes escaped, sign in the middle.
func hookBody(status, key string) []byte {
	signed := `{"type":"payment","uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","url":"https:\/\/pay.heleket.com\/pay\/hk-1","payment_status":"` + status + `","note":"Привет"}`
	sign := hkMD5(signed, key)
	return []byte(`{ "type":"payment", "uuid":"hk-1", "order_id":"ord-1", "sign":"` + sign + `", "amount":"800.00", "currency":"RUB", ` +
		`"url":"https:\/\/pay.heleket.com\/pay\/hk-1", "payment_status":"` + status + `", "note":"Привет" }`)
}

func TestHeleketWebhook(t *testing.T) {
	h := &Heleket{MerchantID: "m", APIKey: "secret"}
	ctx := t.Context()

	ev, relevant, err := h.Webhook(ctx, nil, hookBody("paid", "secret"))
	if err != nil || !relevant || !ev.Paid || ev.Review || ev.OrderID != "ord-1" || ev.ExternalID != "hk-1" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "heleket" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}
	if ev, relevant, _ := h.Webhook(ctx, nil, hookBody("paid_over", "secret")); !relevant || !ev.Paid {
		t.Errorf("paid_over is paid: %+v", ev)
	}
	if ev, relevant, _ := h.Webhook(ctx, nil, hookBody("wrong_amount", "secret")); !relevant || ev.Paid || !ev.Review {
		t.Errorf("wrong_amount needs a human: %+v", ev)
	}
	for _, st := range []string{"check", "cancel", "fail", "system_fail", "process"} {
		if _, relevant, err := h.Webhook(ctx, nil, hookBody(st, "secret")); err != nil || relevant {
			t.Errorf("%s: relevant=%v err=%v", st, relevant, err)
		}
	}
	// The unescaped-slash form of the signature is accepted too.
	plain := `{"type":"payment","uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","payment_status":"paid","url":"https://pay.heleket.com/x"}`
	body := []byte(`{"sign":"` + hkMD5(plain, "secret") + `",` + plain[1:])
	if _, relevant, err := h.Webhook(ctx, nil, body); err != nil || !relevant {
		t.Errorf("unescaped form: relevant=%v err=%v", relevant, err)
	}
	// Forged.
	if _, _, err := h.Webhook(ctx, nil, hookBody("paid", "other-key")); !errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong key: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(strings.Replace(string(hookBody("paid", "secret")), `"800.00"`, `"1.00"`, 1))); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered amount: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(`{"payment_status":"paid"}`)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("no sign: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(`nope`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("junk: %v", err)
	}
}

func TestHeleketWebhookIgnoresPayouts(t *testing.T) {
	h := &Heleket{MerchantID: "m", APIKey: "secret"}
	signed := `{"type":"payout","uuid":"x","payment_status":"paid"}`
	body := []byte(`{"sign":"` + hkMD5(signed, "secret") + `",` + signed[1:])
	if _, relevant, err := h.Webhook(t.Context(), nil, body); err != nil || relevant {
		t.Errorf("payout: relevant=%v err=%v", relevant, err)
	}
}

func TestHeleketStatus(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/payment/info" || b["uuid"] != "hk-1" {
			t.Errorf("request %s %v", r.URL.Path, b)
		}
		w.Write([]byte(`{"state":0,"result":{"uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","payment_status":"paid"}}`))
	})
	ev, err := h.Status(t.Context(), "hk-1")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" || ev.Amount != 800 {
		t.Fatalf("%+v %v", ev, err)
	}
}
