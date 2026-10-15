package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const yooKassaAPI = "https://api.yookassa.ru/v3"

// YooKassa: cards and SBP. Invoices are issued in the order's base currency
// (the shop must accept it). The webhook body is never trusted — the
// payment is re-read from the API (Bedolaga does the same: its IP check is only
// a first barrier, the API read is the authority).
type YooKassa struct {
	ShopID, Secret string
	ReturnURL      string
	ReceiptEmail   string // fiscal receipt customer; no receipt when empty
	VAT            int
	BaseURL        string // tests; default https://api.yookassa.ru/v3
	Client         *http.Client
}

var (
	_ WebhookProvider = (*YooKassa)(nil)
	_ Poller          = (*YooKassa)(nil)
)

func (y *YooKassa) ID() string    { return "yookassa" }
func (y *YooKassa) Title() string { return "Card / bank transfer" }

type ykPayment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Paid   bool   `json:"paid"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Metadata     map[string]string `json:"metadata"`
	Confirmation struct {
		URL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

func (p ykPayment) event() Event {
	v, _ := strconv.ParseFloat(p.Amount.Value, 64)
	return Event{Provider: "yookassa", OrderID: p.Metadata["order_id"], ExternalID: p.ID,
		Paid: p.Status == "succeeded" && p.Paid, Amount: v, Currency: p.Amount.Currency}
}

func (y *YooKassa) do(ctx context.Context, method, path, idempotence string, in, out any) error {
	base := y.BaseURL
	if base == "" {
		base = yooKassaAPI
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(y.ShopID, y.Secret)
	req.Header.Set("Content-Type", "application/json")
	if idempotence != "" {
		req.Header.Set("Idempotence-Key", idempotence)
	}
	resp, err := pick(y.Client).Do(req)
	if err != nil {
		return fmt.Errorf("yookassa: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("yookassa: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"yookassa", resp.StatusCode, clip(string(data), 300)}
	}
	return json.Unmarshal(data, out)
}

func (y *YooKassa) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if err := checkCurrency(in.Currency); err != nil {
		return Invoice{}, fmt.Errorf("yookassa: %w", err)
	}
	amount := map[string]string{"value": money(in.Amount), "currency": in.Currency}
	req := map[string]any{
		"amount":       amount,
		"capture":      true,
		"confirmation": map[string]string{"type": "redirect", "return_url": y.ReturnURL},
		"description":  clip(in.Title, 128),
		"metadata":     map[string]string{"order_id": in.OrderID},
	}
	if y.ReceiptEmail != "" {
		req["receipt"] = map[string]any{
			"customer": map[string]string{"email": y.ReceiptEmail},
			"items": []map[string]any{{
				"description": clip(in.Title, 128), "quantity": "1.00", "amount": amount,
				"vat_code": y.VAT, "payment_mode": "full_payment", "payment_subject": "service",
			}},
		}
	}
	var p ykPayment
	// The order id as the idempotence key: re-issuing the same order returns the same payment.
	if err := y.do(ctx, http.MethodPost, "/payments", in.OrderID, req, &p); err != nil {
		return Invoice{}, err
	}
	if p.ID == "" || p.Confirmation.URL == "" {
		return Invoice{}, fmt.Errorf("yookassa: no confirmation url in the response")
	}
	return Invoice{ExternalID: p.ID, URL: p.Confirmation.URL, Amount: in.Amount, Currency: in.Currency}, nil
}

func (y *YooKassa) get(ctx context.Context, id string) (ykPayment, error) {
	var p ykPayment
	// The id may come from an unauthenticated webhook body: keep it one path segment.
	err := y.do(ctx, http.MethodGet, "/payments/"+url.PathEscape(id), "", nil, &p)
	return p, err
}

func (y *YooKassa) Status(ctx context.Context, externalID string) (Event, error) {
	p, err := y.get(ctx, externalID)
	return p.event(), err
}

func (y *YooKassa) Webhook(ctx context.Context, _ http.Header, body []byte) (Event, bool, error) {
	var n struct {
		Event  string `json:"event"`
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	if n.Event != "payment.succeeded" {
		return Event{}, false, nil
	}
	if n.Object.ID == "" {
		return Event{}, false, fmt.Errorf("%w: no payment id", ErrBadBody)
	}
	p, err := y.get(ctx, n.Object.ID) // the authority: ignore everything else in the body
	if err != nil {
		return Event{}, false, err
	}
	ev := p.event()
	return ev, ev.Paid, nil
}

var _ Checker = (*YooKassa)(nil)

// Check reads the shop's own account (GET /me), which needs valid credentials and moves no money.
func (y *YooKassa) Check(ctx context.Context) (string, error) {
	var r struct {
		AccountID string `json:"account_id"`
		Status    string `json:"status"`
	}
	if err := y.do(ctx, http.MethodGet, "/me", "", nil, &r); err != nil {
		return "", err
	}
	return "shop " + r.AccountID + " (" + r.Status + ")", nil
}
