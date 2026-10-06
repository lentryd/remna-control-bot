package view

import (
	"net/url"
	"strings"
	"testing"
)

// What people without access see (onboarding, a friend's invite) says
// nothing about VPN and calls the referral link "a friend's link".
func TestNewcomerTextsAreNeutral(t *testing.T) {
	share := ""
	for _, row := range Referrals(fixture(), links).Keyboard.InlineKeyboard {
		for _, b := range row {
			if strings.HasPrefix(b.URL, "https://t.me/share/") {
				u, _ := url.Parse(b.URL)
				share = u.Query().Get("text")
			}
		}
	}
	for name, text := range map[string]string{
		"need link": TxtNeedLink, "bad referral": TxtBadReferral, "bad link": TxtBadLink,
		"not found": TxtNotFound, "taken": TxtTaken, "share text": share,
	} {
		low := strings.ToLower(text)
		if strings.Contains(low, "vpn") || strings.Contains(low, "впн") || strings.Contains(low, "реферальн") {
			t.Errorf("%s: %q", name, text)
		}
	}
	if !strings.Contains(TxtNeedLink, "по ссылке друга") {
		t.Errorf("need link should mention a friend's link: %q", TxtNeedLink)
	}
}
