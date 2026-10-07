package payments

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Fake is an in-memory provider for the tests of packages that use providers.
type Fake struct {
	Name      string // default "fake"
	Currency  string // invoice currency, default "RUB"
	Err       error  // Invoice fails with it
	StatusErr error  // Status fails with it
	Statuses  map[string]Event
	Hook      func(h http.Header, body []byte) (Event, bool, error)

	mu          sync.Mutex
	Invoices    []InvoiceRequest
	StatusCalls int // how many times Status was asked
}

var (
	_ WebhookProvider = (*Fake)(nil)
	_ Poller          = (*Fake)(nil)
)

func (f *Fake) ID() string {
	if f.Name == "" {
		return "fake"
	}
	return f.Name
}

func (f *Fake) Title() string { return "Fake pay" }

func (f *Fake) Invoice(_ context.Context, in InvoiceRequest) (Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return Invoice{}, f.Err
	}
	f.Invoices = append(f.Invoices, in)
	cur := f.Currency
	if cur == "" {
		cur = "RUB"
	}
	return Invoice{ExternalID: fmt.Sprintf("ext-%d", len(f.Invoices)), URL: "https://pay.test/" + in.OrderID, Amount: in.Amount, Currency: cur}, nil
}

func (f *Fake) Webhook(_ context.Context, h http.Header, body []byte) (Event, bool, error) {
	if f.Hook == nil {
		return Event{}, false, nil
	}
	return f.Hook(h, body)
}

// Status returns the configured result; an unknown invoice is still unpaid.
func (f *Fake) Status(_ context.Context, externalID string) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StatusCalls++
	if f.StatusErr != nil {
		return Event{}, f.StatusErr
	}
	return f.Statuses[externalID], nil
}
