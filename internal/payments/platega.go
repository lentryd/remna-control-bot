package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const plategaAPI = "https://app.platega.io"

// Platega: SBP / cards / crypto. Invoices are issued in the order's base currency. The webhook is
// authenticated by the merchant id and secret it carries in headers (compared in constant time),
// but its body is still never trusted: the transaction is re-read from the API, which is the
// authority on status, amount and order.
type Platega struct {
	MerchantID, Secret string
	PaymentMethod      int    // 2 = SBP QR, 11 = cards, 12 = international cards, 13 = crypto
	APIVersion         string // "v1" (default when empty) or "v2"
	ReturnURL          string
	FailedURL          string
	BaseURL            string // tests; default https://app.platega.io
	Client             *http.Client
}

var (
	_ WebhookProvider = (*Platega)(nil)
	_ Poller          = (*Platega)(nil)
)

func (p *Platega) ID() string    { return "platega" }
func (p *Platega) Title() string { return "СБП / карта (Platega)" }

// plategaID is what a transaction id may look like. The webhook's id is attacker-controlled until
// the headers are checked and is used in a credentialed request path, so anything else is refused.
var plategaID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func (p *Platega) call(ctx context.Context, method, path string, in, out any) error {
	base := p.BaseURL
	if base == "" {
		base = plategaAPI
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return fmt.Errorf("platega: %w", err)
	}
	req.Header.Set("X-MerchantId", p.MerchantID)
	req.Header.Set("X-Secret", p.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pick(p.Client).Do(req)
	if err != nil {
		// url.Error carries only the URL, never the headers; the secret is not in the URL.
		return fmt.Errorf("platega: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("platega: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"platega", resp.StatusCode, clip(string(data), 300)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("platega: bad response: %w", err)
	}
	return nil
}

// pgTx is a transaction as the API reports it; numbers and strings are accepted for the money fields.
type pgTx struct {
	ID            looseValue `json:"id"`
	TransactionID looseValue `json:"transactionId"`
	Status        looseValue `json:"status"`
	Payload       looseValue `json:"payload"`
	Redirect      looseValue `json:"redirect"`
	URL           looseValue `json:"url"`
	Amount        looseValue `json:"amount"`
	Currency      looseValue `json:"currency"`
	Details       *struct {
		Amount   looseValue `json:"amount"`
		Currency looseValue `json:"currency"`
	} `json:"paymentDetails"`
}

func (t pgTx) id() string {
	if s := t.TransactionID.str(); s != "" {
		return s
	}
	return t.ID.str()
}

func (t pgTx) link() string {
	if s := t.Redirect.str(); s != "" {
		return s
	}
	return t.URL.str()
}

func (t pgTx) confirmed() bool { return strings.EqualFold(t.Status.str(), "CONFIRMED") }

// money is the reported amount and currency ("" when not reported); paymentDetails wins over the
// top level. An amount that is present but is not a finite number is an error, never "not reported".
func (t pgTx) money() (amount float64, text, currency string, err error) {
	var raw looseValue
	if t.Details != nil {
		raw, currency = t.Details.Amount, t.Details.Currency.str()
	}
	if raw.str() == "" && !raw.compound() {
		raw = t.Amount
	}
	if currency == "" {
		currency = t.Currency.str()
	}
	text = raw.str()
	if text == "" && !raw.compound() {
		return 0, "", currency, nil
	}
	v, perr := strconv.ParseFloat(text, 64)
	if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, "", "", fmt.Errorf("platega: bad amount %q in the transaction", clip(text, 40))
	}
	return v, text, currency, nil
}

// event is the transaction as an Event for the given id (the one the API was asked about).
//
// The order id is the API's payload; the callback's payload stands in only when the API has none. That
// is safe: orders.Paid rejects an event whose external id differs from the transaction id stored on the
// order, so a swapped order id cannot be paid with somebody else's transaction.
func (t pgTx) event(id, fallbackOrder string) (Event, error) {
	amount, text, currency, err := t.money()
	if err != nil {
		return Event{}, err
	}
	ev := Event{Provider: "platega", OrderID: t.Payload.str(), ExternalID: id, Paid: t.confirmed(),
		Amount: amount, Currency: currency}
	if ev.OrderID == "" {
		ev.OrderID = fallbackOrder
	}
	// Fixed only when the API reports neither amount nor currency. A partial report (one without the
	// other) is deliberately not Fixed: orders.Paid sees a mismatch and sends it to an admin.
	ev.Fixed = ev.Paid && text == "" && currency == ""
	return ev, nil
}

func (p *Platega) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if err := checkCurrency(in.Currency); err != nil {
		return Invoice{}, fmt.Errorf("platega: %w", err)
	}
	who := strconv.Itoa(in.CustomerID)
	if in.CustomerID == 0 {
		who = in.OrderID
	}
	req := map[string]any{
		"paymentMethod": p.PaymentMethod,
		"paymentDetails": map[string]any{
			"amount":   json.Number(strconv.FormatFloat(math.Round(in.Amount*100)/100, 'f', -1, 64)),
			"currency": in.Currency,
		},
		"description": clip(in.Title, 64),
		"payload":     in.OrderID,
		"metadata":    map[string]string{"userId": "c" + who, "userName": "Customer " + who},
	}
	if p.ReturnURL != "" {
		req["return"] = p.ReturnURL
	}
	if p.FailedURL != "" {
		req["failedUrl"] = p.FailedURL
	}
	path := "/transaction/process"
	if strings.EqualFold(p.APIVersion, "v2") {
		path = "/v2/transaction/process"
	}
	var tx pgTx
	if err := p.call(ctx, http.MethodPost, path, req, &tx); err != nil {
		return Invoice{}, err
	}
	id, link := tx.id(), tx.link()
	if id == "" {
		return Invoice{}, fmt.Errorf("platega: no transaction id in the response")
	}
	if link == "" {
		return Invoice{}, fmt.Errorf("platega: no payment link in the response")
	}
	return Invoice{ExternalID: id, URL: link, Amount: in.Amount, Currency: in.Currency}, nil
}

// get reads a transaction. The id may come from a webhook body, so it is validated first and then
// kept to one path segment.
func (p *Platega) get(ctx context.Context, id string) (pgTx, error) {
	var tx pgTx
	if !plategaID.MatchString(id) {
		return tx, fmt.Errorf("platega: bad transaction id")
	}
	err := p.call(ctx, http.MethodGet, "/transaction/"+url.PathEscape(id), nil, &tx)
	return tx, err
}

func (p *Platega) Status(ctx context.Context, externalID string) (Event, error) {
	tx, err := p.get(ctx, externalID)
	if err != nil {
		return Event{}, err
	}
	return tx.event(externalID, "")
}

// authentic compares the callback's credentials with ours in constant time; both are always
// compared, and an unconfigured merchant id or secret never matches.
func (p *Platega) authentic(h http.Header) bool {
	if p.MerchantID == "" || p.Secret == "" {
		return false
	}
	m := hmac.Equal([]byte(h.Get("X-MerchantId")), []byte(p.MerchantID))
	s := hmac.Equal([]byte(h.Get("X-Secret")), []byte(p.Secret))
	return m && s
}

func (p *Platega) Webhook(ctx context.Context, h http.Header, body []byte) (Event, bool, error) {
	if h.Get("X-MerchantId") == "" && h.Get("X-Secret") == "" && len(bytes.TrimSpace(body)) == 0 {
		return Event{}, false, nil // the verification ping carries neither credentials nor a body
	}
	if !p.authentic(h) {
		return Event{}, false, ErrBadSignature
	}
	var n struct {
		ID            looseValue `json:"id"`
		TransactionID looseValue `json:"transactionId"`
		Status        looseValue `json:"status"`
		Payload       looseValue `json:"payload"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	if !strings.EqualFold(n.Status.str(), "CONFIRMED") {
		return Event{}, false, nil
	}
	id := n.ID.str()
	if id == "" {
		id = n.TransactionID.str()
	}
	if id == "" {
		return Event{}, false, fmt.Errorf("%w: no transaction id", ErrBadBody)
	}
	tx, err := p.get(ctx, id) // the authority: ignore everything else in the body
	if err != nil {
		return Event{}, false, err
	}
	// The API must be talking about the transaction the callback named.
	for _, got := range []string{tx.ID.str(), tx.TransactionID.str()} {
		if got != "" && got != id {
			return Event{}, false, nil
		}
	}
	ev, err := tx.event(id, n.Payload.str())
	if err != nil {
		return Event{}, false, err
	}
	return ev, ev.Paid, nil
}
