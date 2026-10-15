package orders

import (
	"testing"

	"remna-control-bot/internal/remnactl"
)

func TestTermCost(t *testing.T) {
	periods := []remnactl.Period{{Months: 3, Price: 250}, {Days: 7, Price: 0}, {Months: 1, Days: 15, Price: 120}}
	cases := []struct {
		months, days int
		want         float64
	}{{0, 7, 0}, {1, 15, 120}, {0, 6, 20}, {3, 0, 250}, {3, 7, 250 + 23.33}, {1, 0, 100}}
	for _, c := range cases {
		if got := TermCost(100, periods, c.months, c.days); got != c.want {
			t.Errorf("TermCost(%d, %d) = %v, want %v", c.months, c.days, got, c.want)
		}
	}
}
