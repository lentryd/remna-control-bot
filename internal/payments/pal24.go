package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const pal24API = "https://pal24.pro/api/v1/"

// Pal24 is PayPalych (SBP / cards). Invoices ("bills") are issued in the order's base currency.
// The postback carries an MD5 signature over OutSum:InvId:token that is verified before anything else in
// the body is interpreted, but it does not cover the status: it is only a pre-filter, and a payment is
// confirmed from the bill itself (see Webhook).
type Pal24 struct {
	APIToken, ShopID string
	SignatureToken   string // postback signing token; empty: APIToken
	BaseURL          string // tests; default https://pal24.pro/api/v1/
	Client           *http.Client
}

var (
	_ WebhookProvider = (*Pal24)(nil)
	_ Poller          = (*Pal24)(nil)
)

func (p *Pal24) ID() string    { return "pal24" }
func (p *Pal24) Title() string { return "Bank transfer / card (PayPalych)" }

// pal24MaxTTL is the longest bill lifetime requested, in seconds (30 days).
const pal24MaxTTL = 30 * 24 * 3600

func (p *Pal24) endpoint(path string) string {
	base := p.BaseURL
	if base == "" {
		base = pal24API
	}
	return strings.TrimRight(base, "/") + "/" + path
}

func (p *Pal24) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.endpoint(path), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := pick(p.Client).Do(req)
	if err != nil {
		// url.Error carries only the URL, never the headers; the token is not in the URL.
		return fmt.Errorf("pal24: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("pal24: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"pal24", resp.StatusCode, clip(string(data), 300)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("pal24: bad response: %w", err)
	}
	return nil
}

type p24Bill struct {
	Status     looseValue `json:"status"`
	Amount     looseValue `json:"amount"`
	CurrencyIn looseValue `json:"currency_in"`
	OrderID    looseValue `json:"order_id"`
}

type p24Reply struct {
	Success     looseValue `json:"success"`
	Message     looseValue `json:"message"`
	BillID      looseValue `json:"bill_id"`
	LinkURL     looseValue `json:"link_url"`
	LinkPageURL looseValue `json:"link_page_url"`
	p24Bill                // top-level status shape
	Bill        *p24Bill   `json:"bill"` // nested status shape
}

func (p *Pal24) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if err := checkCurrency(in.Currency); err != nil {
		return Invoice{}, fmt.Errorf("pal24: %w", err)
	}
	req := map[string]any{
		"amount":      money(in.Amount),
		"shop_id":     p.ShopID,
		"order_id":    in.OrderID,
		"description": clip(in.Title, 255),
		"type":        "normal",
		"currency_in": in.Currency,
	}
	if secs := int(in.ExpiresIn.Seconds()); secs > 0 {
		req["ttl"] = min(secs, pal24MaxTTL)
	}
	var r p24Reply
	if err := p.call(ctx, http.MethodPost, "bill/create", req, &r); err != nil {
		return Invoice{}, err
	}
	if r.Success.failed() {
		return Invoice{}, fmt.Errorf("pal24: bill/create failed: %s", clip(r.Message.str(), 200))
	}
	link := r.LinkPageURL.str()
	if link == "" {
		link = r.LinkURL.str()
	}
	if r.BillID.str() == "" || link == "" {
		return Invoice{}, fmt.Errorf("pal24: no bill id or link in the response")
	}
	return Invoice{ExternalID: r.BillID.str(), URL: link, Amount: in.Amount, Currency: in.Currency}, nil
}

// pal24Outcome maps a bill status to paid / needs-a-human.
func pal24Outcome(status string) (paid, review bool) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "OVERPAID":
		return true, false
	case "UNDERPAID":
		return false, true
	}
	return false, false
}

func (p *Pal24) Status(ctx context.Context, externalID string) (Event, error) {
	var r p24Reply
	if err := p.call(ctx, http.MethodGet, "bill/status?"+url.Values{"id": {externalID}}.Encode(), nil, &r); err != nil {
		return Event{}, err
	}
	if r.Success.failed() {
		return Event{}, fmt.Errorf("pal24: bill/status failed: %s", clip(r.Message.str(), 200))
	}
	b := r.p24Bill
	if r.Bill != nil {
		// Top-level fields win; the nested bill fills the gaps.
		for _, f := range []struct {
			dst *looseValue
			src looseValue
		}{
			{&b.Status, r.Bill.Status}, {&b.Amount, r.Bill.Amount},
			{&b.CurrencyIn, r.Bill.CurrencyIn}, {&b.OrderID, r.Bill.OrderID},
		} {
			if f.dst.str() == "" {
				*f.dst = f.src
			}
		}
	}
	paid, review := pal24Outcome(b.Status.str())
	ev := Event{Provider: "pal24", OrderID: b.OrderID.str(), ExternalID: externalID, Paid: paid, Review: review,
		Currency: strings.ToUpper(b.CurrencyIn.str())}
	if s := b.Amount.str(); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Event{}, fmt.Errorf("pal24: bad amount %q in the status", clip(s, 40))
		}
		ev.Amount = v
	}
	// A paid bill that reports neither amount nor currency: the provider vouches for the bill as issued.
	ev.Fixed = paid && b.Amount.str() == "" && ev.Currency == ""
	return ev, nil
}

// p24Parse reads a postback body, JSON object or form, into flat strings. JSON numbers keep their
// original text (the signature is computed over the text as sent). A key sent more than once keeps its
// first value and is listed in dups: the caller decides what a repeated key means.
func p24Parse(body []byte) (fields map[string]string, dups map[string]bool, err error) {
	trimmed := bytes.TrimSpace(body)
	fields, dups = map[string]string{}, map[string]bool{}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil, nil, fmt.Errorf("not a JSON object")
		}
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return nil, nil, err
			}
			key, ok := tok.(string)
			if !ok {
				return nil, nil, fmt.Errorf("bad JSON key")
			}
			var v any
			if err := dec.Decode(&v); err != nil {
				return nil, nil, err
			}
			if seen[key] {
				dups[key] = true
				continue
			}
			seen[key] = true
			switch t := v.(type) { // only scalars count as values; anything else leaves the key absent
			case string:
				fields[key] = t
			case json.Number:
				fields[key] = t.String()
			case bool:
				fields[key] = strconv.FormatBool(t)
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, nil, err
		}
		return fields, dups, nil
	}
	q, err := url.ParseQuery(string(trimmed))
	if err != nil {
		return nil, nil, err
	}
	for k, vs := range q {
		fields[k] = vs[0]
		dups[k] = len(vs) > 1
	}
	return fields, dups, nil
}

// validSignature: UPPER(hex(md5("OutSum:InvId:token"))), compared in constant time.
func (p *Pal24) validSignature(sig, outSum, invID string) bool {
	token := p.SignatureToken
	if token == "" {
		token = p.APIToken
	}
	if sig == "" || token == "" {
		return false
	}
	sum := md5.Sum([]byte(outSum + ":" + invID + ":" + token))
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	return hmac.Equal([]byte(strings.ToUpper(strings.TrimSpace(sig))), []byte(want))
}

// Webhook handles a PayPalych postback. The MD5 signature (OutSum:InvId:token) does not cover Status,
// bill_id or CurrencyIn, and the same signed values are shown to the customer in the browser redirect, so
// it is only a pre-filter: a paid status in a postback is never taken at its word. Every confirmation is
// re-read from the bill (bill/status), which is the authority.
//
// A postback without a bill_id cannot be checked here and is ignored; the one-minute SweepProviders loop
// confirms the payment through the stored bill id, so in that case it lands within about a minute.
func (p *Pal24) Webhook(ctx context.Context, _ http.Header, body []byte) (Event, bool, error) {
	f, dups, err := p24Parse(body)
	if err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	invID, outSum, status := strings.TrimSpace(f["InvId"]), strings.TrimSpace(f["OutSum"]), strings.TrimSpace(f["Status"])
	sig, hasSig := f["SignatureValue"]
	if invID == "" || outSum == "" || status == "" || !hasSig {
		return Event{}, false, fmt.Errorf("%w: missing InvId, OutSum, Status or SignatureValue", ErrBadBody)
	}
	if _, err := strconv.ParseFloat(outSum, 64); err != nil {
		return Event{}, false, fmt.Errorf("%w: bad OutSum", ErrBadBody)
	}
	// The signature first: nothing below is interpreted until it holds.
	if !p.validSignature(strings.TrimSpace(sig), f["OutSum"], f["InvId"]) {
		return Event{}, false, ErrBadSignature
	}
	for _, k := range []string{"InvId", "OutSum", "SignatureValue", "Status", "bill_id"} {
		if dups[k] {
			return Event{}, false, fmt.Errorf("%w: repeated %s", ErrBadBody, k)
		}
	}
	if paid, review := pal24Outcome(status); !paid && !review {
		return Event{}, false, nil
	}
	billID := strings.TrimSpace(f["bill_id"])
	if billID == "" {
		return Event{}, false, nil
	}
	ev, err := p.Status(ctx, billID)
	if err != nil {
		return Event{}, false, err // Pal24 retries the postback
	}
	if ev.OrderID == "" {
		ev.OrderID = invID
	}
	return ev, ev.Paid || ev.Review, nil
}
