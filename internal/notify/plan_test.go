package notify

import (
	"testing"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

func ptr[T any](v T) *T { return &v }

var (
	now   = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rules = Rules{TrafficPct: 80, RemindDays: []int{3, 1}}
	GB    = int64(1 << 30)
)

func customers() []vpnctl.Customer {
	return []vpnctl.Customer{
		{ID: 1, TelegramID: ptr(int64(100))},
		{ID: 2}, // no Telegram — never notified
		{ID: 3, TelegramID: ptr(int64(300)), Archived: true}, // archived — never notified
	}
}

func sub(id, customer int, u *vpnctl.RwUser) vpnctl.Subscription {
	return vpnctl.Subscription{ID: id, CustomerID: customer, Title: "Sub" + string(rune('0'+id)), TariffID: ptr(10), RwUser: u}
}

func until(d time.Duration) *time.Time { return ptr(now.Add(d)) }

var tariffs = []vpnctl.Tariff{
	{ID: 10, Kind: "base", Name: "Base", Active: true, MonthlyPrice: 100, TrafficLimitBytes: 50 * GB},
	{ID: 11, Kind: "base", Name: "Plus", Active: true, MonthlyPrice: 200, TrafficLimitBytes: 200 * GB},
	{ID: 12, Kind: "base", Name: "Max", Active: true, MonthlyPrice: 300}, // unlimited, pricier
	{ID: 13, Kind: "base", Name: "Hidden", Active: false, MonthlyPrice: 150, TrafficLimitBytes: 500 * GB},
	{ID: 14, Kind: "base", Name: "VIP", Active: true, MonthlyPrice: 0}, // free — not offered
	{ID: 20, Kind: "addon", AddonID: ptr(4), Name: "LTE S", Active: true, MonthlyPrice: 50, TrafficLimitBytes: 10 * GB},
	{ID: 21, Kind: "addon", AddonID: ptr(4), Name: "LTE L", Active: true, MonthlyPrice: 90, TrafficLimitBytes: 30 * GB},
	{ID: 22, Kind: "addon", AddonID: ptr(5), Name: "Other", Active: true, MonthlyPrice: 10, TrafficLimitBytes: 99 * GB},
}

func byKey(ns []Notice) map[string]Notice {
	m := map[string]Notice{}
	for _, n := range ns {
		m[n.Key] = n
	}
	return m
}

func TestTrafficAlert(t *testing.T) {
	reset := until(5 * 24 * time.Hour)
	subs := []vpnctl.Subscription{
		sub(1, 1, &vpnctl.RwUser{UsedTrafficBytes: 41 * GB, TrafficLimitBytes: 50 * GB, NextTrafficResetAt: reset, ExpireAt: until(30 * 24 * time.Hour)}),
		sub(2, 1, &vpnctl.RwUser{UsedTrafficBytes: 39 * GB, TrafficLimitBytes: 50 * GB, ExpireAt: until(30 * 24 * time.Hour)}), // 78% — quiet
		sub(3, 1, &vpnctl.RwUser{UsedTrafficBytes: 99 * GB, TrafficLimitBytes: 0, ExpireAt: until(30 * 24 * time.Hour)}),       // no limit
		sub(4, 2, &vpnctl.RwUser{UsedTrafficBytes: 49 * GB, TrafficLimitBytes: 50 * GB}),                                       // no Telegram
		sub(5, 1, &vpnctl.RwUser{UsedTrafficBytes: 49 * GB, TrafficLimitBytes: 50 * GB, Deleted: true}),
	}
	ns := Plan(customers(), subs, tariffs, rules, now)
	if len(ns) != 1 {
		t.Fatalf("want 1 notice, got %+v", ns)
	}
	n := ns[0]
	if n.Kind != Traffic || n.ChatID != 100 || n.SubID != 1 || n.Used != 41*GB || n.Limit != 50*GB || n.ResetAt != reset {
		t.Fatalf("notice: %+v", n)
	}
	if n.Upgrade == nil || n.Upgrade.ID != 11 {
		t.Fatalf("upgrade should be the cheapest active paid tariff with more traffic (Plus), got %+v", n.Upgrade)
	}
	if n.Key != "traffic:sub:1:2026-10-15" {
		t.Fatalf("key = %q", n.Key)
	}
}

func TestTrafficAlertNoResetUsesExpiryInKey(t *testing.T) {
	subs := []vpnctl.Subscription{sub(1, 1, &vpnctl.RwUser{UsedTrafficBytes: 45 * GB, TrafficLimitBytes: 50 * GB, TrafficLimitStrategy: "NO_RESET", ExpireAt: until(20 * 24 * time.Hour)})}
	n := Plan(customers(), subs, tariffs, rules, now)[0]
	if n.ResetAt != nil || n.Key != "traffic:sub:1:until-2026-10-30" {
		t.Fatalf("notice: %+v", n)
	}
}

func TestAddonTrafficUpgradeSameAddon(t *testing.T) {
	s := sub(1, 1, &vpnctl.RwUser{ExpireAt: until(30 * 24 * time.Hour)})
	s.Addons = []vpnctl.Addon{{ID: 7, AddonID: 4, AddonName: "LTE", TariffID: ptr(20), RwUser: &vpnctl.RwUser{UsedTrafficBytes: 9 * GB, TrafficLimitBytes: 10 * GB, ExpireAt: until(30 * 24 * time.Hour)}}}
	ns := Plan(customers(), []vpnctl.Subscription{s}, tariffs, rules, now)
	if len(ns) != 1 || ns[0].SubID != 1 || ns[0].Title != "Sub1 · LTE" || ns[0].Upgrade == nil || ns[0].Upgrade.ID != 21 {
		t.Fatalf("addon notice: %+v", ns)
	}
	if ns[0].Key[:15] != "traffic:addon:7" {
		t.Fatalf("key = %q", ns[0].Key)
	}
}

func TestNoUpgradeWhenNothingBigger(t *testing.T) {
	s := sub(1, 1, &vpnctl.RwUser{UsedTrafficBytes: 190 * GB, TrafficLimitBytes: 200 * GB, ExpireAt: until(30 * 24 * time.Hour)})
	s.TariffID = ptr(11)
	n := Plan(customers(), []vpnctl.Subscription{s}, tariffs[:2], rules, now)[0]
	if n.Upgrade != nil {
		t.Fatalf("unexpected upgrade %+v", n.Upgrade)
	}
}

func TestReminders(t *testing.T) {
	cases := []struct {
		left  time.Duration
		stage string // "" — no reminder
	}{
		{5 * 24 * time.Hour, ""},
		{72 * time.Hour, "3"},
		{50 * time.Hour, "3"},
		{20 * time.Hour, "1"},
		{-2 * time.Hour, "0"},
		{-4 * 24 * time.Hour, ""}, // ended long ago — don't nag
	}
	for _, c := range cases {
		s := sub(1, 1, &vpnctl.RwUser{ExpireAt: until(c.left)})
		got := byKey(Plan(customers(), []vpnctl.Subscription{s}, tariffs, rules, now))
		key := "remind:sub:1:" + now.Add(c.left).Format("2006-01-02") + ":" + c.stage
		if c.stage == "" {
			if len(got) != 0 {
				t.Errorf("%v left: want nothing, got %v", c.left, got)
			}
			continue
		}
		n, ok := got[key]
		if !ok || n.Kind != Reminder || n.SubID != 1 || n.Expired != (c.left <= 0) {
			t.Errorf("%v left: want %s, got %+v", c.left, key, got)
		}
	}
}

func TestNoRemindersForUnlimited(t *testing.T) {
	s := sub(1, 1, &vpnctl.RwUser{ExpireAt: until(time.Hour), Unlimited: true})
	if ns := Plan(customers(), []vpnctl.Subscription{s}, tariffs, rules, now); len(ns) != 0 {
		t.Fatalf("got %+v", ns)
	}
}

// An add-on ending the same day as its subscription is renewed with it:
// one reminder, not two.
func TestAddonEndingWithSubscriptionIsNotRemindedSeparately(t *testing.T) {
	end := until(48 * time.Hour)
	s := sub(1, 1, &vpnctl.RwUser{ExpireAt: end})
	s.Addons = []vpnctl.Addon{
		{ID: 7, AddonID: 4, AddonName: "same day", RwUser: &vpnctl.RwUser{ExpireAt: ptr(end.Add(time.Hour))}},
		{ID: 8, AddonID: 4, AddonName: "own date", RwUser: &vpnctl.RwUser{ExpireAt: until(20 * time.Hour)}},
	}
	got := byKey(Plan(customers(), []vpnctl.Subscription{s}, tariffs, rules, now))
	if len(got) != 2 {
		t.Fatalf("want the subscription and the add-on with its own date, got %v", got)
	}
	for k := range got {
		if k[:15] == "remind:addon:7:" {
			t.Fatalf("same-day add-on reminded: %v", got)
		}
	}
}
