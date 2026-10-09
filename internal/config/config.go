// Package config reads the settings the bot needs before its database is
// open: where things are, the secrets that identify it, and who the admins
// are. Everything else is edited in the admin part of the WebApp.
package config

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeOpen   Mode = "open"
	ModeClosed Mode = "closed"
)

// PayMode is how renewals may use the balance.
type PayMode string

const (
	PayMixed        PayMode = "mixed"         // own and referral money together
	PayReferralFull PayMode = "referral_full" // referral money only if it covers the whole price
)

type Config struct {
	BotToken    string
	VpnctlURL   string
	VpnctlToken string
	PublicURL   string
	WebAppPath  string // "" or "/x[/y]", no trailing slash
	HTTPAddr    string
	DBPath      string
	LogLevel    string
	RefSecret   string // also the root of the key that encrypts stored secrets

	AdminIDs []int64 // who may open the admin part and receives order cards
}

// WebAppURL is where Telegram opens the WebApp.
func (c Config) WebAppURL() string { return c.PublicURL + c.WebAppPath }

func (c Config) IsAdmin(id int64) bool { return slices.Contains(c.AdminIDs, id) }

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
		VpnctlURL:   strings.TrimRight(or("VPNCTL_URL", ""), "/"),
		VpnctlToken: or("VPNCTL_TOKEN", ""),
		PublicURL:   strings.TrimRight(or("PUBLIC_URL", ""), "/"),
		WebAppPath:  normPath(or("WEBAPP_PATH", "/bot")),
		HTTPAddr:    or("HTTP_ADDR", ":8090"),
		DBPath:      or("DB_PATH", "/data/bot.db"),
		LogLevel:    or("LOG_LEVEL", "info"),
		RefSecret:   or("REF_SECRET", ""),
	}
	for _, r := range []struct{ k, v string }{
		{"BOT_TOKEN", c.BotToken}, {"VPNCTL_URL", c.VpnctlURL}, {"VPNCTL_TOKEN", c.VpnctlToken},
		{"PUBLIC_URL", c.PublicURL}, {"REF_SECRET", c.RefSecret},
	} {
		if r.v == "" {
			return Config{}, fmt.Errorf("config: %s is required", r.k)
		}
	}
	for _, f := range strings.Split(or("ADMIN_IDS", ""), ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("config: ADMIN_IDS must be Telegram ids like 5,7")
		}
		c.AdminIDs = append(c.AdminIDs, id)
	}
	if len(c.AdminIDs) == 0 {
		return Config{}, fmt.Errorf("config: ADMIN_IDS is required: admins configure the bot in the web app")
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
