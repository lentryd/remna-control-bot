package orders

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/vpnctl"
)

func TestQuote(t *testing.T) {
	s, f, _, c := setup(t)
	f.Tariffs[0].Periods = append(f.Tariffs[0].Periods, vpnctl.Period{Days: 7, Price: 0}) // a free trial is not for sale
	ctx := context.Background()
	cases := []struct {
		name  string
		r     Request
		price float64
		title string
	}{
		{"renew", Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3}, 900, "Продление «Дом» · 3 мес."},
		{"renew addon", Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1}, 50, "Продление «Доп. устройства» · 1 мес."},
		{"subscribe period", Request{Kind: KindSubscribe, TariffID: 1, Months: 3}, 800, "Новая подписка «Базовый» · 3 мес."},
		{"addon", Request{Kind: KindAddon, ItemID: 3, TariffID: 9, Months: 2}, 100, "Устройства «+2» к «Дом» · 2 мес."},
		{"change tariff", Request{Kind: KindChangeTariff, ItemKind: "subscription", ItemID: 3, TariffID: 2}, 13.33, "Смена тарифа «Дом» → «Про»"},
		{"topup", Request{Kind: KindTopup, Amount: 250}, 250, "Пополнение баланса"},
	}
	for _, tc := range cases {
		q, err := s.Quote(ctx, c, tc.r)
		if err != nil || q.Price != tc.price || q.Title != tc.title {
			t.Errorf("%s: %+v %v", tc.name, q, err)
		}
	}
	q, _ := s.Quote(ctx, c, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3})
	if q.FromBalance != 100 || q.ToPay != 800 || len(q.Methods) != 1 || q.Methods[0] != MethodManual {
		t.Errorf("split: %+v", q)
	}
	q, _ = s.Quote(ctx, c, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1})
	if q.ToPay != 0 || q.Methods[0] != MethodBalance {
		t.Errorf("covered by balance: %+v", q)
	}
	if q, _ := s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 250}); q.FromBalance != 0 || q.ToPay != 250 {
		t.Errorf("topup never uses the balance: %+v", q)
	}
	for _, bad := range []Request{
		{Kind: KindRenew, ItemKind: "subscription", ItemID: 3},             // no term
		{Kind: KindRenew, ItemKind: "subscription", ItemID: 99, Months: 1}, // not theirs
		{Kind: KindAddon, ItemID: 99, TariffID: 9, Months: 1},              // not theirs
		{Kind: KindSubscribe, TariffID: 9, Months: 1},                      // addon tariff as a subscription
		{Kind: KindTopup, Amount: -5},
		{Kind: KindTopup, Amount: 0.004},                              // rounds to zero
		{Kind: KindSubscribe, TariffID: 1, Days: 7},                   // free trial period
		{Kind: KindAddon, ItemID: 3, TariffID: 9, Days: 0, Months: 0}, // no term
		{Kind: "nope"},
	} {
		if _, err := s.Quote(ctx, c, bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: want ErrBadRequest, got %v", bad, err)
		}
	}
}

func TestManualOffOnlyBalance(t *testing.T) {
	s, _, _, c := setup(t)
	s.UpdateLive(func(l *Live) { l.Manual = false })
	ctx := context.Background()
	if _, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 1}); !errors.Is(err, ErrManualOff) {
		t.Fatalf("want ErrManualOff, got %v", err)
	}
	if _, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1}); err != nil {
		t.Fatalf("balance order: %v", err)
	}
}

func TestReceiptFlow(t *testing.T) {
	s, _, sp, c := setup(t)
	o := submitted(t, s, c)
	if o.Status != StatusPendingReview || o.ReceiptFileID != "file1" || o.Comment != "перевёл" || o.AdminMsgID != 501 {
		t.Fatalf("%+v", o)
	}
	if len(sp.pay) != 1 || len(sp.posted) != 1 {
		t.Fatalf("pay screens %d, cards %d", len(sp.pay), len(sp.posted))
	}
	if _, ok, _ := s.Chats.GetChatState(context.Background(), 77); ok {
		t.Fatal("chat still waits for a receipt")
	}
	if _, err := s.AttachReceipt(context.Background(), 77, ReceiptPhoto, "f2", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second receipt without a waiting order: %v", err)
	}
}

func TestAttachReceiptRejectsUnknownKind(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	s.RequestReceipt(ctx, c.ID, o.ID)
	if _, err := s.AttachReceipt(ctx, 77, "sticker", "x", ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusAwaitingReceipt {
		t.Fatalf("status %s", got.Status)
	}
}

func TestCancelOtherCustomer(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	if err := s.Cancel(ctx, 6, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign cancel: %v", err)
	}
	if err := s.Cancel(ctx, 5, o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(ctx, 5, o.ID); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("cancel twice: %v", err)
	}
}

func TestExpireStale(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	s.RequestReceipt(ctx, c.ID, o.ID)
	fresh := submitted(t, s, c) // pending_review never expires
	s.Now = func() time.Time { return now.Add(49 * time.Hour) }
	if err := s.ExpireStale(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusExpired {
		t.Fatalf("status %s", got.Status)
	}
	if got, _ := s.Get(ctx, fresh.ID); got.Status != StatusPendingReview {
		t.Fatalf("reviewed order expired: %s", got.Status)
	}
}

func TestOrderIDsAreUUIDs(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	s.NewID = nil // the real ids
	at := now
	s.Now = func() time.Time { at = at.Add(time.Minute); return at }
	seen := map[string]bool{}
	var last *Order
	for range 3 {
		o, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uuid.Parse(o.ID); err != nil || seen[o.ID] {
			t.Fatalf("order id %q: %v (seen %v)", o.ID, err, seen[o.ID])
		}
		seen[o.ID] = true
		if o.Short() != o.ID[:8] {
			t.Errorf("short %q of %q", o.Short(), o.ID)
		}
		if got, err := s.Get(ctx, o.ID); err != nil || got.ID != o.ID {
			t.Errorf("get %q: %v", o.ID, err)
		}
		if _, err := s.RequestReceipt(ctx, c.ID, o.ID); err != nil {
			t.Fatal(err)
		}
		last = o
	}
	if o, err := s.ForCustomer(ctx, c.ID, last.ID); err != nil || o.Note() != "bot order #"+last.ID {
		t.Errorf("note %v %v", o, err)
	}
	list, total, err := s.List(ctx, c.ID, 10, 0)
	if err != nil || total != 3 || len(list) != 3 || list[0].ID != last.ID {
		t.Fatalf("list %v %d %v: newest first", list, total, err)
	}
}

func TestQuoteListsProviders(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	r := Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3}

	q, err := s.Quote(ctx, c, r)
	if err != nil || !slices.Equal(q.Methods, []Method{MethodManual}) || len(q.Titles) != 0 {
		t.Fatalf("no providers: %+v %v", q, err)
	}
	s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry(&payments.Fake{Name: "fake"}) })
	q, _ = s.Quote(ctx, c, r)
	if !slices.Equal(q.Methods, []Method{MethodManual, "fake"}) || q.Titles["fake"] != "Fake pay" {
		t.Fatalf("with a provider: %+v", q)
	}
	s.UpdateLive(func(l *Live) { l.Manual = false })
	if q, _ = s.Quote(ctx, c, r); !slices.Equal(q.Methods, []Method{"fake"}) {
		t.Fatalf("providers only: %+v", q)
	}
	// Fully covered by the balance: only the balance, whatever is configured.
	c.Balance, c.BalanceOwn = 5000, 5000
	if q, _ = s.Quote(ctx, c, r); !slices.Equal(q.Methods, []Method{MethodBalance}) {
		t.Fatalf("covered: %+v", q)
	}
}

func TestProviderColumnsRoundTrip(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.move(ctx, o.ID, []Status{StatusAwaitingReceipt}, StatusAwaitingPayment, map[string]any{
		"method": "fake", "external_id": "ext-1", "pay_url": "https://pay.test/x", "pay_amount": 66.67, "pay_currency": "XTR"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, o.ID)
	if err != nil || got.Status != StatusAwaitingPayment || got.Method != "fake" || !got.Method.IsProvider() || got.ExternalID != "ext-1" ||
		got.PayURL != "https://pay.test/x" || got.PayAmount != 66.67 || got.PayCurrency != "XTR" {
		t.Fatalf("%+v %v", got, err)
	}
	if MethodManual.IsProvider() || MethodBalance.IsProvider() || Method("").IsProvider() {
		t.Error("manual, balance and empty are not providers")
	}
}
