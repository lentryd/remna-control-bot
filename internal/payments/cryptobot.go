package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CryptoBot (Crypto Pay API): invoices are issued in fiat (RUB); the customer
// pays in USDT/TON/… and CryptoBot converts.
type CryptoBot struct {
	Token   string
	Testnet bool
	Assets  string // accepted assets, e.g. "USDT,TON"; empty: all
	BaseURL string // tests
	Client  *http.Client
}

var (
	_ WebhookProvider = (*CryptoBot)(nil)
	_ Poller          = (*CryptoBot)(nil)
)

func (c *CryptoBot) ID() string    { return "cryptobot" }
func (c *CryptoBot) Title() string { return "Криптовалюта (CryptoBot)" }

func (c *CryptoBot) base() string {
	switch {
	case c.BaseURL != "":
		return c.BaseURL
	case c.Testnet:
		return "https://testnet-pay.crypt.bot/api"
	}
	return "https://pay.crypt.bot/api"
}

type cbInvoice struct {
	InvoiceID     int64  `json:"invoice_id"`
	Status        string `json:"status"`
	Fiat          string `json:"fiat"`
	Asset         string `json:"asset"`
	Amount        string `json:"amount"`
	Payload       string `json:"payload"`
	BotInvoiceURL string `json:"bot_invoice_url"`
}

func (i cbInvoice) event() Event {
	amount, _ := strconv.ParseFloat(i.Amount, 64)
	cur := i.Fiat
	if cur == "" {
		cur = i.Asset
	}
	return Event{Provider: "cryptobot", OrderID: i.Payload, ExternalID: strconv.FormatInt(i.InvoiceID, 10),
		Paid: i.Status == "paid", Amount: amount, Currency: cur}
}

type cbReply[T any] struct {
	OK     bool `json:"ok"`
	Result T    `json:"result"`
	Error  any  `json:"error"`
}

func (c *CryptoBot) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Crypto-Pay-API-Token", c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pick(c.Client).Do(req)
	if err != nil {
		return fmt.Errorf("cryptobot: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("cryptobot: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"cryptobot", resp.StatusCode, clip(string(data), 300)}
	}
	return json.Unmarshal(data, out)
}

func (c *CryptoBot) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	req := map[string]any{
		"currency_type": "fiat", "fiat": "RUB", "amount": money(in.Amount),
		"description": clip(in.Title, 1024), "payload": in.OrderID,
	}
	if secs := int(in.ExpiresIn.Seconds()); secs > 0 {
		req["expires_in"] = min(secs, 2678400) // the API's maximum is 28 days
	}
	if c.Assets != "" {
		req["accepted_assets"] = c.Assets
	}
	var r cbReply[cbInvoice]
	if err := c.call(ctx, http.MethodPost, "/createInvoice", req, &r); err != nil {
		return Invoice{}, err
	}
	if !r.OK || r.Result.InvoiceID == 0 || r.Result.BotInvoiceURL == "" {
		return Invoice{}, fmt.Errorf("cryptobot: createInvoice failed: %v", r.Error)
	}
	return Invoice{ExternalID: strconv.FormatInt(r.Result.InvoiceID, 10), URL: r.Result.BotInvoiceURL, Amount: in.Amount, Currency: "RUB"}, nil
}

func (c *CryptoBot) Status(ctx context.Context, externalID string) (Event, error) {
	var r cbReply[struct {
		Items []cbInvoice `json:"items"`
	}]
	if err := c.call(ctx, http.MethodGet, "/getInvoices?"+url.Values{"invoice_ids": {externalID}}.Encode(), nil, &r); err != nil {
		return Event{}, err
	}
	if !r.OK || len(r.Result.Items) == 0 {
		return Event{}, fmt.Errorf("cryptobot: invoice %s not found", externalID)
	}
	return r.Result.Items[0].event(), nil
}

// validSignature: HMAC-SHA256 of the raw body, key = SHA256(api token) (Crypto Pay docs).
func (c *CryptoBot) validSignature(sig string, body []byte) bool {
	if sig == "" || c.Token == "" {
		return false
	}
	key := sha256.Sum256([]byte(c.Token))
	m := hmac.New(sha256.New, key[:])
	m.Write(body)
	return hmac.Equal([]byte(strings.ToLower(sig)), []byte(hex.EncodeToString(m.Sum(nil))))
}

func (c *CryptoBot) Webhook(_ context.Context, h http.Header, body []byte) (Event, bool, error) {
	if !c.validSignature(h.Get("crypto-pay-api-signature"), body) {
		return Event{}, false, ErrBadSignature
	}
	var u struct {
		UpdateType string    `json:"update_type"`
		Payload    cbInvoice `json:"payload"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	if u.UpdateType != "invoice_paid" {
		return Event{}, false, nil
	}
	return u.Payload.event(), true, nil
}
