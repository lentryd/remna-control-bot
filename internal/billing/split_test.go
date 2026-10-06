package billing

import (
	"testing"

	"vpn-control-bot/internal/config"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		name             string
		mode             config.PayMode
		price, own, ref  float64
		fromOwn, fromRef float64
		toPay            float64
	}{
		{"mixed: referral first, then own", config.PayMixed, 300, 200, 150, 150, 150, 0},
		{"mixed: not enough, rest to pay", config.PayMixed, 300, 50, 100, 50, 100, 150},
		{"mixed: referral alone covers", config.PayMixed, 300, 0, 500, 0, 300, 0},
		{"full: referral covers all", config.PayReferralFull, 300, 1000, 300, 0, 300, 0},
		{"full: referral short — untouched, own pays", config.PayReferralFull, 300, 1000, 299, 300, 0, 0},
		{"full: referral short, own short", config.PayReferralFull, 300, 100, 200, 100, 0, 200},
		{"negative own counts as zero", config.PayMixed, 300, -50, 100, 0, 100, 200},
		{"free", config.PayMixed, 0, 10, 10, 0, 0, 0},
	}
	for _, c := range cases {
		got := Split(c.price, c.own, c.ref, c.mode)
		want := Payment{Price: c.price, FromOwn: c.fromOwn, FromReferral: c.fromRef, ToPay: c.toPay}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, want)
		}
	}
}
