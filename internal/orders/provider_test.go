package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

func setupP(t *testing.T) (*Service, *vpnctl.Fake, *spy, *vpnctl.Customer, *payments.Fake) {
	t.Helper()
	s, f, sp, c := setup(t)
	fp := &payments.Fake{Name: "fake"}
	s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry(fp) })
	return s, f, sp, c, fp
}

var renew3 = Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3} // 900, 100 from balance → 800 to pay

func providerOrder(t *testing.T, s *Service, c *vpnctl.Customer) *Order {
	t.Helper()
	o, err := s.CreateWith(context.Background(), c, 77, renew3, "fake")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func paidEvent(o *Order) payments.Event {
	return payments.Event{Provider: "fake", OrderID: o.ID, ExternalID: o.ExternalID, Paid: true, Amount: o.PayAmount, Currency: "RUB"}
}

func TestCreateWithProvider(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	o := providerOrder(t, s, c)
	if o.Status != StatusAwaitingPayment || o.Method != "fake" || o.ExternalID != "ext-1" || o.PayURL != "https://pay.test/"+o.ID ||
		o.PayAmount != 800 || o.PayCurrency != "RUB" || o.ToPay != 800 {
		t.Fatalf("%+v", o)
	}
	if len(fp.Invoices) != 1 || fp.Invoices[0].Amount != 800 || fp.Invoices[0].OrderID != o.ID || fp.Invoices[0].ExpiresIn != 48*time.Hour || fp.Invoices[0].Title != o.Title {
		t.Fatalf("invoice request %+v", fp.Invoices)
	}
	got, err := s.Get(context.Background(), o.ID)
	if err != nil || got.ExternalID != "ext-1" || got.Status != StatusAwaitingPayment {
		t.Fatalf("persisted %+v %v", got, err)
	}
	if _, err := s.CreateWith(context.Background(), c, 77, renew3, "nope"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("unknown method: %v", err)
	}
}

func TestCreateDefaultMethods(t *testing.T) {
	s, _, _, c, _ := setupP(t)
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, renew3)
	if err != nil || o.Method != MethodManual || o.Status != StatusAwaitingReceipt {
		t.Fatalf("manual default: %+v %v", o, err)
	}
	s.UpdateLive(func(l *Live) { l.Manual = false }) // providers only: the customer must pick one
	o, err = s.Create(ctx, c, 77, renew3)
	if err != nil || o.Method != "" || o.Status != StatusNew {
		t.Fatalf("undecided: %+v %v", o, err)
	}
	s.UpdateLive(func(l *Live) { l.Providers = nil })
	if _, err := s.Create(ctx, c, 77, renew3); !errors.Is(err, ErrManualOff) {
		t.Errorf("nothing configured: %v", err)
	}
}

func TestProviderInvoiceFailureCancels(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	fp.Err = errors.New("provider is down")
	_, err := s.CreateWith(context.Background(), c, 77, renew3, "fake")
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("want ErrProvider, got %v", err)
	}
	var st Status
	if err := s.DB.QueryRow(`SELECT status FROM orders`).Scan(&st); err != nil || st != StatusCancelled {
		t.Fatalf("a failed invoice must not leave a live order: %q %v", st, err)
	}
	fp.Err = nil
	if _, err := s.CreateWith(context.Background(), c, 77, renew3, "fake"); err != nil {
		t.Fatalf("another attempt works: %v", err)
	}
}

func TestPayOnExistingOrder(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, renew3) // manual, awaiting a receipt
	if _, err := s.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Pay(ctx, c.ID, o.ID, "fake")
	if err != nil || got.Status != StatusAwaitingPayment || got.Method != "fake" || got.PayURL == "" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, ok, _ := s.Chats.GetChatState(ctx, 77); ok {
		t.Error("the receipt wait must end when the customer switches to a provider")
	}
	again, err := s.Pay(ctx, c.ID, o.ID, "fake") // idempotent: same invoice
	if err != nil || again.PayURL != got.PayURL || len(fp.Invoices) != 1 {
		t.Fatalf("repeat: %+v %v invoices=%d", again, err, len(fp.Invoices))
	}
	if _, err := s.Pay(ctx, 999, o.ID, "fake"); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else's order: %v", err)
	}
	if _, err := s.Pay(ctx, c.ID, o.ID, "zzz"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("unknown provider: %v", err)
	}
	s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry(fp, &payments.Fake{Name: "other"}) })
	if _, err := s.Pay(ctx, c.ID, o.ID, "other"); !errors.Is(err, ErrStateChanged) {
		t.Errorf("switching an issued invoice to another provider: %v", err)
	}
}

func TestPaidAppliesOnce(t *testing.T) {
	s, f, sp, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	ev := paidEvent(o)

	got, err := s.Paid(ctx, ev)
	if err != nil || got.Status != StatusApproved || got.Admin != "provider:fake" || got.Decision != DecideFull {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.Payments) != 1 || f.Payments[0].Method != "fake" || f.Payments[0].Note != "bot order #"+o.ID || f.Payments[0].Amount != 800 {
		t.Fatalf("panel payments %+v", f.Payments)
	}
	if len(sp.sent) != 1 || sp.sent[0].Status != StatusApproved {
		t.Errorf("the customer must be told once: %+v", sp.sent)
	}
	if len(sp.posted) != 0 {
		t.Errorf("a clean provider payment needs no admin card: %d", len(sp.posted))
	}
	for range 3 { // webhook retries, sweep races
		if _, err := s.Paid(ctx, ev); !errors.Is(err, ErrStateChanged) {
			t.Fatalf("replay: %v", err)
		}
	}
	if len(f.Payments) != 1 {
		t.Fatalf("replays moved money again: %d payments", len(f.Payments))
	}
}

func TestPaidTopupCreditsBalance(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	o, err := s.CreateWith(context.Background(), c, 77, Request{Kind: KindTopup, Amount: 250}, "fake")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Paid(context.Background(), paidEvent(o))
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || f.Payments[0].Amount != 250 || len(f.Payments[0].Allocations) != 0 {
		t.Fatalf("%+v %v %+v", got, err, f.Payments)
	}
}

func TestPaidAfterExpiredOrCancelled(t *testing.T) {
	for name, prep := range map[string]func(*Service, *vpnctl.Customer, *Order) error{
		"expired": func(s *Service, _ *vpnctl.Customer, o *Order) error {
			return s.move(context.Background(), o.ID, []Status{StatusAwaitingPayment}, StatusExpired, nil)
		},
		"cancelled": func(s *Service, c *vpnctl.Customer, o *Order) error {
			return s.Cancel(context.Background(), c.ID, o.ID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, f, _, c, _ := setupP(t)
			o := providerOrder(t, s, c)
			if err := prep(s, c, o); err != nil {
				t.Fatal(err)
			}
			got, err := s.Paid(context.Background(), paidEvent(o))
			if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 {
				t.Fatalf("money that arrived must be applied: %+v %v payments=%d", got, err, len(f.Payments))
			}
		})
	}
}

func TestPaidNeedsReview(t *testing.T) {
	cases := map[string]func(*payments.Event){
		"less money":     func(e *payments.Event) { e.Amount = 500 },
		"review flag":    func(e *payments.Event) { e.Paid, e.Review, e.Amount = false, true, 0 },
		"other currency": func(e *payments.Event) { e.Currency = "USDT" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			s, f, sp, c, _ := setupP(t)
			ctx := context.Background()
			o := providerOrder(t, s, c)
			ev := paidEvent(o)
			mod(&ev)
			got, err := s.Paid(ctx, ev)
			if err != nil || got.Status != StatusPendingReview {
				t.Fatalf("%+v %v", got, err)
			}
			if len(f.Payments) != 0 {
				t.Fatal("a doubtful payment must not reach the panel")
			}
			if len(sp.posted) != 1 || !strings.Contains(got.Comment, "fake") {
				t.Errorf("an admin card explaining it: posted=%d comment=%q", len(sp.posted), got.Comment)
			}
			if name == "less money" && (got.Received == nil || *got.Received != 500) {
				t.Errorf("received %v", got.Received)
			}
			if name != "less money" && got.Received != nil {
				t.Errorf("received must stay empty when the base-currency amount is unknown: %v", *got.Received)
			}
			// The admin decides with the existing flow.
			if _, err := s.Approve(ctx, o.ID, "@adm", DecideCredit); err != nil && name == "less money" {
				t.Errorf("approve as credit: %v", err)
			}
		})
	}
}

func TestPaidForeignEvents(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	for name, mod := range map[string]func(*payments.Event){
		"other provider":          func(e *payments.Event) { e.Provider = "yookassa" },
		"other invoice":           func(e *payments.Event) { e.ExternalID = "zzz" },
		"unknown order":           func(e *payments.Event) { e.OrderID = "00000000-0000-4000-8000-000000000000" },
		"neither paid nor review": func(e *payments.Event) { e.Paid = false },
	} {
		ev := paidEvent(o)
		mod(&ev)
		got, err := s.Paid(ctx, ev)
		if name == "neither paid nor review" {
			if err != nil || got != nil {
				t.Errorf("%s: %+v %v", name, got, err)
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.Payments) != 0 {
		t.Fatalf("nothing may be applied: %+v", f.Payments)
	}
	if cur, _ := s.Get(ctx, o.ID); cur.Status != StatusAwaitingPayment {
		t.Errorf("order moved: %s", cur.Status)
	}
}

func TestPaidUnmatchedAlertsAdmin(t *testing.T) {
	const unknown = "00000000-0000-4000-8000-000000000000"
	for name, mod := range map[string]func(*payments.Event){
		"other provider":      func(e *payments.Event) { e.Provider = "yookassa" },
		"other invoice":       func(e *payments.Event) { e.ExternalID = "zzz" },
		"unknown order":       func(e *payments.Event) { e.OrderID = unknown },
		"review, unknown":     func(e *payments.Event) { e.OrderID, e.Paid, e.Review = unknown, false, true },
		"review, other order": func(e *payments.Event) { e.Provider, e.Paid, e.Review = "yookassa", false, true },
	} {
		t.Run(name, func(t *testing.T) {
			s, f, sp, c, _ := setupP(t)
			o := providerOrder(t, s, c)
			ev := paidEvent(o)
			ev.Amount = 123.45
			mod(&ev)
			if _, err := s.Paid(context.Background(), ev); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if len(sp.alerts) != 1 || !strings.Contains(sp.alerts[0], ev.Provider) || !strings.Contains(sp.alerts[0], "123.45") {
				t.Fatalf("want one alert naming the provider and amount: %q", sp.alerts)
			}
			if len(f.Payments) != 0 {
				t.Errorf("nothing may be applied: %+v", f.Payments)
			}
		})
	}
	t.Run("matched payments and ignorable events stay quiet", func(t *testing.T) {
		s, _, sp, c, _ := setupP(t)
		o := providerOrder(t, s, c)
		if _, err := s.Paid(context.Background(), paidEvent(o)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Paid(context.Background(), paidEvent(o)); !errors.Is(err, ErrStateChanged) {
			t.Fatalf("replay: %v", err)
		}
		ev := paidEvent(o)
		ev.Paid, ev.OrderID = false, unknown // neither paid nor review: no money
		if _, err := s.Paid(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		if len(sp.alerts) != 0 {
			t.Errorf("unexpected alerts: %q", sp.alerts)
		}
	})
}

func TestCheckInvoice(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	fp.Currency = "XTR"
	ctx := context.Background()
	o := providerOrder(t, s, c) // invoiced 800 XTR
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "XTR"); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 78, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else's chat: %v", err)
	}
	if err := s.CheckInvoice(ctx, "other", o.ID, 77, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other provider: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 700, "XTR"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("changed amount: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "RUB"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("changed currency: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", "no-such-order", 77, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
	if err := s.Cancel(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "XTR"); !errors.Is(err, ErrStateChanged) {
		t.Errorf("cancelled order must not be payable: %v", err)
	}
}

func TestSweepPicksUpLostWebhook(t *testing.T) {
	s, f, _, c, fp := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	fp.Statuses = map[string]payments.Event{o.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}

	s.Now = func() time.Time { return now.Add(time.Minute) } // too fresh to poll
	if err := s.SweepProviders(ctx); err != nil || len(f.Payments) != 0 {
		t.Fatalf("fresh order polled: %v payments=%d", err, len(f.Payments))
	}
	s.Now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, o.ID)
	if got.Status != StatusApproved || len(f.Payments) != 1 {
		t.Fatalf("lost webhook not recovered: %s payments=%d", got.Status, len(f.Payments))
	}
	if err := s.SweepProviders(ctx); err != nil || len(f.Payments) != 1 {
		t.Fatalf("second sweep: %v payments=%d", err, len(f.Payments))
	}
}

func TestSweepExpires(t *testing.T) {
	s, f, _, c, fp := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	late := func() time.Time { return now.Add(49 * time.Hour) } // TTL is 48 h

	// A failing poll must not expire the order: the answer is unknown.
	fp.StatusErr = errors.New("provider unreachable")
	s.Now = late
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusAwaitingPayment {
		t.Fatalf("expired on an unknown answer: %s", got.Status)
	}
	// Still unpaid at the last look → expired.
	fp.StatusErr = nil
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusExpired {
		t.Fatalf("not expired: %s", got.Status)
	}
	// Paid at the last look → applied, not expired.
	o2 := providerOrder(t, s, c)
	fp.Statuses = map[string]payments.Event{o2.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}
	s.Now = func() time.Time { return now.Add(98 * time.Hour) }
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o2.ID); got.Status != StatusApproved || len(f.Payments) != 1 {
		t.Fatalf("a payment found at expiry must be applied: %s payments=%d", got.Status, len(f.Payments))
	}
}

func TestSweepCancelledOrders(t *testing.T) {
	cancelled := func(t *testing.T, s *Service, c *vpnctl.Customer) *Order {
		t.Helper()
		o := providerOrder(t, s, c)
		if err := s.Cancel(context.Background(), c.ID, o.ID); err != nil {
			t.Fatal(err)
		}
		return o
	}
	t.Run("a payment found for a cancelled order is applied", func(t *testing.T) {
		s, f, _, c, fp := setupP(t)
		o := cancelled(t, s, c)
		fp.Statuses = map[string]payments.Event{o.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}
		s.Now = func() time.Time { return now.Add(3 * time.Minute) }
		if err := s.SweepProviders(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(context.Background(), o.ID); got.Status != StatusApproved || len(f.Payments) != 1 {
			t.Fatalf("a lost webhook for a cancelled order: %s payments=%d", got.Status, len(f.Payments))
		}
	})
	t.Run("unpaid stays cancelled", func(t *testing.T) {
		s, f, _, c, fp := setupP(t)
		o := cancelled(t, s, c)
		s.Now = func() time.Time { return now.Add(3 * time.Minute) }
		if err := s.SweepProviders(context.Background()); err != nil {
			t.Fatal(err)
		}
		s.Now = func() time.Time { return now.Add(47 * time.Hour) }
		if err := s.SweepProviders(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(context.Background(), o.ID); got.Status != StatusCancelled || len(f.Payments) != 0 {
			t.Fatalf("%s payments=%d", got.Status, len(f.Payments))
		}
		if fp.StatusCalls != 2 {
			t.Errorf("a cancelled order is still polled within the TTL: %d calls", fp.StatusCalls)
		}
	})
	t.Run("a failing poll changes nothing", func(t *testing.T) {
		s, _, _, c, fp := setupP(t)
		o := cancelled(t, s, c)
		fp.StatusErr = errors.New("provider unreachable")
		s.Now = func() time.Time { return now.Add(3 * time.Minute) }
		if err := s.SweepProviders(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(context.Background(), o.ID); got.Status != StatusCancelled {
			t.Fatalf("%s", got.Status)
		}
	})
	t.Run("too fresh to poll", func(t *testing.T) {
		s, _, _, c, fp := setupP(t)
		cancelled(t, s, c)
		s.Now = func() time.Time { return now.Add(time.Minute) }
		if err := s.SweepProviders(context.Background()); err != nil || fp.StatusCalls != 0 {
			t.Fatalf("%v calls=%d", err, fp.StatusCalls)
		}
	})
	t.Run("older than the TTL is not polled", func(t *testing.T) {
		s, f, _, c, fp := setupP(t)
		o := cancelled(t, s, c)
		fp.Statuses = map[string]payments.Event{o.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}
		s.Now = func() time.Time { return now.Add(49 * time.Hour) }
		if err := s.SweepProviders(context.Background()); err != nil {
			t.Fatal(err)
		}
		if fp.StatusCalls != 0 || len(f.Payments) != 0 {
			t.Fatalf("calls=%d payments=%d", fp.StatusCalls, len(f.Payments))
		}
		if got, _ := s.Get(context.Background(), o.ID); got.Status != StatusCancelled {
			t.Fatalf("%s", got.Status)
		}
	})
	t.Run("a cancelled order that never got an invoice is not polled", func(t *testing.T) {
		s, _, _, c, fp := setupP(t)
		fp.Err = errors.New("down")
		if _, err := s.CreateWith(context.Background(), c, 77, renew3, "fake"); err == nil {
			t.Fatal("want the provider error")
		}
		s.Now = func() time.Time { return now.Add(3 * time.Minute) }
		if err := s.SweepProviders(context.Background()); err != nil || fp.StatusCalls != 0 {
			t.Fatalf("%v calls=%d", err, fp.StatusCalls)
		}
	})
}

func TestCancelAwaitingPayment(t *testing.T) {
	s, _, _, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	if err := s.Cancel(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusCancelled {
		t.Fatalf("%s", got.Status)
	}
}

var _ = store.ChatState{} // keep the import used by helpers in this file

func useUSD(s *Service) { s.BaseCurrency = func(context.Context) string { return "USD" } }

func TestInvoiceCarriesBaseCurrency(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	o := providerOrder(t, s, c)
	if len(fp.Invoices) != 1 || fp.Invoices[0].Currency != "RUB" || o.PayCurrency != "RUB" {
		t.Fatalf("default base is RUB: %+v / %q", fp.Invoices, o.PayCurrency)
	}

	s, _, _, c, fp = setupP(t)
	useUSD(s)
	o = providerOrder(t, s, c)
	if len(fp.Invoices) != 1 || fp.Invoices[0].Currency != "USD" || fp.Invoices[0].Amount != 800 || o.PayCurrency != "USD" {
		t.Fatalf("invoice request %+v, order currency %q", fp.Invoices, o.PayCurrency)
	}

	// Whatever the source returns is normalised; an empty answer falls back to RUB.
	for got, want := range map[string]string{"usd": "USD", "": "RUB"} {
		s, _, _, c, fp = setupP(t)
		s.BaseCurrency = func(context.Context) string { return got }
		providerOrder(t, s, c)
		if fp.Invoices[0].Currency != want {
			t.Errorf("source %q: invoice currency %q, want %q", got, fp.Invoices[0].Currency, want)
		}
	}
}

func TestPaidReceivedFollowsBaseCurrency(t *testing.T) {
	// base USD: an underpaid USD event records what was received, a RUB one does not.
	for cur, want := range map[string]bool{"USD": true, "RUB": false, "XTR": false} {
		s, _, _, c, _ := setupP(t)
		useUSD(s)
		o := providerOrder(t, s, c)
		ev := paidEvent(o)
		ev.Currency, ev.Amount = cur, 500
		got, err := s.Paid(context.Background(), ev)
		if err != nil || got.Status != StatusPendingReview {
			t.Fatalf("%s: %+v %v", cur, got, err)
		}
		if want != (got.Received != nil) || want && *got.Received != 500 {
			t.Errorf("base USD, %s event: received=%v, recorded want %v", cur, got.Received, want)
		}
	}
	// default base (RUB): a RUB event is recorded, as before.
	s, _, _, c, _ := setupP(t)
	o := providerOrder(t, s, c)
	ev := paidEvent(o)
	ev.Amount = 500
	got, err := s.Paid(context.Background(), ev)
	if err != nil || got.Received == nil || *got.Received != 500 {
		t.Errorf("default base, RUB event: %+v %v", got, err)
	}
}

func TestPaidReceivedUsesTheOrdersCurrencyNotTheCurrentBase(t *testing.T) {
	// The invoice was issued while the base was USD; then the panel switches to EUR.
	for cur, want := range map[string]bool{"USD": true, "EUR": false, "RUB": false} {
		s, _, _, c, _ := setupP(t)
		useUSD(s)
		o := providerOrder(t, s, c)
		s.BaseCurrency = func(context.Context) string { return "EUR" }
		ev := paidEvent(o)
		ev.Currency, ev.Amount = cur, 500
		got, err := s.Paid(context.Background(), ev)
		if err != nil || got.Status != StatusPendingReview {
			t.Fatalf("%s: %+v %v", cur, got, err)
		}
		if want != (got.Received != nil) || want && *got.Received != 500 {
			t.Errorf("USD order, base now EUR, %s event: received=%v, recorded want %v", cur, got.Received, want)
		}
	}
}

func TestPaidReceivedNeverForStarsOrders(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	fp.Currency = "XTR"
	o := providerOrder(t, s, c)
	ev := paidEvent(o)
	ev.Currency, ev.Amount = "XTR", 500
	got, err := s.Paid(context.Background(), ev)
	if err != nil || got.Status != StatusPendingReview {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Received != nil {
		t.Errorf("stars are not base-currency money, received=%v", *got.Received)
	}
}

func TestPaidFullInBaseCurrency(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	useUSD(s)
	o := providerOrder(t, s, c)
	ev := paidEvent(o)
	ev.Currency = "USD"
	got, err := s.Paid(context.Background(), ev)
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 {
		t.Fatalf("a full USD payment for a USD invoice: %+v %v payments=%d", got, err, len(f.Payments))
	}
}

func TestProviderRefusedWhileCurrencyUnconfirmed(t *testing.T) {
	ctx := context.Background()
	s, _, _, c, fp := setupP(t)
	known := false
	s.CurrencyKnown = func() bool { return known }

	_, err := s.CreateWith(ctx, c, 77, renew3, "fake")
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("want ErrProvider, got %v", err)
	}
	if len(fp.Invoices) != 0 {
		t.Fatalf("the provider must not be asked for an invoice: %+v", fp.Invoices)
	}
	var st Status
	if err := s.DB.QueryRow(`SELECT status FROM orders`).Scan(&st); err != nil || st != StatusCancelled {
		t.Fatalf("a refused invoice must not leave a live order: %q %v", st, err)
	}

	// Pay on an existing order is refused as well.
	o, err := s.Create(ctx, c, 77, renew3) // manual, awaiting a receipt
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pay(ctx, c.ID, o.ID, "fake"); !errors.Is(err, ErrProvider) {
		t.Fatalf("Pay: want ErrProvider, got %v", err)
	}
	if len(fp.Invoices) != 0 {
		t.Fatalf("invoices %+v", fp.Invoices)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusAwaitingReceipt {
		t.Fatalf("a refused Pay leaves the manual order alone: %s", got.Status)
	}

	// Once the currency is confirmed everything works.
	known = true
	if got, err := s.Pay(ctx, c.ID, o.ID, "fake"); err != nil || got.Status != StatusAwaitingPayment || len(fp.Invoices) != 1 {
		t.Fatalf("known: %+v %v invoices=%d", got, err, len(fp.Invoices))
	}
	if _, err := s.CreateWith(ctx, c, 77, renew3, "fake"); err != nil {
		t.Fatalf("CreateWith when known: %v", err)
	}
}

func TestInvoiceRequestCarriesCustomerID(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	providerOrder(t, s, c)
	if len(fp.Invoices) != 1 || fp.Invoices[0].CustomerID != c.ID || c.ID == 0 {
		t.Fatalf("invoice request %+v, customer %d", fp.Invoices, c.ID)
	}
}

// fixedEvent is a confirmation from a provider that does not report the amount or currency.
func fixedEvent(o *Order) payments.Event {
	return payments.Event{Provider: "fake", OrderID: o.ID, ExternalID: o.ExternalID, Paid: true, Fixed: true}
}

func TestPaidFixedAppliesInFull(t *testing.T) {
	s, f, sp, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	ev := fixedEvent(o)

	got, err := s.Paid(ctx, ev)
	if err != nil || got.Status != StatusApproved || got.Admin != "provider:fake" || got.Decision != DecideFull {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.Payments) != 1 || f.Payments[0].Amount != o.ToPay || f.Payments[0].Amount != 800 {
		t.Fatalf("panel payments %+v", f.Payments)
	}
	if len(sp.sent) != 1 || sp.sent[0].Status != StatusApproved {
		t.Errorf("the customer must be told once: %+v", sp.sent)
	}
	if len(sp.posted) != 0 {
		t.Errorf("a clean provider payment needs no admin card: %d", len(sp.posted))
	}
	for range 3 {
		if _, err := s.Paid(ctx, ev); !errors.Is(err, ErrStateChanged) {
			t.Fatalf("replay: %v", err)
		}
	}
	if len(f.Payments) != 1 {
		t.Fatalf("replays moved money again: %d payments", len(f.Payments))
	}
}

func TestPaidFixedStillChecked(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	for name, mod := range map[string]func(*payments.Event){
		"other provider": func(e *payments.Event) { e.Provider = "yookassa" },
		"other invoice":  func(e *payments.Event) { e.ExternalID = "zzz" },
		"unknown order":  func(e *payments.Event) { e.OrderID = "00000000-0000-4000-8000-000000000000" },
	} {
		ev := fixedEvent(o)
		mod(&ev)
		if _, err := s.Paid(ctx, ev); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ev := fixedEvent(o)
	ev.Paid = false
	if got, err := s.Paid(ctx, ev); err != nil || got != nil {
		t.Errorf("fixed but neither paid nor review: %+v %v", got, err)
	}
	if len(f.Payments) != 0 {
		t.Fatalf("nothing may be applied: %+v", f.Payments)
	}
	if cur, _ := s.Get(ctx, o.ID); cur.Status != StatusAwaitingPayment {
		t.Fatalf("order moved: %s", cur.Status)
	}

	t.Run("fixed together with review is still a review", func(t *testing.T) {
		s, f, sp, c, _ := setupP(t)
		o := providerOrder(t, s, c)
		ev := fixedEvent(o)
		ev.Review = true
		got, err := s.Paid(ctx, ev)
		if err != nil || got.Status != StatusPendingReview {
			t.Fatalf("%+v %v", got, err)
		}
		if len(f.Payments) != 0 || len(sp.posted) != 1 {
			t.Fatalf("payments=%d cards=%d", len(f.Payments), len(sp.posted))
		}
	})
}

func TestPaidZeroWithoutFixedNeedsReview(t *testing.T) {
	// Regression pin: zero is not "unreported". Only Fixed vouches for the invoice.
	s, f, sp, c, _ := setupP(t)
	o := providerOrder(t, s, c)
	ev := paidEvent(o)
	ev.Amount, ev.Currency = 0, ""
	got, err := s.Paid(context.Background(), ev)
	if err != nil || got.Status != StatusPendingReview {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.Payments) != 0 || len(sp.posted) != 1 {
		t.Fatalf("payments=%d cards=%d", len(f.Payments), len(sp.posted))
	}
}

func TestPaidFixedAfterExpiredOrCancelled(t *testing.T) {
	for name, prep := range map[string]func(*Service, *vpnctl.Customer, *Order) error{
		"expired": func(s *Service, _ *vpnctl.Customer, o *Order) error {
			return s.move(context.Background(), o.ID, []Status{StatusAwaitingPayment}, StatusExpired, nil)
		},
		"cancelled": func(s *Service, c *vpnctl.Customer, o *Order) error {
			return s.Cancel(context.Background(), c.ID, o.ID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, f, _, c, _ := setupP(t)
			o := providerOrder(t, s, c)
			if err := prep(s, c, o); err != nil {
				t.Fatal(err)
			}
			got, err := s.Paid(context.Background(), fixedEvent(o))
			if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 {
				t.Fatalf("%+v %v payments=%d", got, err, len(f.Payments))
			}
		})
	}
}
