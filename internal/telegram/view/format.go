package view

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"
)

func Esc(s string) string { return html.EscapeString(s) }

// Rub formats rubles Russian-style: "1 234,50 ₽", whole amounts without kopecks.
func Rub(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "−", -v
	}
	cents := int64(math.Round(v * 100))
	whole := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if frac := cents % 100; frac != 0 {
		return fmt.Sprintf("%s%s,%02d ₽", sign, b.String(), frac)
	}
	return sign + b.String() + " ₽"
}

const unlimitedYear = 2090 // vpn-control's "never expires" dates

func Date(t *time.Time) string {
	switch {
	case t == nil:
		return "—"
	case t.Year() >= unlimitedYear:
		return "∞"
	}
	return t.Format("02.01.2006")
}

func Bytes(n int64) string {
	const gb, mb = 1 << 30, 1 << 20
	switch {
	case n <= 0:
		return "∞"
	case n >= gb:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/gb), ".0") + " ГБ"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/mb), ".0") + " МБ"
}
