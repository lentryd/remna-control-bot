package profile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

func ptr[T any](v T) *T { return &v }

func TestBuild(t *testing.T) {
	d1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &vpnctl.Customer{
		ID: 1, Name: "Ann", Notes: "SECRET-NOTE", Contact: "SECRET-CONTACT", Balance: 10,
		Subscriptions: []vpnctl.Subscription{
			{ID: 1, Title: "Main", TariffName: "Std", RwUser: &vpnctl.RwUser{SubscriptionURL: "https://s/a", Status: "ACTIVE"},
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
	p := Build(c, "https://t.me/bot?start=ref_x")

	if len(p.Subscriptions) != 1 || p.Subscriptions[0].URL != "https://s/a" || len(p.Subscriptions[0].Addons) != 1 {
		t.Fatalf("subscriptions: %+v", p.Subscriptions)
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
		{ID: 1, Kind: "addon", Name: "A", Active: true},
		{ID: 2, Kind: "base", Name: "B", Active: true, SortOrder: 2},
		{ID: 3, Kind: "base", Name: "C", Active: false},
		{ID: 4, Kind: "base", Name: "D", Active: true, SortOrder: 1},
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
