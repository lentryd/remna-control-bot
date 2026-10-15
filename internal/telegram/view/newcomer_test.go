package view

import (
	"strings"
	"testing"
)

// What people without access see (onboarding, a friend's invite) says
// nothing about VPN and calls the referral link "a friend's link".
func TestNewcomerTextsAreNeutral(t *testing.T) {
	for name, text := range map[string]string{
		"need link": ru.T("msg.need_link"), "bad referral": ru.T("msg.bad_referral"), "bad link": ru.T("msg.bad_link"),
		"not found": ru.T("msg.not_found"), "taken": ru.T("msg.taken"),
	} {
		low := strings.ToLower(text)
		if strings.Contains(low, "vpn") || strings.Contains(low, "впн") || strings.Contains(low, "реферальн") {
			t.Errorf("%s: %q", name, text)
		}
	}
	if !strings.Contains(ru.T("msg.need_link"), "по его ссылке") {
		t.Errorf("need link should mention a friend's link: %q", ru.T("msg.need_link"))
	}
}
