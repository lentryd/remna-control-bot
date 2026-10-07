package profile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/vpnctl"
)

func ptr[T any](v T) *T { return &v }

func TestBuild(t *testing.T) {
	d1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &vpnctl.Customer{
		ID: 1, Name: "Ann", Notes: "SECRET-NOTE", Contact: "SECRET-CONTACT", Balance: 10,
		Subscriptions: []vpnctl.Subscription{
			{ID: 1, Title: "Main", TariffID: ptr(4), TariffName: "Std", RwUser: &vpnctl.RwUser{SubscriptionURL: "https://s/a", Status: "ACTIVE"},
				Addons: []vpnctl.Addon{{ID: 3, AddonName: "YouTube", RwUser: &vpnctl.RwUser{Status: "ACTIVE"}}}},
			{ID: 2, Title: "Gone", RwUser: &vpnctl.RwUser{Deleted: true}},
		},
		Ledger:    []vpnctl.LedgerEntry{{Type: "payment", Amount: 100, Date: d1, Note: "SECRET-LEDGER"}, {Type: "charge", Amount: -50, Date: d1.AddDate(0, 1, 0)}},
		Referrals: []vpnctl.Referee{{ID: 7, Name: "Bob", TotalPaid: 300}, {ID: 8, Name: "Eve", Archived: true}},
		Accruals: []vpnctl.Accrual{
			{ReferrerID: 1, RefereeID: 7, Amount: 30}, {ReferrerID: 1, RefereeID: 7, Amount: 15},
			{ReferrerID: 1, RefereeID: 8, Amount: 50}, {ReferrerID: 99, RefereeID: 1, Amount: 1000},
		},
	}
	p := Build(c, "https://t.me/bot?start=ref_x", config.PayMixed)

	if len(p.Subscriptions) != 1 || p.Subscriptions[0].URL != "https://s/a" || len(p.Subscriptions[0].Addons) != 1 {
		t.Fatalf("subscriptions: %+v", p.Subscriptions)
	}
	if got := p.Subscriptions[0].TariffID; got == nil || *got != 4 {
		t.Fatalf("tariff id: %v", got)
	}
	if !strings.Contains(string(mustJSON(t, p.Subscriptions[0])), `"tariff_id":4`) {
		t.Fatal("tariff_id not in JSON")
	}
	if p.Ledger[0].Amount != -50 { // newest first
		t.Fatalf("ledger order: %+v", p.Ledger)
	}
	if p.ReferralsEarned != 95 || p.Referrals[0].Name != "Eve" || p.Referrals[0].Earned != 50 || p.Referrals[1].Earned != 45 || p.Referrals[0].Active {
		t.Fatalf("referrals: %+v earned=%v", p.Referrals, p.ReferralsEarned)
	}
	raw, _ := json.Marshal(p)
	// invitees: only what they brought, not how much they pay or have paid
	for _, secret := range []string{"SECRET-NOTE", "SECRET-CONTACT", "SECRET-LEDGER", "referral_percent", "total_paid", "\"monthly\":0,\"total"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("profile JSON leaks %q", secret)
		}
	}
}

func TestTariffs(t *testing.T) {
	got := Tariffs([]vpnctl.Tariff{
		{ID: 1, Kind: "addon", Name: "A", Active: true, MonthlyPrice: 1},
		{ID: 2, Kind: "base", Name: "B", Active: true, MonthlyPrice: 1, SortOrder: 2},
		{ID: 3, Kind: "base", Name: "C", Active: false, MonthlyPrice: 1},
		{ID: 4, Kind: "base", Name: "D", Active: true, MonthlyPrice: 1, SortOrder: 1},
	})
	ids := []int{}
	for _, t := range got {
		ids = append(ids, t.ID)
	}
	if len(ids) != 3 || ids[0] != 4 || ids[1] != 2 || ids[2] != 1 {
		t.Fatalf("order = %v", ids)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		exp       *time.Time
		unlimited bool
		want      State
	}{
		{nil, false, StateNone},
		{ptr(now.Add(-time.Hour)), false, StateExpired},
		{ptr(now.Add(48 * time.Hour)), false, StateSoon},
		{ptr(now.Add(100 * time.Hour)), false, StateActive},
		{ptr(now.Add(-time.Hour)), true, StateActive},
	}
	for i, c := range cases {
		if got := Expiry(c.exp, c.unlimited, now); got != c.want {
			t.Errorf("case %d: %v want %v", i, got, c.want)
		}
	}
}

func TestSubscriptionsByUrgency(t *testing.T) {
	now := time.Now()
	sub := func(id int, exp *time.Time, unlimited bool) vpnctl.Subscription {
		return vpnctl.Subscription{ID: id, RwUser: &vpnctl.RwUser{ExpireAt: exp, Unlimited: unlimited}}
	}
	c := &vpnctl.Customer{Subscriptions: []vpnctl.Subscription{
		sub(1, nil, true),                         // unlimited — last
		sub(2, ptr(now.AddDate(0, 1, 0)), false),  // a month
		sub(3, ptr(now.Add(48*time.Hour)), false), // 2 days
		sub(4, ptr(now.AddDate(0, 0, -5)), false), // expired 5 days ago — first
		{ID: 5}, // not provisioned yet — before unlimited
		sub(6, ptr(now.AddDate(0, 0, -1)), false), // expired yesterday
	}}
	var got []int
	for _, s := range Build(c, "", config.PayMixed).Subscriptions {
		got = append(got, s.ID)
	}
	want := []int{4, 6, 3, 2, 5, 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestTariffsHideFree(t *testing.T) {
	got := Tariffs([]vpnctl.Tariff{
		{ID: 1, Kind: "base", Name: "Base", Active: true, MonthlyPrice: 100},
		{ID: 2, Kind: "base", Name: "VIP", Active: true, MonthlyPrice: 0},
		{ID: 3, Kind: "addon", Name: "Included", Active: true, MonthlyPrice: 0},
	})
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestRenewalUsesPayMode(t *testing.T) {
	c := &vpnctl.Customer{BalanceOwn: 100, BalanceReferral: 250, Subscriptions: []vpnctl.Subscription{{
		ID: 1, Price: 200, RwUser: &vpnctl.RwUser{},
		Addons: []vpnctl.Addon{{Price: 100}, {Price: 0, Included: true}},
	}}}
	mixed := Build(c, "", config.PayMixed).Subscriptions[0].Renewal
	if mixed != (billing.Payment{Price: 300, FromReferral: 250, FromOwn: 50}) {
		t.Fatalf("mixed: %+v", mixed)
	}
	full := Build(c, "", config.PayReferralFull).Subscriptions[0].Renewal
	if full != (billing.Payment{Price: 300, FromOwn: 100, ToPay: 200}) {
		t.Fatalf("referral_full: %+v", full)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
