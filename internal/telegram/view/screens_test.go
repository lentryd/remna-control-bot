package view

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/i18n"
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

var langs = []string{"en", "ru"}

// ru is the localizer for tests that assert Russian wording.
var ru = i18n.Default.For("ru")

var rawKey = regexp.MustCompile(`(^|[\s>(])(btn|msg|home|notice|subs?|tariffs|balance|refs|orders?|admin|status|ledger|time|unit|fmt|term|method|provider|payment|alert|review|title)\.[a-z_]+`)

func noRawKeys(t *testing.T, s Screen) {
	t.Helper()
	if m := rawKey.FindString(s.Text); m != "" {
		t.Errorf("screen text contains raw key %q: %q", strings.TrimSpace(m), s.Text)
	}
}

func golden(t *testing.T, lang, name string, s Screen) {
	t.Helper()
	noRawKeys(t, s)
	path := filepath.Join("testdata", lang, name+".golden")
	got := render(s)
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s/%s mismatch:\n--- got\n%s\n--- want\n%s", lang, name, got, want)
	}
}

func TestScreens(t *testing.T) {
	for _, lang := range langs {
		loc := i18n.Default.For(lang)
		p := fixture()
		golden(t, lang, "home", Home(loc, p, links, now))
		golden(t, lang, "subscriptions", Subscriptions(loc, p, 0, now))
		sub, ok := Subscription(loc, p, 3, links, now)
		if !ok {
			t.Fatal("subscription 3 not found")
		}
		golden(t, lang, "subscription", sub)
		golden(t, lang, "balance", Balance(loc, p, links))
		golden(t, lang, "referrals", Referrals(loc, p, links))
		golden(t, lang, "tariffs", Tariffs(loc, []profile.Tariff{
			{ID: 1, Kind: "base", Name: "Стандарт", Description: "Все страны", MonthlyPrice: 250, TrafficLimitBytes: 100 << 30, HwidLimit: ptr(3)},
			{ID: 2, Kind: "addon", Name: "YT", AddonName: "YouTube", MonthlyPrice: 50},
		}, 0, links))
	}
}

func TestEmptyStates(t *testing.T) {
	for _, lang := range langs {
		loc := i18n.Default.For(lang)
		p := profile.Profile{Name: "New", RefLink: "https://t.me/vpnbot?start=ref_x"}
		golden(t, lang, "home_empty", Home(loc, p, links, now))
		golden(t, lang, "subscriptions_empty", Subscriptions(loc, p, 0, now))
		golden(t, lang, "referrals_empty", Referrals(loc, p, links))
		if _, ok := Subscription(loc, p, 1, links, now); ok {
			t.Error("missing subscription reported as found")
		}
	}
}

func TestHomeEscapesName(t *testing.T) {
	p := fixture()
	p.Name = "<b>A&B</b>"
	if txt := Home(ru, p, links, now).Text; !strings.Contains(txt, "&lt;b&gt;A&amp;B&lt;/b&gt;") {
		t.Fatalf("name not escaped: %s", txt)
	}
}

func TestLeft(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]map[int]string{
		"en": {0: "today", 1: "in 1 day", 3: "in 3 days"},
		"ru": {0: "сегодня", 1: "через 1 день", 2: "через 2 дня", 5: "через 5 дней", 11: "через 11 дней", 12: "через 12 дней", 14: "через 14 дней", 21: "через 21 день"},
	}
	for lang, m := range cases {
		loc := i18n.Default.For(lang)
		for days, want := range m {
			if got := left(loc, now.Add(time.Duration(days)*24*time.Hour+time.Hour), now); got != want {
				t.Errorf("%s %d: got %q, want %q", lang, days, got, want)
			}
		}
	}
}
