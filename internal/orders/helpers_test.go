package orders

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/store"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

type spy struct {
	mu               sync.Mutex
	pay, cards, sent []Order
	alerts           []Alert
}

func (s *spy) PayScreen(_ context.Context, o Order, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pay = append(s.pay, o)
	return nil
}
func (s *spy) Card(_ context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards = append(s.cards, o)
	return nil
}

// asked counts the card calls for states that need an admin's decision (a new card in every admin's chat).
func (s *spy) asked() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, o := range s.cards {
		if o.Status == StatusPendingReview || o.Status == StatusFailed {
			n++
		}
	}
	return n
}

func (s *spy) Customer(_ context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, o)
	return nil
}

func (s *spy) Alert(_ context.Context, a Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = append(s.alerts, a)
	return nil
}

func ptr[T any](v T) *T { return &v }

// setup: customer 5 (tg 77) with subscription 3 (tariff 1, 300/month) and
// add-on 8 on it; balance own 100, referral 0; tariffs: base 1 (300/mo,
// 3 months for 800), base 2 (500/mo), addon 9 (50/mo).
func setup(t *testing.T) (*Service, *remnactl.Fake, *spy, *remnactl.Customer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := remnactl.NewFake()
	exp := now.AddDate(0, 0, 2)
	c := f.Add(remnactl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Balance: 100, BalanceOwn: 100,
		Subscriptions: []remnactl.Subscription{{ID: 3, Title: "Дом", TariffID: ptr(1), Price: 300,
			RwUser: &remnactl.RwUser{ExpireAt: &exp},
			Addons: []remnactl.Addon{{ID: 8, AddonName: "Доп. устройства", Price: 50, RwUser: &remnactl.RwUser{ExpireAt: &exp}}}}}})
	f.Tariffs = []remnactl.Tariff{
		{ID: 1, Kind: "base", Name: "Базовый", MonthlyPrice: 300, Active: true, Periods: []remnactl.Period{{Months: 3, Price: 800}}},
		{ID: 2, Kind: "base", Name: "Про", MonthlyPrice: 500, Active: true},
		{ID: 9, Kind: "addon", AddonID: ptr(4), AddonName: "Устройства", Name: "+2", MonthlyPrice: 50, Active: true},
	}
	f.Quotes = map[string]remnactl.ExtendQuote{"subscription:3": {CustomerID: 5, Amount: 300}, "addon:8": {CustomerID: 5, Amount: 50}}
	f.TariffQuotes = map[string]remnactl.TariffQuote{"subscription:3:2": {Surcharge: 13.33}}
	sp := &spy{}
	s := &Service{DB: st.DB(), Chats: st, API: f, Notify: sp, Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), NewID: counter()}
	s.SetLive(Live{PayMode: config.PayMixed, Manual: true, Details: "Карта 2200", TTL: 48 * time.Hour})
	return s, f, sp, c
}

// submitted creates a renew order for 3 months and attaches a photo.
func submitted(t *testing.T, s *Service, c *remnactl.Customer) *Order {
	t.Helper()
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if o, err = s.AttachReceipt(ctx, 77, ReceiptPhoto, "file1", "перевёл"); err != nil {
		t.Fatal(err)
	}
	return o
}

// counter numbers orders 1, 2, 3… so tests can name them.
func counter() func() string {
	n := 0
	return func() string { n++; return strconv.Itoa(n) }
}
