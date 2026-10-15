package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func cbServer(t *testing.T, h http.HandlerFunc) *CryptoBot {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &CryptoBot{Token: "123:AAtoken", BaseURL: srv.URL}
}

// cbSig is the documented signature: HMAC-SHA256 of the raw body, keyed with SHA256(token).
func cbSig(token string, body []byte) string {
	key := sha256.Sum256([]byte(token))
	m := hmac.New(sha256.New, key[:])
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func TestCryptoBotInvoice(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/createInvoice" || r.Header.Get("Crypto-Pay-API-Token") != "123:AAtoken" {
			t.Errorf("request %s %s token=%q", r.Method, r.URL.Path, r.Header.Get("Crypto-Pay-API-Token"))
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["currency_type"] != "fiat" || b["fiat"] != "RUB" || b["amount"] != "800.00" || b["payload"] != "ord-1" ||
			b["expires_in"] != float64(3600) || b["accepted_assets"] != "USDT,TON" || b["description"] != "Продление" {
			t.Errorf("body %v", b)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"invoice_id":77,"status":"active","bot_invoice_url":"https://t.me/CryptoBot?start=IVabc"}}`))
	})
	c.Assets = "USDT,TON"
	inv, err := c.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "ord-1", Title: "Продление", Amount: 800, ExpiresIn: time.Hour})
	if err != nil || inv.ExternalID != "77" || inv.URL != "https://t.me/CryptoBot?start=IVabc" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestCryptoBotInvoiceErrors(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":400,"name":"AMOUNT_TOO_SMALL"}}`))
	})
	if _, err := c.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error when ok=false")
	}
}

func TestCryptoBotTestnetBase(t *testing.T) {
	if (&CryptoBot{Testnet: true}).base() != "https://testnet-pay.crypt.bot/api" || (&CryptoBot{}).base() != "https://pay.crypt.bot/api" {
		t.Error("base urls")
	}
}

const cbPaid = `{"update_id":1,"update_type":"invoice_paid","request_date":"2026-10-07T12:00:00Z","payload":{"invoice_id":77,"status":"paid","currency_type":"fiat","fiat":"RUB","amount":"800.00","payload":"ord-1"}}`

func TestCryptoBotWebhook(t *testing.T) {
	c := &CryptoBot{Token: "123:AAtoken"}
	hdr := func(body []byte) http.Header {
		h := http.Header{}
		h.Set("crypto-pay-api-signature", cbSig("123:AAtoken", body))
		return h
	}
	body := []byte(cbPaid)
	ev, relevant, err := c.Webhook(t.Context(), hdr(body), body)
	if err != nil || !relevant || !ev.Paid || ev.OrderID != "ord-1" || ev.ExternalID != "77" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "cryptobot" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}
	// Forged: wrong signature, missing signature, altered body under the old signature.
	bad := http.Header{}
	bad.Set("crypto-pay-api-signature", cbSig("other-token", body))
	for name, h := range map[string]http.Header{"wrong key": bad, "missing": {}} {
		if _, _, err := c.Webhook(t.Context(), h, body); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	tampered := []byte(`{"update_type":"invoice_paid","payload":{"invoice_id":77,"status":"paid","fiat":"RUB","amount":"1.00","payload":"ord-1"}}`)
	if _, _, err := c.Webhook(t.Context(), hdr(body), tampered); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered: %v", err)
	}
	// Signed but not a payment, or not JSON.
	other := []byte(`{"update_type":"something_else","payload":{}}`)
	if _, relevant, err := c.Webhook(t.Context(), hdr(other), other); err != nil || relevant {
		t.Errorf("other update: relevant=%v err=%v", relevant, err)
	}
	junk := []byte(`nope`)
	if _, _, err := c.Webhook(t.Context(), hdr(junk), junk); !errors.Is(err, ErrBadBody) {
		t.Errorf("junk: %v", err)
	}
}

func TestCryptoBotStatus(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/getInvoices" || r.URL.Query().Get("invoice_ids") != "77" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"items":[{"invoice_id":77,"status":"paid","fiat":"RUB","amount":"800.00","payload":"ord-1"}]}}`))
	})
	ev, err := c.Status(t.Context(), "77")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" || ev.Amount != 800 {
		t.Fatalf("%+v %v", ev, err)
	}
	empty := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"items":[]}}`))
	})
	if _, err := empty.Status(t.Context(), "77"); err == nil {
		t.Fatal("want an error for an unknown invoice")
	}
}

func TestCryptoBotInvoiceCurrency(t *testing.T) {
	var got map[string]any
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"invoice_id":77,"status":"active","bot_invoice_url":"https://t.me/CryptoBot?start=IVabc"}}`))
	})
	inv, err := c.Invoice(t.Context(), InvoiceRequest{Currency: "USD", OrderID: "o", Title: "t", Amount: 9.5})
	if err != nil || inv.Currency != "USD" || inv.Amount != 9.5 {
		t.Fatalf("%+v %v", inv, err)
	}
	if got["fiat"] != "USD" || got["currency_type"] != "fiat" || got["amount"] != "9.50" {
		t.Errorf("body %v", got)
	}
}

func TestCryptoBotInvoiceRejectsBadCurrency(t *testing.T) {
	calls := 0
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, cur := range []string{"", "usd", "US", "RUBL"} {
		if _, err := c.Invoice(t.Context(), InvoiceRequest{Currency: cur, OrderID: "o", Title: "t", Amount: 1}); err == nil {
			t.Errorf("currency %q: want an error", cur)
		}
	}
	if calls != 0 {
		t.Errorf("a bad currency must not reach the provider: %d requests", calls)
	}
}
