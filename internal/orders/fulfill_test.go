package orders

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/vpnctl"
)

func TestApproveRenew(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	got, err := s.Approve(context.Background(), o.ID, "@boss", DecideFull)
	if err != nil || got.Status != StatusApproved || got.Admin != "@boss" || got.ResultUntil == nil || got.PanelPaymentID != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	p := f.Payments[0]
	if p.Amount != 800 || p.Method != "manual" || p.Note != "bot order #1" || len(p.Allocations) != 1 ||
		p.Allocations[0] != (vpnctl.Allocation{Kind: "subscription", ID: 3, Months: 3, Amount: 900}) {
		t.Fatalf("payment %+v", p)
	}
	if len(sp.cards) == 0 || sp.cards[len(sp.cards)-1].Status != StatusApproved || len(sp.sent) != 1 || sp.sent[0].Status != StatusApproved {
		t.Fatalf("notifications: cards %d, customer %d", len(sp.cards), len(sp.sent))
	}
}

func TestApproveTwiceRecordsOnePayment(t *testing.T) {
	s, f, _, c := setup(t)
	o := submitted(t, s, c)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = s.Approve(context.Background(), o.ID, "@a", DecideFull) }()
	}
	wg.Wait()
	if len(f.Payments) != 1 || (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("payments %d, errs %v", len(f.Payments), errs)
	}
	if !errors.Is(errs[0], ErrStateChanged) && !errors.Is(errs[1], ErrStateChanged) {
		t.Fatalf("loser should get ErrStateChanged: %v", errs)
	}
}

func TestRenewExtensionFailsThenRetry(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	f.FailExtend = "remnawave down"
	got, err := s.Approve(context.Background(), o.ID, "@a", DecideFull)
	if err != nil || got.Status != StatusFailed || got.Error != "remnawave down" || got.PanelPaymentID == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if sp.sent[len(sp.sent)-1].Status != StatusFailed {
		t.Fatal("customer not told about the failure")
	}
	f.FailExtend = ""
	got, err = s.Retry(context.Background(), o.ID, "@a")
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || len(f.Extends) != 1 ||
		f.Extends[0].In != (vpnctl.ExtendInput{Months: 3, Amount: 900, AllowDebt: true}) {
		t.Fatalf("retry %+v %v; payments %d extends %+v", got, err, len(f.Payments), f.Extends)
	}
}

func TestPanelDownFailsWithUnknownOutcome(t *testing.T) {
	s, f, _, c := setup(t)
	o := submitted(t, s, c)
	f.Err = vpnctl.ErrUnavailable
	got, err := s.Approve(context.Background(), o.ID, "@a", DecideFull)
	if err != nil || got.Status != StatusFailed || !strings.Contains(got.Error, "bot order #1") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRetryKeepsCreditDecision(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o := submitted(t, s, c)
	s.SetReceived(ctx, o.ID, 500, "@a")
	f.Err = &vpnctl.APIError{Status: 400, Message: "x"}
	got, err := s.Approve(ctx, o.ID, "@a", DecideCredit)
	if err != nil || got.Status != StatusFailed || got.Error != "x" {
		t.Fatalf("%+v %v", got, err)
	}
	f.Err = nil
	got, err = s.Retry(ctx, o.ID, "@a")
	if err != nil || got.Status != StatusCredited || len(f.Payments) != 1 || len(f.Payments[0].Allocations) != 0 || len(f.Extends) != 0 {
		t.Fatalf("%+v %v payments %+v extends %+v", got, err, f.Payments, f.Extends)
	}
}

func TestUnknownPayOutcomeKeepsDecision(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o := submitted(t, s, c)
	f.Err = vpnctl.ErrUnavailable
	got, _ := s.Approve(ctx, o.ID, "@a", DecideFull)
	if got.PanelPaymentID != 0 || got.Decision != DecideFull {
		t.Fatalf("%+v", got)
	}
}

func TestPaymentIDSurvivesFailedFinalMove(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o := submitted(t, s, c)
	f.FailExtend = "down"
	if _, err := s.Approve(ctx, o.ID, "@a", DecideFull); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, o.ID)
	if got.PanelPaymentID != 1 {
		t.Fatalf("payment id %d", got.PanelPaymentID)
	}
}

func TestTopupApproval(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 250})
	if err != nil {
		t.Fatal(err)
	}
	s.RequestReceipt(ctx, c.ID, o.ID)
	s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
	got, err := s.Approve(ctx, o.ID, "@a", DecideFull)
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || f.Payments[0].Amount != 250 || len(f.Payments[0].Allocations) != 0 {
		t.Fatalf("%+v %v %+v", got, err, f.Payments)
	}
}

func TestCheckDecision(t *testing.T) {
	two := 200.0
	renew := &Order{Request: Request{Kind: KindRenew}, ToPay: 800, Received: &two}
	if err := checkDecision(renew, DecideFull); !errors.Is(err, ErrUnderpaid) {
		t.Fatalf("full on underpaid: %v", err)
	}
	if err := checkDecision(renew, DecideDebt); err != nil {
		t.Fatalf("debt on renew: %v", err)
	}
	sub := &Order{Request: Request{Kind: KindSubscribe}, ToPay: 800}
	if err := checkDecision(sub, DecideDebt); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("debt on subscribe: %v", err)
	}
	if err := checkDecision(sub, "bogus"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bogus: %v", err)
	}
}

func TestSubscribeAddonChangeTariff(t *testing.T) {
	ctx := context.Background()
	for _, r := range []Request{
		{Kind: KindSubscribe, TariffID: 1, Months: 3},
		{Kind: KindAddon, ItemID: 3, TariffID: 9, Months: 2},
		{Kind: KindChangeTariff, ItemKind: "subscription", ItemID: 3, TariffID: 2},
	} {
		s, f, _, c := setup(t)
		c.BalanceOwn = 0
		o, err := s.Create(ctx, c, 77, r)
		if err != nil {
			t.Fatal(err)
		}
		s.RequestReceipt(ctx, c.ID, o.ID)
		s.AttachReceipt(ctx, 77, ReceiptText, "", "оплатил")
		got, err := s.Approve(ctx, o.ID, "@a", DecideFull)
		if err != nil || got.Status != StatusApproved {
			t.Fatalf("%s: %+v %v", r.Kind, got, err)
		}
		if len(f.Payments) != 1 || f.Payments[0].Amount != o.ToPay || len(f.Payments[0].Allocations) != 0 {
			t.Fatalf("%s: payment %+v", r.Kind, f.Payments)
		}
		switch r.Kind {
		case KindSubscribe:
			p := f.Provisions[0]
			if p.CustomerID != 5 || p.TariffID != 1 || p.Months != 3 || p.Amount != 800 || p.AllowDebt || p.Username != "bot_c5_o1" {
				t.Fatalf("provision %+v", p)
			}
		case KindAddon:
			a := f.AddonCalls[0]
			if a.SubscriptionID != 3 || a.In.TariffID != 9 || a.In.Amount != 100 || a.In.AllowDebt {
				t.Fatalf("addon %+v", a)
			}
		case KindChangeTariff:
			if tc := f.TariffChanges[0]; tc.ID != 3 || tc.In.TariffID != 2 || tc.In.Surcharge != 13.33 {
				t.Fatalf("tariff %+v", tc)
			}
		}
	}
}

func TestSecondStepFailsRetryDoesNotPayAgain(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
	s.RequestReceipt(ctx, c.ID, o.ID)
	s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
	f.FailStep = &vpnctl.APIError{Status: 400, Message: "no free squad"}
	got, _ := s.Approve(ctx, o.ID, "@a", DecideFull)
	if got.Status != StatusFailed || got.Error == "" {
		t.Fatalf("%+v", got)
	}
	f.FailStep = nil
	got, err := s.Retry(ctx, o.ID, "@a")
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || len(f.Provisions) != 1 {
		t.Fatalf("%+v %v payments %d", got, err, len(f.Payments))
	}
}

func TestCorrectedAmount(t *testing.T) {
	ctx := context.Background()
	t.Run("underpaid full is refused", func(t *testing.T) {
		s, _, _, c := setup(t)
		o := submitted(t, s, c)
		if _, err := s.SetReceived(ctx, o.ID, 500, "@a"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Approve(ctx, o.ID, "@a", DecideFull); !errors.Is(err, ErrUnderpaid) {
			t.Fatalf("want ErrUnderpaid, got %v", err)
		}
		if got, _ := s.Get(ctx, o.ID); got.Status != StatusPendingReview {
			t.Fatalf("status %s", got.Status)
		}
	})
	t.Run("debt", func(t *testing.T) {
		s, f, _, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 500, "@a")
		got, err := s.Approve(ctx, o.ID, "@a", DecideDebt)
		if err != nil || got.Status != StatusApproved || f.Payments[0].Amount != 500 || f.Payments[0].Allocations[0].Amount != 900 {
			t.Fatalf("%+v %v %+v", got, err, f.Payments)
		}
	})
	t.Run("credit", func(t *testing.T) {
		s, f, sp, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 500, "@a")
		got, err := s.Approve(ctx, o.ID, "@a", DecideCredit)
		if err != nil || got.Status != StatusCredited || f.Payments[0].Amount != 500 || len(f.Payments[0].Allocations) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
		if sp.sent[0].Status != StatusCredited || *sp.sent[0].Received != 500 {
			t.Fatalf("customer notice %+v", sp.sent[0])
		}
	})
	t.Run("overpaid stays on balance", func(t *testing.T) {
		s, f, _, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 1000, "@a")
		if got, err := s.Approve(ctx, o.ID, "@a", DecideFull); err != nil || got.Status != StatusApproved || f.Payments[0].Amount != 1000 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("debt only for renewals", func(t *testing.T) {
		s, _, _, c := setup(t)
		o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
		s.RequestReceipt(ctx, c.ID, o.ID)
		s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
		if _, err := s.Approve(ctx, o.ID, "@a", DecideDebt); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("want ErrBadRequest, got %v", err)
		}
	})
	t.Run("bad amounts", func(t *testing.T) {
		s, _, _, c := setup(t)
		o := submitted(t, s, c)
		if _, err := s.SetReceived(ctx, o.ID, 0, "@a"); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("zero: %v", err)
		}
	})
}

func TestReject(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	got, err := s.Reject(context.Background(), o.ID, "нет перевода", "@a")
	if err != nil || got.Status != StatusRejected || got.RejectReason != "нет перевода" || len(f.Payments) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if sp.sent[0].RejectReason != "нет перевода" {
		t.Fatal("customer not told the reason")
	}
	if _, err := s.Approve(context.Background(), o.ID, "@a", DecideFull); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("approve after reject: %v", err)
	}
}

func TestPayFromBalance(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1})
	if o.Method != MethodBalance || o.Status != StatusNew {
		t.Fatalf("%+v", o)
	}
	got, err := s.PayFromBalance(ctx, c.ID, o.ID)
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 0 ||
		f.Extends[0] != (vpnctl.ExtendCall{Kind: "addon", ID: 8, In: vpnctl.ExtendInput{Months: 1, Amount: 50}}) {
		t.Fatalf("%+v %v %+v", got, err, f.Extends)
	}
	if _, err := s.PayFromBalance(ctx, 6, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign: %v", err)
	}
}

func TestApplyStepServerErrorAfterPaymentIsUnknown(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
	s.RequestReceipt(ctx, c.ID, o.ID)
	s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
	f.FailStep = &vpnctl.APIError{Status: 502, Message: "bad gateway"}
	got, _ := s.Approve(ctx, o.ID, "@a", DecideFull)
	en, ru := i18n.Default.For("en").Text(got.Error), i18n.Default.For("ru").Text(got.Error)
	if got.Status != StatusFailed || got.PanelPaymentID == 0 ||
		!strings.Contains(en, "result is unknown") || !strings.Contains(en, "bot order #1") || !strings.Contains(en, "customer #5") ||
		!strings.Contains(en, "bad gateway") {
		t.Fatalf("%+v en=%q", got, en)
	}
	if !strings.Contains(ru, "Результат неизвестен") || !strings.Contains(ru, "VPN-панели") || !strings.Contains(ru, "клиента #5") {
		t.Fatalf("ru=%q", ru)
	}
}

func TestApplyStepClientErrorKeepsPlainMessage(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
	s.RequestReceipt(ctx, c.ID, o.ID)
	s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
	f.FailStep = &vpnctl.APIError{Status: 400, Message: "no free squad"}
	if got, _ := s.Approve(ctx, o.ID, "@a", DecideFull); got.Error != "no free squad" {
		t.Fatalf("%q", got.Error)
	}
}

func TestFailedBalanceOrderPostsAdminCard(t *testing.T) {
	s, f, sp, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1})
	f.FailExtend = "remnawave down"
	got, err := s.PayFromBalance(ctx, c.ID, o.ID)
	if err != nil || got.Status != StatusFailed || sp.asked() != 1 || sp.cards[len(sp.cards)-1].Status != StatusFailed {
		t.Fatalf("%+v %v asked %d", got, err, sp.asked())
	}
	f.FailExtend = ""
	got, err = s.Retry(ctx, o.ID, "@a")
	if err != nil || got.Status != StatusApproved || sp.asked() != 1 || sp.cards[len(sp.cards)-1].Status != StatusApproved {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStaleApprovingRecovers(t *testing.T) {
	s, _, sp, c := setup(t)
	ctx := context.Background()
	stale := submitted(t, s, c)
	if err := s.move(ctx, stale.ID, []Status{StatusPendingReview}, StatusApproving, nil); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(5 * time.Minute) }
	if err := s.ExpireStale(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, stale.ID); got.Status != StatusApproving {
		t.Fatalf("fresh approving touched: %s", got.Status)
	}
	s.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if err := s.ExpireStale(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, stale.ID)
	en, ru := i18n.Default.For("en").Text(got.Error), i18n.Default.For("ru").Text(got.Error)
	if got.Status != StatusFailed || !strings.Contains(en, "check the VPN panel") || !strings.Contains(en, "bot order #1") ||
		!strings.Contains(en, "subscription #3") {
		t.Fatalf("%+v en=%q", got, en)
	}
	if !strings.Contains(ru, "проверьте в VPN-панели") || !strings.Contains(ru, "bot order #1") {
		t.Fatalf("ru=%q", ru)
	}
	if len(sp.cards) == 0 || sp.cards[len(sp.cards)-1].Status != StatusFailed {
		t.Fatal("admin card not updated")
	}
	if len(sp.sent) != 1 || sp.sent[0].ID != stale.ID || sp.sent[0].Status != StatusFailed {
		t.Fatalf("customer notice: %+v", sp.sent)
	}
}
