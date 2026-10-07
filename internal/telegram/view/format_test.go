package view

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	for in, want := range map[float64]string{0: "0 ₽", 100: "100 ₽", 1234.5: "1 234,50 ₽", -50: "−50 ₽", 1000000: "1 000 000 ₽"} {
		if got := Money(in); got != want {
			t.Errorf("Money(%v) = %q, want %q", in, got, want)
		}
	}
	d := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	far := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if Date(&d) != "05.03.2026" || Date(nil) != "—" || Date(&far) != "∞" {
		t.Error("Date")
	}
	if Bytes(0) != "∞" || Bytes(50*1<<30) != "50 ГБ" || Bytes(1536*1<<20) != "1.5 ГБ" || Bytes(300*1<<20) != "300 МБ" {
		t.Errorf("Bytes: %q %q %q", Bytes(50*1<<30), Bytes(1536*1<<20), Bytes(300*1<<20))
	}
	if (Links{WebApp: "https://x/bot"}).Page("/balance").URL != "https://x/bot/#/balance" {
		t.Error("Page")
	}
}

func TestMoneyCurrencies(t *testing.T) {
	t.Cleanup(func() { SetCurrency("RUB") })
	cases := []struct {
		cur  string
		in   float64
		want string
	}{
		{"RUB", 1234.5, "1 234,50 ₽"},
		{"USD", 1234.5, "1 234,50 $"},
		{"USD", 500, "500 $"},
		{"EUR", -50, "−50 €"},
		{"CHF", 1234.5, "1 234,50 CHF"},
		{"CHF", 0, "0 CHF"},
		{"EUR", 1000000, "1 000 000 €"},
		{"JPY", 99.99, "99,99 ¥"},
	}
	for _, c := range cases {
		SetCurrency(c.cur)
		if got := Money(c.in); got != c.want {
			t.Errorf("Money(%v) in %s = %q, want %q", c.in, c.cur, got, c.want)
		}
	}
}

func TestSetCurrency(t *testing.T) {
	t.Cleanup(func() { SetCurrency("RUB") })
	if Currency() != "RUB" {
		t.Fatalf("default = %q", Currency())
	}
	SetCurrency("usd")
	if Currency() != "USD" {
		t.Errorf("lower-case not normalised: %q", Currency())
	}
	for _, bad := range []string{"", "US", "USDX", "12$", "ЕВР", "U D"} {
		SetCurrency(bad)
		if Currency() != "USD" {
			t.Errorf("invalid %q accepted: %q", bad, Currency())
		}
	}
}

func TestCurrencyHints(t *testing.T) {
	t.Cleanup(func() { SetCurrency("RUB") })
	SetCurrency("USD")
	if got := CurrencyHints(); len(got) != 2 || got[0] != "$" || got[1] != "USD" {
		t.Errorf("USD hints = %v", got)
	}
	SetCurrency("CHF")
	if got := CurrencyHints(); len(got) != 1 || got[0] != "CHF" {
		t.Errorf("CHF hints = %v", got)
	}
}

func TestPlainStripsCurrentSuffix(t *testing.T) {
	t.Cleanup(func() { SetCurrency("RUB") })
	if got := plain(700); got != "700" {
		t.Errorf("RUB plain = %q", got)
	}
	SetCurrency("USD")
	if got := plain(700); got != "700" {
		t.Errorf("USD plain = %q", got)
	}
	SetCurrency("CHF")
	if got := plain(1234.5); got != "1 234,50" {
		t.Errorf("CHF plain = %q", got)
	}
}
