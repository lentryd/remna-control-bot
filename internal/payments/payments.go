// Package payments talks to online payment providers: it issues invoices,
// verifies the providers' webhooks and polls payment status. It never moves
// money in the panel — internal/orders does that after an Event says money
// arrived — so this package does not import orders.
package payments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrBadSignature = errors.New("payments: bad signature")
	ErrBadBody      = errors.New("payments: bad webhook body")
)

// InvoiceRequest is what a provider needs to bill an order.
type InvoiceRequest struct {
	OrderID    string // our order UUID; comes back in the webhook
	Title      string
	Amount     float64 // in Currency
	Currency   string  // ISO 4217 code of Amount: the base currency of the panel
	ExpiresIn  time.Duration
	CustomerID int // the bot's customer id, for providers that require a payer identity
}

// Invoice is the provider's answer: where to send the customer and what it asks.
type Invoice struct {
	ExternalID string
	URL        string
	Amount     float64 // in Currency (the order's base currency for fiat providers, stars for Stars)
	Currency   string  // the base currency ("RUB", "USD", …) or "XTR"
}

// Event is a provider's statement about one invoice.
type Event struct {
	Provider   string
	OrderID    string
	ExternalID string
	Paid       bool    // money arrived
	Review     bool    // the provider flags a wrong amount: a human decides
	Amount     float64 // in the invoice's own unit
	Currency   string
	// Fixed: the provider vouches that the invoice was paid as issued but does not report the amount and/or
	// currency (so Amount/Currency are not compared); set it only for that reason, never to skip a check on
	// data that was reported.
	Fixed bool
}

type Provider interface {
	ID() string // "yookassa"
	Title() string
	Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error)
}

// WebhookProvider receives the provider's callbacks. relevant=false means a
// valid message that needs no action (a status other than paid).
type WebhookProvider interface {
	Provider
	Webhook(ctx context.Context, h http.Header, body []byte) (ev Event, relevant bool, err error)
}

// Poller can be asked about an invoice (lost-webhook recovery).
type Poller interface {
	Provider
	Status(ctx context.Context, externalID string) (Event, error)
}

// Registry is the set of enabled providers, in registration order. All
// methods are safe on a nil Registry.
type Registry struct{ list []Provider }

func NewRegistry(ps ...Provider) *Registry { return &Registry{list: ps} }

func (r *Registry) Get(id string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	for _, p := range r.list {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

func (r *Registry) Enabled() []Provider {
	if r == nil {
		return nil
	}
	return r.list
}

func (r *Registry) IDs() []string {
	var out []string
	for _, p := range r.Enabled() {
		out = append(out, p.ID())
	}
	return out
}

// Title is the customer-facing name of a provider ("" when unknown).
func (r *Registry) Title(id string) string {
	if p, ok := r.Get(id); ok {
		return p.Title()
	}
	return ""
}

// defaultClient bounds every provider call: a slow provider must not hold an order.
var defaultClient = &http.Client{Timeout: 30 * time.Second}

func pick(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return defaultClient
}

// apiError is a non-2xx answer from a provider.
type apiError struct {
	Provider string
	Status   int
	Body     string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Body)
}

func readBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// checkCurrency accepts exactly three upper-case ASCII letters (an ISO 4217
// code); providers must not be sent anything else as an invoice currency.
func checkCurrency(c string) error {
	if len(c) != 3 {
		return fmt.Errorf("payments: bad currency %q", c)
	}
	for i := 0; i < 3; i++ {
		if c[i] < 'A' || c[i] > 'Z' {
			return fmt.Errorf("payments: bad currency %q", c)
		}
	}
	return nil
}

func money(v float64) string { return fmt.Sprintf("%.2f", v) }
