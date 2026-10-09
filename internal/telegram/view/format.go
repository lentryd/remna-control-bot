package view

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vpn-control-bot/internal/i18n"
)

func Esc(s string) string { return html.EscapeString(s) }

// currencySymbols maps ISO codes to the symbol printed after an amount; any
// other code is printed as is ("1 234,50 CHF").
var currencySymbols = map[string]string{
	"RUB": "₽", "USD": "$", "EUR": "€", "GBP": "£", "UAH": "₴", "KZT": "₸", "TRY": "₺", "BYN": "Br",
	"PLN": "zł", "CZK": "Kč", "GEL": "₾", "AMD": "֏", "CNY": "¥", "JPY": "¥", "INR": "₹", "KRW": "₩",
}

var currentCurrency atomic.Value // string, upper-case ISO code

func init() { currentCurrency.Store("RUB") }

// SetCurrency switches the currency Money prints; a code that isn't three
// ASCII letters is ignored.
func SetCurrency(code string) {
	code = strings.ToUpper(code)
	if len(code) != 3 {
		return
	}
	for i := 0; i < 3; i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return
		}
	}
	currentCurrency.Store(code)
}

// Currency is the ISO code Money currently prints (RUB by default).
func Currency() string { return currentCurrency.Load().(string) }

// suffix is the symbol (or, lacking one, the ISO code) printed after amounts.
func suffix() string {
	code := Currency()
	if sym, ok := currencySymbols[code]; ok {
		return sym
	}
	return code
}

// CurrencyHints lists the spellings of the current currency (symbol, then ISO
// code) that an amount typed by a human may carry.
func CurrencyHints() []string {
	code := Currency()
	if sym, ok := currencySymbols[code]; ok {
		return []string{sym, code}
	}
	return []string{code}
}

// Money formats an amount in the current currency using the language's
// separators (fmt.decimal, fmt.group): "1 234,50 ₽" in ru, "1,234.50 ₽" in en;
// whole amounts have no fraction.
func Money(loc i18n.Localizer, v float64) string {
	cur := suffix()
	sign := ""
	if v < 0 {
		sign, v = "−", -v
	}
	cents := int64(math.Round(v * 100))
	whole := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(loc.T("fmt.group"))
		}
		b.WriteRune(r)
	}
	if frac := cents % 100; frac != 0 {
		return fmt.Sprintf("%s%s%s%02d %s", sign, b.String(), loc.T("fmt.decimal"), frac, cur)
	}
	return sign + b.String() + " " + cur
}

const unlimitedYear = 2090 // vpn-control's "never expires" dates

func Date(loc i18n.Localizer, t *time.Time) string {
	switch {
	case t == nil:
		return "—"
	case t.Year() >= unlimitedYear:
		return "∞"
	}
	return t.Format(loc.T("fmt.date"))
}

func Bytes(loc i18n.Localizer, n int64) string {
	const gb, mb = 1 << 30, 1 << 20
	switch {
	case n <= 0:
		return "∞"
	case n >= gb:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/gb), ".0") + " " + loc.T("unit.gb")
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/mb), ".0") + " " + loc.T("unit.mb")
}
