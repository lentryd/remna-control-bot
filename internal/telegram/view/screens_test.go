package view

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/profile"
)

var update = flag.Bool("update", false, "rewrite golden files")

var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func fixture() profile.Profile {
	return profile.Profile{
		ID: 1, Name: "Ann", Balance: 1234.5, BalanceOwn: 1000, BalanceReferral: 234.5, Monthly: 300,
		NearestExpireAt: ptr(now.Add(48 * time.Hour)),
		Subscriptions: []profile.Subscription{{
			ID: 3, Title: "Основная", Tariff: "Стандарт", Price: 250, Status: "ACTIVE", ExpireAt: ptr(now.Add(48 * time.Hour)),
			UsedBytes: 10 << 30, LimitBytes: 100 << 30, DeviceLimit: ptr(3), URL: "https://sub.x/AbC123xyz",
			Addons: []profile.Addon{{ID: 9, Name: "YouTube", Tariff: "YT", Price: 50, Status: "ACTIVE", ExpireAt: ptr(now.AddDate(0, 1, 0))}},
		}},
		Ledger: []profile.LedgerEntry{
			{Type: "charge", Amount: -250, Date: now.AddDate(0, 0, -1)},
			{Type: "payment", Amount: 500, Date: now.AddDate(0, 0, -2)},
		},
		Referrals:       []profile.Referral{{Name: "Bob", Active: true, Earned: 75}},
		ReferralsEarned: 75,
		RefLink:         "https://t.me/vpnbot?start=ref_Ab12cd",
	}
}

var links = Links{WebApp: "https://x.dev/bot"}

func render(s Screen) string {
	var b strings.Builder
	b.WriteString(s.Text)
	b.WriteString("\n---\n")
	if s.Keyboard != nil {
		for _, row := range s.Keyboard.InlineKeyboard {
			for i, k := range row {
				if i > 0 {
					b.WriteString(" | ")
				}
				switch {
				case k.WebApp != nil:
					fmt.Fprintf(&b, "[%s → app %s]", k.Text, k.WebApp.URL)
				case k.URL != "":
					fmt.Fprintf(&b, "[%s → %s]", k.Text, k.URL)
				case k.CopyText != nil:
					fmt.Fprintf(&b, "[%s ⧉ %s]", k.Text, k.CopyText.Text)
				default:
					fmt.Fprintf(&b, "[%s · %s]", k.Text, k.CallbackData)
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func golden(t *testing.T, name string, s Screen) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got := render(s)
	if *update {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestScreens(t *testing.T) {
	p := fixture()
	golden(t, "home", Home(p, links, now))
	golden(t, "subscriptions", Subscriptions(p, 0, now))
	sub, ok := Subscription(p, 3, links, now)
	if !ok {
		t.Fatal("subscription 3 not found")
	}
	golden(t, "subscription", sub)
	golden(t, "balance", Balance(p, links))
	golden(t, "referrals", Referrals(p, links))
	golden(t, "tariffs", Tariffs([]profile.Tariff{
		{ID: 1, Kind: "base", Name: "Стандарт", Description: "Все страны", MonthlyPrice: 250, TrafficLimitBytes: 100 << 30, HwidLimit: ptr(3)},
		{ID: 2, Kind: "addon", Name: "YT", AddonName: "YouTube", MonthlyPrice: 50},
	}, 0, links))
}

func TestEmptyStates(t *testing.T) {
	p := profile.Profile{Name: "New", RefLink: "https://t.me/vpnbot?start=ref_x"}
	golden(t, "home_empty", Home(p, links, now))
	golden(t, "subscriptions_empty", Subscriptions(p, 0, now))
	golden(t, "referrals_empty", Referrals(p, links))
	if _, ok := Subscription(p, 1, links, now); ok {
		t.Error("missing subscription reported as found")
	}
}

func TestHomeEscapesName(t *testing.T) {
	p := fixture()
	p.Name = "<b>A&B</b>"
	if txt := Home(p, links, now).Text; !strings.Contains(txt, "&lt;b&gt;A&amp;B&lt;/b&gt;") {
		t.Fatalf("name not escaped: %s", txt)
	}
}
