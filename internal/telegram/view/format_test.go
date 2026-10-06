package view

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	for in, want := range map[float64]string{0: "0 ₽", 100: "100 ₽", 1234.5: "1 234,50 ₽", -50: "−50 ₽", 1000000: "1 000 000 ₽"} {
		if got := Rub(in); got != want {
			t.Errorf("Rub(%v) = %q, want %q", in, got, want)
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
