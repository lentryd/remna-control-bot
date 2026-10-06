package view

import "testing"

func TestHomeOpensAppFirst(t *testing.T) {
	for _, s := range []Screen{Home(fixture(), links, now), Home(fixture(), links, now.AddDate(1, 0, 0))} {
		first := s.Keyboard.InlineKeyboard[0][0]
		if first.WebApp == nil || first.Text != BtnApp {
			t.Fatalf("first button = %+v", first)
		}
	}
}

// The subscription page has the setup instructions: open it inside Telegram
// or in the browser.
func TestSubscriptionLinkOpens(t *testing.T) {
	s, _ := Subscription(fixture(), 3, links, now)
	var inApp, inBrowser bool
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			inApp = inApp || (b.WebApp != nil && b.WebApp.URL == "https://sub.x/AbC123xyz")
			inBrowser = inBrowser || b.URL == "https://sub.x/AbC123xyz"
		}
	}
	if !inApp || !inBrowser {
		t.Fatalf("open in Telegram %v, in browser %v: %+v", inApp, inBrowser, s.Keyboard.InlineKeyboard)
	}
}
