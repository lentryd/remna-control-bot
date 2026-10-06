package view

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"vpn-control-bot/internal/profile"
)

func manySubs(n int) profile.Profile {
	p := profile.Profile{Name: "Ann"}
	for i := 1; i <= n; i++ {
		p.Subscriptions = append(p.Subscriptions, profile.Subscription{
			ID: i, Title: fmt.Sprintf("Подписка %d %s", i, strings.Repeat("я", 300)), Status: "ACTIVE", ExpireAt: ptr(now.AddDate(0, 1, 0)),
			Addons: make([]profile.Addon, 40),
		})
	}
	return p
}

func manyTariffs(n int) []profile.Tariff {
	var ts []profile.Tariff
	for i := 1; i <= n; i++ {
		ts = append(ts, profile.Tariff{ID: i, Kind: "base", Name: fmt.Sprintf("Тариф %d", i), Description: strings.Repeat("описание ", 200), MonthlyPrice: 100})
	}
	return ts
}

// buttons returns callback data of every button, in order.
func buttons(s Screen) []string {
	var out []string
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			out = append(out, b.CallbackData)
		}
	}
	return out
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestSubscriptionsPaging(t *testing.T) {
	p := manySubs(20) // 8 per page → 3 pages
	first := buttons(Subscriptions(p, 0, now))
	if !has(first, CbSub+"1") || has(first, CbSub+"9") || !has(first, CbSubsPage+"1") || has(first, CbSubsPage+"-1") {
		t.Fatalf("page 0: %v", first)
	}
	mid := Subscriptions(p, 1, now)
	b := buttons(mid)
	if !has(b, CbSub+"9") || !has(b, CbSubsPage+"0") || !has(b, CbSubsPage+"2") || !strings.Contains(mid.Text, "2 из 3") {
		t.Fatalf("page 1: %v\n%s", b, mid.Text)
	}
	last := buttons(Subscriptions(p, 99, now)) // clamps to the last page
	if !has(last, CbSub+"20") || has(last, CbSubsPage+"3") {
		t.Fatalf("page 99: %v", last)
	}
	if one := buttons(Subscriptions(manySubs(3), 0, now)); has(one, CbSubsPage+"1") {
		t.Fatalf("single page has nav: %v", one)
	}
}

func TestTariffsPaging(t *testing.T) {
	ts := manyTariffs(12) // 5 per page → 3 pages
	if b := buttons(Tariffs(ts, 0, links)); !has(b, CbTariff+"1") || has(b, CbTariff+"6") || !has(b, CbTariffsPage+"1") {
		t.Fatalf("page 0: %v", b)
	}
	if b := buttons(Tariffs(ts, 2, links)); !has(b, CbTariff+"11") || !has(b, CbTariffsPage+"1") || has(b, CbTariffsPage+"3") {
		t.Fatalf("page 2: %v", b)
	}
}

func TestSubscriptionBackGoesToItsPage(t *testing.T) {
	s, _ := Subscription(manySubs(20), 17, links, now)
	if b := buttons(s); !has(b, CbSubsPage+"2") {
		t.Fatalf("back button: %v", b)
	}
}

// Telegram rejects texts over 4096 characters.
func TestScreensFitTelegramLimit(t *testing.T) {
	p := manySubs(200)
	for page := 0; page < 25; page++ {
		check(t, fmt.Sprintf("subscriptions %d", page), Subscriptions(p, page, now))
	}
	s, _ := Subscription(p, 1, links, now)
	check(t, "subscription", s)
	ts := manyTariffs(50)
	for page := 0; page < 10; page++ {
		check(t, fmt.Sprintf("tariffs %d", page), Tariffs(ts, page, links))
	}
}

func check(t *testing.T, name string, s Screen) {
	t.Helper()
	if n := utf8.RuneCountInString(s.Text); n > 4096 {
		t.Errorf("%s: %d chars", name, n)
	}
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if len(b.CallbackData) > 64 || utf8.RuneCountInString(b.Text) > 64 {
				t.Errorf("%s: button too long: %q", name, b.Text)
			}
		}
	}
}
