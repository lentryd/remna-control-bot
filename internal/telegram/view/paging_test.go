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

// Telegram rejects texts over 4096 characters.
func TestScreensFitTelegramLimit(t *testing.T) {
	check(t, "home", Home(ru, manySubs(200), links, now))
	check(t, "traffic", TrafficAlert(ru, fixtureNotice(), links, now))
	check(t, "reminder", PaymentReminder(ru, fixtureNotice(), links, now))
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
