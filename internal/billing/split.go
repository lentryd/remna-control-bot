package billing

import (
	"math"

	"vpn-control-bot/internal/config"
)

// Payment is how a price is covered: from the customer's own funds, from
// referral money, and what is left to pay by other means.
type Payment struct {
	Price        float64 `json:"price"`
	FromOwn      float64 `json:"from_own"`
	FromReferral float64 `json:"from_referral"`
	ToPay        float64 `json:"to_pay"`
}

// Split applies BALANCE_PAY_MODE. Referral money is spent first in mixed
// mode; in referral_full mode it is spent only when it covers the whole
// price, otherwise only own funds are used.
func Split(price, own, referral float64, mode config.PayMode) Payment {
	p := Payment{Price: price}
	own, referral = math.Max(own, 0), math.Max(referral, 0)
	left := price
	if mode == config.PayMixed || referral >= price {
		p.FromReferral = math.Min(referral, left)
		left -= p.FromReferral
	}
	p.FromOwn = math.Min(own, left)
	p.ToPay = round(left - p.FromOwn)
	p.FromOwn, p.FromReferral = round(p.FromOwn), round(p.FromReferral)
	return p
}

func round(v float64) float64 { return math.Round(v*100) / 100 }
