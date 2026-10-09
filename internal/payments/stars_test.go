package payments

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"vpn-control-bot/internal/i18n"
)

func TestStarsCount(t *testing.T) {
	s := &Stars{PerStar: 1.5}
	for rub, want := range map[float64]int{150: 100, 100.01: 67, 1: 1, 0.5: 1, 800: 534, 1.5: 1} {
		if got := s.Count(rub); got != want {
			t.Errorf("Count(%v) = %d, want %d", rub, got, want)
		}
	}
}

func TestStarsInvoice(t *testing.T) {
	var gotTitle, gotDesc, gotPayload string
	var gotStars int
	s := &Stars{PerStar: 2, Link: func(_ context.Context, title, desc, payload string, stars int) (string, error) {
		gotTitle, gotDesc, gotPayload, gotStars = title, desc, payload, stars
		return "https://t.me/$abc", nil
	}}
	inv, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "ord-1", Title: "Продление «Дом» · 3 мес.", Amount: 800})
	if err != nil || inv.URL != "https://t.me/$abc" || inv.Amount != 400 || inv.Currency != "XTR" || inv.ExternalID != "" {
		t.Fatalf("%+v %v", inv, err)
	}
	if gotPayload != "ord-1" || gotStars != 400 || gotTitle != "VPN payment" || gotDesc != "Продление «Дом» · 3 мес." {
		t.Errorf("link args: %q %q %q %d", gotTitle, gotDesc, gotPayload, gotStars)
	}
	ru := i18n.NewContext(t.Context(), i18n.Default.For("ru"))
	if _, err := s.Invoice(ru, InvoiceRequest{Currency: "RUB", OrderID: "ord-1", Title: "t", Amount: 800}); err != nil || gotTitle != "Оплата VPN" {
		t.Errorf("the invoice title is in the payer's language: %q %v", gotTitle, err)
	}
	if len([]rune(gotTitle)) > 32 || len([]rune(i18n.Default.For("en").T("invoice.title"))) > 32 {
		t.Error("Telegram allows at most 32 characters in an invoice title")
	}
	s.Link = func(context.Context, string, string, string, int) (string, error) {
		return "", errors.New("telegram down")
	}
	if _, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 10}); err == nil {
		t.Error("want the link error")
	}
	if _, err := (&Stars{PerStar: 2}).Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 10}); err == nil {
		t.Error("want an error before the bot is wired")
	}
}

func TestStarsInvoiceRejectsUnusableInput(t *testing.T) {
	calls := 0
	link := func(context.Context, string, string, string, int) (string, error) {
		calls++
		return "https://t.me/$abc", nil
	}
	cases := map[string]struct{ rate, amount float64 }{
		"rate zero":         {0, 100},
		"rate negative":     {-1, 100},
		"rate NaN":          {math.NaN(), 100},
		"rate +Inf":         {math.Inf(1), 100},
		"amount zero":       {2, 0},
		"amount negative":   {2, -5},
		"amount NaN":        {2, math.NaN()},
		"amount +Inf":       {2, math.Inf(1)},
		"amount huge":       {2, 1e300},
		"just over the cap": {1, 2_500_001},
		"tiny rate":         {1e-300, 100},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := &Stars{PerStar: c.rate, Link: link}
			if inv, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: c.amount}); err == nil {
				t.Fatalf("want an error, got %+v", inv)
			}
		})
	}
	if calls != 0 {
		t.Errorf("a rejected invoice must not reach Telegram: %d link calls", calls)
	}
	s := &Stars{PerStar: 1, Link: link}
	inv, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 2_500_000})
	if err != nil || inv.Amount != 2_500_000 {
		t.Errorf("the cap itself is allowed: %+v %v", inv, err)
	}
	inv, err = s.Invoice(t.Context(), InvoiceRequest{Currency: "RUB", OrderID: "o", Title: "t", Amount: 0.01})
	if err != nil || inv.Amount != 1 {
		t.Errorf("a positive amount costs at least one star: %+v %v", inv, err)
	}
}

func TestStarsErrorsNameTheGivenCurrency(t *testing.T) {
	s := &Stars{PerStar: 1, Link: func(context.Context, string, string, string, int) (string, error) {
		return "https://t.me/$abc", nil
	}}
	_, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "USD", OrderID: "o", Title: "t", Amount: 3_000_000})
	if err == nil || !strings.Contains(err.Error(), "USD") || strings.Contains(err.Error(), "RUB") {
		t.Errorf("the cap error must name the given currency: %v", err)
	}
	// The invoice itself is in stars whatever the base currency is.
	inv, err := s.Invoice(t.Context(), InvoiceRequest{Currency: "USD", OrderID: "o", Title: "t", Amount: 5})
	if err != nil || inv.Currency != "XTR" || inv.Amount != 5 {
		t.Errorf("%+v %v", inv, err)
	}
}
