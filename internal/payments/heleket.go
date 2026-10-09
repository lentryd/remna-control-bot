package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const heleketAPI = "https://api.heleket.com/v1"

// Heleket: multi-chain crypto. Invoices are issued in the order's base currency; Heleket converts.
type Heleket struct {
	MerchantID, APIKey  string
	ToCurrency, Network string // optional fixed asset / network
	CallbackURL         string // where Heleket posts status changes
	BaseURL             string // tests; default https://api.heleket.com/v1
	Client              *http.Client
}

var (
	_ WebhookProvider = (*Heleket)(nil)
	_ Poller          = (*Heleket)(nil)
)

func (h *Heleket) ID() string    { return "heleket" }
func (h *Heleket) Title() string { return "Crypto (Heleket)" }

// hkSign is hex(md5(base64(body) + api key)).
func hkSign(signed []byte, key string) string {
	sum := md5.Sum([]byte(base64.StdEncoding.EncodeToString(signed) + key))
	return hex.EncodeToString(sum[:])
}

type hkPayment struct {
	Type          string `json:"type"`
	UUID          string `json:"uuid"`
	OrderID       string `json:"order_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	URL           string `json:"url"`
	PaymentStatus string `json:"payment_status"`
	Status        string `json:"status"`
}

func (p hkPayment) state() string {
	if p.PaymentStatus != "" {
		return p.PaymentStatus
	}
	return p.Status
}

func (p hkPayment) event() Event {
	amount, _ := strconv.ParseFloat(p.Amount, 64)
	st := p.state()
	return Event{Provider: "heleket", OrderID: p.OrderID, ExternalID: p.UUID,
		Paid:   st == "paid" || st == "paid_over",
		Review: st == "wrong_amount" || st == "wrong_amount_waiting",
		Amount: amount, Currency: p.Currency}
}

func (h *Heleket) call(ctx context.Context, path string, in map[string]any) (hkPayment, error) {
	base := h.BaseURL
	if base == "" {
		base = heleketAPI
	}
	body, err := json.Marshal(in) // the exact bytes are signed and sent
	if err != nil {
		return hkPayment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return hkPayment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("merchant", h.MerchantID)
	req.Header.Set("sign", hkSign(body, h.APIKey))
	resp, err := pick(h.Client).Do(req)
	if err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return hkPayment{}, &apiError{"heleket", resp.StatusCode, clip(string(data), 300)}
	}
	var r struct {
		State   int       `json:"state"`
		Message string    `json:"message"`
		Result  hkPayment `json:"result"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	if r.State != 0 {
		return hkPayment{}, fmt.Errorf("heleket: state %d: %s", r.State, clip(r.Message, 200))
	}
	return r.Result, nil
}

func (h *Heleket) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if err := checkCurrency(in.Currency); err != nil {
		return Invoice{}, fmt.Errorf("heleket: %w", err)
	}
	// Heleket accepts lifetimes of 5 minutes … 12 hours.
	lifetime := int(min(max(in.ExpiresIn, 5*time.Minute), 12*time.Hour).Seconds())
	req := map[string]any{"amount": money(in.Amount), "currency": in.Currency, "order_id": in.OrderID, "lifetime": lifetime}
	if h.CallbackURL != "" {
		req["url_callback"] = h.CallbackURL
	}
	if h.ToCurrency != "" {
		req["to_currency"] = h.ToCurrency
	}
	if h.Network != "" {
		req["network"] = h.Network
	}
	p, err := h.call(ctx, "/payment", req)
	if err != nil {
		return Invoice{}, err
	}
	if p.UUID == "" || p.URL == "" {
		return Invoice{}, fmt.Errorf("heleket: no uuid/url in the response")
	}
	return Invoice{ExternalID: p.UUID, URL: p.URL, Amount: in.Amount, Currency: in.Currency}, nil
}

func (h *Heleket) Status(ctx context.Context, externalID string) (Event, error) {
	p, err := h.call(ctx, "/payment/info", map[string]any{"uuid": externalID})
	return p.event(), err
}

// withoutSign is the webhook body minus the "sign" member: compact, members in
// their original order, each value in its original text (so \uXXXX escapes and
// number formats survive), which is what the sender signed up to slash escaping.
func withoutSign(body []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return "", errors.New("not an object")
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return "", err
		}
		key, ok := kt.(string)
		if !ok {
			return "", errors.New("bad key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", err
		}
		if key == "sign" {
			continue
		}
		var v bytes.Buffer
		if err := json.Compact(&v, raw); err != nil {
			return "", err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v.Bytes())
	}
	b.WriteByte('}')
	return b.String(), nil
}

// validSign accepts the signature over the escaped-slash form (PHP's default,
// what Bedolaga computes) and over the plain-slash form.
func (h *Heleket) validSign(body []byte, got string) bool {
	if got == "" || h.APIKey == "" {
		return false
	}
	rest, err := withoutSign(body)
	if err != nil {
		return false
	}
	plain := strings.ReplaceAll(rest, `\/`, `/`)
	for _, signed := range []string{strings.ReplaceAll(plain, `/`, `\/`), plain} {
		if hmac.Equal([]byte(strings.ToLower(got)), []byte(hkSign([]byte(signed), h.APIKey))) {
			return true
		}
	}
	return false
}

func (h *Heleket) Webhook(_ context.Context, _ http.Header, body []byte) (Event, bool, error) {
	var p hkPayment
	var s struct {
		Sign string `json:"sign"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	_ = json.Unmarshal(body, &s)
	if !h.validSign(body, s.Sign) {
		return Event{}, false, ErrBadSignature
	}
	if p.Type != "" && p.Type != "payment" {
		return Event{}, false, nil // e.g. payout notifications
	}
	ev := p.event()
	return ev, ev.Paid || ev.Review, nil
}
