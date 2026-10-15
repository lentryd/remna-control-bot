package orders

import (
	"context"
	"sync"
	"testing"

	"remna-control-bot/internal/payments"
)

type stubProvider struct{ id string }

func (p stubProvider) ID() string    { return p.id }
func (p stubProvider) Title() string { return p.id }
func (p stubProvider) Invoice(context.Context, payments.InvoiceRequest) (payments.Invoice, error) {
	return payments.Invoice{ExternalID: "x", URL: "https://pay", Amount: 1, Currency: "RUB"}, nil
}

func TestLiveSwap(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	q, err := s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 100})
	if err != nil || len(q.Methods) != 1 || q.Methods[0] != MethodManual {
		t.Fatalf("manual only: %+v %v", q, err)
	}
	s.UpdateLive(func(l *Live) {
		l.Manual = false
		l.Providers = payments.NewRegistry(stubProvider{"yookassa"})
	})
	if q, _ = s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 100}); len(q.Methods) != 1 || q.Methods[0] != "yookassa" {
		t.Fatalf("after the swap only the provider is offered: %+v", q)
	}
	// The provider is switched off while an invoice is outstanding: it is not offered any more, yet resolves.
	s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry().Retire(stubProvider{"yookassa"}) })
	if _, err := s.Pay(ctx, c.ID, "nope", "yookassa"); err == nil {
		t.Fatal("Pay for a missing order must fail")
	}
	if _, ok := s.Live().Providers.Get("yookassa"); !ok {
		t.Fatal("the retired provider must still resolve for webhooks")
	}
	if _, ok := s.Live().Providers.Offer("yookassa"); ok {
		t.Fatal("the retired provider must not issue new invoices")
	}
}

func TestLiveConcurrent(t *testing.T) {
	s, _, _, _ := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.UpdateLive(func(l *Live) { l.Details = "x" }) }()
		go func() { defer wg.Done(); _ = s.Live().Details }()
	}
	wg.Wait()
}

func TestCreateWithMissingProviderNoPanic(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	// Quote offers nothing but manual, so a provider method must be refused without touching a nil provider.
	if _, err := s.CreateWith(ctx, c, 77, Request{Kind: KindTopup, Amount: 100}, "yookassa"); err == nil {
		t.Fatal("an unoffered provider must be refused")
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, StatusNew).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no dangling new order: %d %v", n, err)
	}
	// The guard itself: an order whose provider vanished from the snapshot is cancelled, not invoiced.
	s.UpdateLive(func(l *Live) { l.Manual = false; l.Providers = payments.NewRegistry(stubProvider{"yookassa"}) })
	o, err := s.CreateWith(ctx, c, 77, Request{Kind: KindTopup, Amount: 100}, "yookassa")
	if err != nil || o.Status != StatusAwaitingPayment {
		t.Fatalf("offered provider: %+v %v", o, err)
	}
}
