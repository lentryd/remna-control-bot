package config

import (
	"fmt"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{
		"BOT_TOKEN": "t", "VPNCTL_URL": "http://p:8080", "VPNCTL_TOKEN": "x",
		"PUBLIC_URL": "https://vpn.example.com/", "REF_SECRET": "s",
	}
}

func TestDefaults(t *testing.T) {
	c, err := load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeClosed || c.WebAppPath != "/bot" || c.HTTPAddr != ":8090" || c.DBPath != "/data/bot.db" || c.LogLevel != "info" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if got := c.WebAppURL(); got != "https://vpn.example.com/bot" {
		t.Fatalf("WebAppURL = %q", got)
	}
}

func TestWebAppPathNormalized(t *testing.T) {
	for in, want := range map[string]string{"bot/": "/bot", "/a/b/": "/a/b", "/": ""} {
		m := base()
		m["WEBAPP_PATH"] = in
		c, err := load(env(m))
		if err != nil {
			t.Fatal(err)
		}
		if c.WebAppPath != want {
			t.Errorf("%q → %q, want %q", in, c.WebAppPath, want)
		}
	}
}

func TestRequiredAndMode(t *testing.T) {
	for _, k := range []string{"BOT_TOKEN", "VPNCTL_URL", "VPNCTL_TOKEN", "PUBLIC_URL", "REF_SECRET"} {
		m := base()
		delete(m, k)
		if _, err := load(env(m)); err == nil {
			t.Errorf("missing %s: want error", k)
		}
	}
	m := base()
	m["BOT_MODE"] = "public"
	if _, err := load(env(m)); err == nil {
		t.Error("bad mode: want error")
	}
	m["BOT_MODE"] = "open"
	if c, _ := load(env(m)); c.Mode != ModeOpen {
		t.Error("open mode not parsed")
	}
}

func TestNotifyAndPayDefaults(t *testing.T) {
	c, err := load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.PayMode != PayMixed || c.NotifyInterval != time.Hour || c.TrafficAlertPct != 80 || fmt.Sprint(c.RemindDays) != "[3 1]" {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestNotifyAndPayParsing(t *testing.T) {
	m := base()
	m["BALANCE_PAY_MODE"] = "referral_full"
	m["NOTIFY_INTERVAL"] = "0"
	m["TRAFFIC_ALERT_PCT"] = "90"
	m["REMIND_DAYS"] = " 1, 7 ,3"
	c, err := load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.PayMode != PayReferralFull || c.NotifyInterval != 0 || c.TrafficAlertPct != 90 || fmt.Sprint(c.RemindDays) != "[7 3 1]" {
		t.Fatalf("parsed: %+v", c)
	}
	for k, v := range map[string]string{"BALANCE_PAY_MODE": "all", "NOTIFY_INTERVAL": "soon", "TRAFFIC_ALERT_PCT": "120", "REMIND_DAYS": "x"} {
		m := base()
		m[k] = v
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s=%s: want error", k, v)
		}
	}
}

func TestManualPayment(t *testing.T) {
	base := map[string]string{"BOT_TOKEN": "t", "VPNCTL_URL": "u", "VPNCTL_TOKEN": "k", "PUBLIC_URL": "p", "REF_SECRET": "s"}
	env := func(extra map[string]string) func(string) string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return func(k string) string { return m[k] }
	}
	c, err := load(env(nil))
	if err != nil || c.ManualEnabled() || c.OrderTTL != 48*time.Hour {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = load(env(map[string]string{"ADMIN_CHAT_ID": "-100123", "ADMIN_IDS": "5, 7", "MANUAL_PAY_DETAILS": "Карта 2200", "ORDER_TTL": "24h"}))
	if err != nil || !c.ManualEnabled() || c.AdminChatID != -100123 || !c.IsAdmin(7) || c.IsAdmin(8) || c.OrderTTL != 24*time.Hour {
		t.Fatalf("manual: %+v %v", c, err)
	}
	for _, bad := range []map[string]string{{"ADMIN_IDS": "x"}, {"ADMIN_CHAT_ID": "x"}, {"ORDER_TTL": "-1h"}} {
		if _, err := load(env(bad)); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestPayProviders(t *testing.T) {
	c, err := load(env(base()))
	if err != nil || c.Pay.YooKassaOn() || c.Pay.CryptoBotOn() || c.Pay.HeleketOn() || c.Pay.StarsOn() || c.Pay.YooKassaVAT != 1 {
		t.Fatalf("defaults: %+v %v", c.Pay, err)
	}
	m := base()
	for k, v := range map[string]string{
		"YOOKASSA_SHOP_ID": "123", "YOOKASSA_SECRET_KEY": "sk", "YOOKASSA_VAT_CODE": "4", "YOOKASSA_RECEIPT_EMAIL": "a@b.c",
		"CRYPTOBOT_TOKEN": "tok", "CRYPTOBOT_TESTNET": "true", "CRYPTOBOT_ASSETS": "USDT,TON",
		"HELEKET_MERCHANT_ID": "m", "HELEKET_API_KEY": "k", "HELEKET_TO_CURRENCY": "USDT", "STARS_RATE": "1.5",
	} {
		m[k] = v
	}
	c, err = load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Pay
	if !p.YooKassaOn() || !p.CryptoBotOn() || !p.HeleketOn() || !p.StarsOn() || p.YooKassaVAT != 4 || !p.CryptoBotTestnet ||
		p.CryptoBotAssets != "USDT,TON" || p.StarsRate != 1.5 || p.HeleketToCurrency != "USDT" || p.YooKassaReceiptEmail != "a@b.c" {
		t.Fatalf("configured: %+v", p)
	}
	if p.YooKassaReturnURL != "https://vpn.example.com/bot/" {
		t.Errorf("default return url %q", p.YooKassaReturnURL)
	}
	for _, bad := range []map[string]string{
		{"YOOKASSA_SHOP_ID": "1"}, {"YOOKASSA_SECRET_KEY": "s"}, {"HELEKET_MERCHANT_ID": "m"}, {"HELEKET_API_KEY": "k"},
		{"YOOKASSA_VAT_CODE": "9"}, {"CRYPTOBOT_TESTNET": "maybe"}, {"STARS_RATE": "-1"}, {"STARS_RATE": "abc"},
	} {
		m := base()
		for k, v := range bad {
			m[k] = v
		}
		if _, err := load(env(m)); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestBaseCurrencySet(t *testing.T) {
	c, err := load(env(base()))
	if err != nil || c.BaseCurrencySet {
		t.Fatalf("unset: set=%v %v", c.BaseCurrencySet, err)
	}
	m := base()
	m["BASE_CURRENCY"] = "   "
	if c, err = load(env(m)); err != nil || c.BaseCurrencySet || c.BaseCurrency != "RUB" {
		t.Fatalf("blank: set=%v %q %v", c.BaseCurrencySet, c.BaseCurrency, err)
	}
	m["BASE_CURRENCY"] = " usd "
	if c, err = load(env(m)); err != nil || !c.BaseCurrencySet || c.BaseCurrency != "USD" {
		t.Fatalf("set: set=%v %q %v", c.BaseCurrencySet, c.BaseCurrency, err)
	}
}

func TestBaseCurrency(t *testing.T) {
	c, err := load(env(base()))
	if err != nil || c.BaseCurrency != "RUB" {
		t.Fatalf("default: %q %v", c.BaseCurrency, err)
	}
	m := base()
	m["BASE_CURRENCY"] = " usd "
	if c, err = load(env(m)); err != nil || c.BaseCurrency != "USD" {
		t.Fatalf("normalised: %q %v", c.BaseCurrency, err)
	}
	for _, bad := range []string{"US", "USDT", "1"} {
		m["BASE_CURRENCY"] = bad
		if _, err := load(env(m)); err == nil {
			t.Errorf("BASE_CURRENCY=%q: want error", bad)
		}
	}
}
