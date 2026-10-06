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
