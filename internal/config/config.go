// Package config reads the bot's settings from the environment.
package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeOpen   Mode = "open"
	ModeClosed Mode = "closed"
)

// PayMode is how renewals may use the balance (BALANCE_PAY_MODE).
type PayMode string

const (
	PayMixed        PayMode = "mixed"         // own and referral money together
	PayReferralFull PayMode = "referral_full" // referral money only if it covers the whole price
)

type Config struct {
	BotToken    string
	Mode        Mode
	VpnctlURL   string
	VpnctlToken string
	PublicURL   string
	WebAppPath  string // "" or "/x[/y]", no trailing slash
	HTTPAddr    string
	DBPath      string
	LogLevel    string
	RefSecret   string

	PayMode         PayMode
	NotifyInterval  time.Duration // 0 disables notifications
	TrafficAlertPct int           // traffic share that triggers an alert
	RemindDays      []int         // days before the end to remind about payment, descending
}

// WebAppURL is where Telegram opens the WebApp.
func (c Config) WebAppURL() string { return c.PublicURL + c.WebAppPath }

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	or := func(k, def string) string {
		if v := strings.TrimSpace(get(k)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		BotToken:    or("BOT_TOKEN", ""),
		Mode:        Mode(or("BOT_MODE", string(ModeClosed))),
		VpnctlURL:   strings.TrimRight(or("VPNCTL_URL", ""), "/"),
		VpnctlToken: or("VPNCTL_TOKEN", ""),
		PublicURL:   strings.TrimRight(or("PUBLIC_URL", ""), "/"),
		WebAppPath:  normPath(or("WEBAPP_PATH", "/bot")),
		HTTPAddr:    or("HTTP_ADDR", ":8090"),
		DBPath:      or("DB_PATH", "/data/bot.db"),
		LogLevel:    or("LOG_LEVEL", "info"),
		RefSecret:   or("REF_SECRET", ""),
	}
	for k, v := range map[string]string{
		"BOT_TOKEN": c.BotToken, "VPNCTL_URL": c.VpnctlURL, "VPNCTL_TOKEN": c.VpnctlToken,
		"PUBLIC_URL": c.PublicURL, "REF_SECRET": c.RefSecret,
	} {
		if v == "" {
			return Config{}, fmt.Errorf("config: %s is required", k)
		}
	}
	c.PayMode = PayMode(or("BALANCE_PAY_MODE", string(PayMixed)))
	if c.PayMode != PayMixed && c.PayMode != PayReferralFull {
		return Config{}, fmt.Errorf("config: BALANCE_PAY_MODE must be mixed or referral_full, got %q", c.PayMode)
	}
	var err error
	if v := or("NOTIFY_INTERVAL", "1h"); v != "0" {
		if c.NotifyInterval, err = time.ParseDuration(v); err != nil || c.NotifyInterval < 0 {
			return Config{}, fmt.Errorf("config: NOTIFY_INTERVAL %q: want a duration like 1h or 0", v)
		}
	}
	if c.TrafficAlertPct, err = strconv.Atoi(or("TRAFFIC_ALERT_PCT", "80")); err != nil || c.TrafficAlertPct < 1 || c.TrafficAlertPct > 100 {
		return Config{}, fmt.Errorf("config: TRAFFIC_ALERT_PCT must be 1..100")
	}
	for _, f := range strings.Split(or("REMIND_DAYS", "3,1"), ",") {
		d, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || d < 1 {
			return Config{}, fmt.Errorf("config: REMIND_DAYS must be positive day counts like 3,1")
		}
		c.RemindDays = append(c.RemindDays, d)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(c.RemindDays)))
	if c.Mode != ModeOpen && c.Mode != ModeClosed {
		return Config{}, fmt.Errorf("config: BOT_MODE must be open or closed, got %q", c.Mode)
	}
	return c, nil
}

func normPath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}
