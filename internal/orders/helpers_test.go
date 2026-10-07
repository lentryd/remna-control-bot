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

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

type spy struct {
	mu                         sync.Mutex
	pay, posted, updated, sent []Order
}

func (s *spy) PayScreen(_ context.Context, o Order, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pay = append(s.pay, o)
	return nil
}
func (s *spy) PostCard(_ context.Context, o Order) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.posted = append(s.posted, o)
	return 500 + len(s.posted), nil
}
func (s *spy) UpdateCard(_ context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, o)
	return nil
}
func (s *spy) Customer(_ context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, o)
	return nil
}

func ptr[T any](v T) *T { return &v }

// setup: customer 5 (tg 77) with subscription 3 (tariff 1, 300/month) and
// add-on 8 on it; balance own 100, referral 0; tariffs: base 1 (300/mo,
// 3 months for 800), base 2 (500/mo), addon 9 (50/mo).
func setup(t *testing.T) (*Service, *vpnctl.Fake, *spy, *vpnctl.Customer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := vpnctl.NewFake()
	exp := now.AddDate(0, 0, 2)
	c := f.Add(vpnctl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Balance: 100, BalanceOwn: 100,
		Subscriptions: []vpnctl.Subscription{{ID: 3, Title: "Дом", TariffID: ptr(1), Price: 300,
			RwUser: &vpnctl.RwUser{ExpireAt: &exp},
			Addons: []vpnctl.Addon{{ID: 8, AddonName: "Доп. устройства", Price: 50, RwUser: &vpnctl.RwUser{ExpireAt: &exp}}}}}})
	f.Tariffs = []vpnctl.Tariff{
		{ID: 1, Kind: "base", Name: "Базовый", MonthlyPrice: 300, Active: true, Periods: []vpnctl.Period{{Months: 3, Price: 800}}},
		{ID: 2, Kind: "base", Name: "Про", MonthlyPrice: 500, Active: true},
		{ID: 9, Kind: "addon", AddonID: ptr(4), AddonName: "Устройства", Name: "+2", MonthlyPrice: 50, Active: true},
	}
	f.Quotes = map[string]vpnctl.ExtendQuote{"subscription:3": {CustomerID: 5, Amount: 300}, "addon:8": {CustomerID: 5, Amount: 50}}
	f.TariffQuotes = map[string]vpnctl.TariffQuote{"subscription:3:2": {Surcharge: 13.33}}
	sp := &spy{}
	s := &Service{DB: st.DB(), Chats: st, API: f, PayMode: config.PayMixed, Manual: true, Details: "Карта 2200",
		TTL: 48 * time.Hour, Notify: sp, Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), NewID: counter()}
	return s, f, sp, c
}

// submitted creates a renew order for 3 months and attaches a photo.
func submitted(t *testing.T, s *Service, c *vpnctl.Customer) *Order {
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
