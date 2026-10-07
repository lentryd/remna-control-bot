package orders

import (
	"math"

	"vpn-control-bot/internal/vpnctl"
)

// TermCost prices months+days of a tariff like vpn-control does (keep in
// sync with its internal/billing/pricing.go): an exact package wins,
// otherwise the cheapest mix of whole-month periods plus monthly/30 a day.
func TermCost(monthly float64, periods []vpnctl.Period, months, days int) float64 {
	if days > 0 {
		for _, p := range periods {
			if p.Months == months && p.Days == days {
				return p.Price
			}
		}
	}
	m := kop(monthly)
	best := make([]int64, max(months, 0)+1)
	for i := 1; i <= months; i++ {
		best[i] = best[i-1] + m
		for _, p := range periods {
			if p.Days == 0 && p.Months > 0 && p.Months <= i && best[i-p.Months]+kop(p.Price) < best[i] {
				best[i] = best[i-p.Months] + kop(p.Price)
			}
		}
	}
	total := best[max(months, 0)] + int64(math.Round(float64(m)*float64(days)/30))
	return float64(total) / 100
}

func kop(v float64) int64      { return int64(math.Round(v * 100)) }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
