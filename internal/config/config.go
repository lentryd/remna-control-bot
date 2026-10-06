// Package config reads the bot's settings from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Mode string

const (
	ModeOpen   Mode = "open"
	ModeClosed Mode = "closed"
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
