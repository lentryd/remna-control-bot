package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"remna-control-bot/internal/settings"
)

func snapshot(secs ...settings.Section) settings.Snapshot {
	s := settings.Defaults()
	for _, sec := range secs {
		s = s.With(sec)
	}
	return s
}

func TestBuildOrderAndDefaults(t *testing.T) {
	s := snapshot(
		&settings.YooKassa{Enabled: true, ShopID: "1", SecretKey: "k", VATCode: 3},
		&settings.CryptoBot{Enabled: true, Token: "t", Testnet: true, Assets: "TON"},
		&settings.Heleket{Enabled: true, MerchantID: "m", APIKey: "a"},
		&settings.Pal24{Enabled: true, APIToken: "p", ShopID: "s"},
		&settings.Platega{Enabled: true, MerchantID: "m", Secret: "s", PaymentMethod: 11, APIVersion: "v2"},
		&settings.Stars{Enabled: true, Rate: 1.5},
	)
	linked := false
	r := Build(BuildInput{Settings: s, PublicURL: "https://pub.example", WebAppURL: "https://pub.example/bot",
		StarsLink: func(context.Context, string, string, string, int) (string, error) { linked = true; return "l", nil }})
	if want := []string{"yookassa", "cryptobot", "heleket", "pal24", "platega", "stars"}; !slices.Equal(r.IDs(), want) {
		t.Fatalf("ids %v", r.IDs())
	}
	y, _ := r.Get("yookassa")
	if yk := y.(*YooKassa); yk.ReturnURL != "https://pub.example/bot/" || yk.VAT != 3 {
		t.Fatalf("yookassa %+v", yk)
	}
	h, _ := r.Get("heleket")
	if hk := h.(*Heleket); hk.CallbackURL != "https://pub.example/pay/heleket" {
		t.Fatalf("heleket callback %q", hk.CallbackURL)
	}
	pl, _ := r.Get("platega")
	if p := pl.(*Platega); p.ReturnURL != "https://pub.example/bot/" || p.PaymentMethod != 11 || p.APIVersion != "v2" {
		t.Fatalf("platega %+v", p)
	}
	st, _ := r.Get("stars")
	if stars := st.(*Stars); stars.PerStar != 1.5 || stars.Link == nil {
		t.Fatalf("stars %+v", stars)
	} else if _, err := stars.Link(context.Background(), "", "", "", 1); err != nil || !linked {
		t.Fatal("stars link must be the one passed in")
	}
}

func TestBuildSkipsIncomplete(t *testing.T) {
	s := snapshot(
		&settings.YooKassa{Enabled: true, VATCode: 1},  // enabled but no keys: invalid, never built
		&settings.CryptoBot{Enabled: false, Token: ""}, // off and empty
	)
	r := Build(BuildInput{Settings: s})
	if len(r.IDs()) != 0 {
		t.Fatalf("ids %v", r.IDs())
	}
	if _, ok := r.Get("yookassa"); ok {
		t.Fatal("an incomplete provider must not resolve")
	}
}

func TestRetiredProviderStillResolves(t *testing.T) {
	s := snapshot(&settings.CryptoBot{Enabled: false, Token: "t"}) // switched off, credentials kept
	r := Build(BuildInput{Settings: s})
	if len(r.IDs()) != 0 || len(r.Enabled()) != 0 {
		t.Fatalf("a switched-off provider must not be offered: %v", r.IDs())
	}
	if _, ok := r.Offer("cryptobot"); ok {
		t.Fatal("Offer must not return a retired provider")
	}
	if _, ok := r.Get("cryptobot"); !ok {
		t.Fatal("Get must still resolve it: invoices issued before the switch-off are paid later")
	}
}

func TestRetireOnNilRegistry(t *testing.T) {
	var r *Registry
	r = r.Retire(&CryptoBot{Token: "t"})
	if _, ok := r.Get("cryptobot"); !ok {
		t.Fatal("retired provider must resolve after Retire on nil")
	}
	if _, ok := r.Offer("cryptobot"); ok {
		t.Fatal("must not be offered")
	}
}

func TestCheckers(t *testing.T) {
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getMe" || r.Header.Get("Crypto-Pay-API-Token") != "tok" {
			http.Error(w, `{"ok":false}`, 401)
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"name":"Shop"}}`))
	}))
	defer cb.Close()
	msg, err := (&CryptoBot{Token: "tok", BaseURL: cb.URL}).Check(context.Background())
	if err != nil || msg == "" {
		t.Fatalf("cryptobot: %q %v", msg, err)
	}
	if _, err := (&CryptoBot{Token: "bad", BaseURL: cb.URL}).Check(context.Background()); err == nil {
		t.Fatal("cryptobot accepted a bad token")
	}

	yk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if r.URL.Path != "/me" || u != "42" || p != "sec" {
			http.Error(w, `{}`, 401)
			return
		}
		w.Write([]byte(`{"account_id":"42","status":"enabled"}`))
	}))
	defer yk.Close()
	if msg, err := (&YooKassa{ShopID: "42", Secret: "sec", BaseURL: yk.URL}).Check(context.Background()); err != nil || msg == "" {
		t.Fatalf("yookassa: %q %v", msg, err)
	}
	if _, err := (&YooKassa{ShopID: "42", Secret: "no", BaseURL: yk.URL}).Check(context.Background()); err == nil {
		t.Fatal("yookassa accepted a bad secret")
	}
}
