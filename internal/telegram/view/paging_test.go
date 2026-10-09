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
	first := buttons(Subscriptions(ru, p, 0, now))
	if !has(first, CbSub+"1") || has(first, CbSub+"9") || !has(first, CbSubsPage+"1") || has(first, CbSubsPage+"-1") {
		t.Fatalf("page 0: %v", first)
	}
	mid := Subscriptions(ru, p, 1, now)
	b := buttons(mid)
	if !has(b, CbSub+"9") || !has(b, CbSubsPage+"0") || !has(b, CbSubsPage+"2") || !strings.Contains(mid.Text, "2 из 3") {
		t.Fatalf("page 1: %v\n%s", b, mid.Text)
	}
	last := buttons(Subscriptions(ru, p, 99, now)) // clamps to the last page
	if !has(last, CbSub+"20") || has(last, CbSubsPage+"3") {
		t.Fatalf("page 99: %v", last)
	}
	if one := buttons(Subscriptions(ru, manySubs(3), 0, now)); has(one, CbSubsPage+"1") {
		t.Fatalf("single page has nav: %v", one)
	}
}

func TestTariffsPaging(t *testing.T) {
	ts := manyTariffs(12) // 5 per page → 3 pages
	first := Tariffs(ru, ts, 0, links)
	if b := buttons(first); !has(b, CbTariffsPage+"1") || has(b, CbTariffsPage+"-1") || !strings.Contains(first.Text, "Тариф 1") || strings.Contains(first.Text, "Тариф 6") {
		t.Fatalf("page 0: %v\n%s", b, first.Text)
	}
	if b := buttons(Tariffs(ru, ts, 2, links)); !has(b, CbTariffsPage+"1") || has(b, CbTariffsPage+"3") {
		t.Fatalf("page 2: %v", b)
	}
}

// Tariffs are picked only in the WebApp: the bot's catalog has no pick buttons.
func TestTariffsHaveNoPickButtons(t *testing.T) {
	for _, b := range buttons(Tariffs(ru, manyTariffs(3), 0, links)) {
		if strings.HasPrefix(b, "t:") {
			t.Fatalf("pick button %q in the bot", b)
		}
	}
}

func TestSubscriptionBackGoesToItsPage(t *testing.T) {
	s, _ := Subscription(ru, manySubs(20), 17, links, now)
	if b := buttons(s); !has(b, CbSubsPage+"2") {
		t.Fatalf("back button: %v", b)
	}
}

// Telegram rejects texts over 4096 characters.
func TestScreensFitTelegramLimit(t *testing.T) {
	p := manySubs(200)
	for page := 0; page < 25; page++ {
		check(t, fmt.Sprintf("subscriptions %d", page), Subscriptions(ru, p, page, now))
	}
	s, _ := Subscription(ru, p, 1, links, now)
	check(t, "subscription", s)
	ts := manyTariffs(50)
	for page := 0; page < 10; page++ {
		check(t, fmt.Sprintf("tariffs %d", page), Tariffs(ru, ts, page, links))
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
