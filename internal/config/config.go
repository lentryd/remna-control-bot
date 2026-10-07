// Package config reads the bot's settings from the environment.
package config

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
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
	BotToken     string
	Mode         Mode
	VpnctlURL    string
	VpnctlToken  string
	PublicURL    string
	WebAppPath   string // "" or "/x[/y]", no trailing slash
	HTTPAddr     string
	DBPath       string
	LogLevel     string
	RefSecret    string
	BaseCurrency string // fallback while the panel can't tell its base currency
	// BaseCurrencySet: BASE_CURRENCY was given explicitly, i.e. the operator vouches for the currency.
	BaseCurrencySet bool

	PayMode         PayMode
	NotifyInterval  time.Duration // 0 disables notifications
	TrafficAlertPct int           // traffic share that triggers an alert
	RemindDays      []int         // days before the end to remind about payment, descending

	AdminChatID      int64         // where receipts go; 0 disables manual payment
	AdminIDs         []int64       // who may decide on them
	ManualPayDetails string        // where to send money, shown with the amount
	OrderTTL         time.Duration // unpaid orders expire after this
	Pay              PayConfig
}

// PayConfig holds the online payment providers' credentials. A provider is
// on when its required keys are set.
type PayConfig struct {
	YooKassaShopID, YooKassaSecret string
	YooKassaReturnURL              string // where the customer lands after paying
	YooKassaReceiptEmail           string // fiscal receipt customer; no receipt when empty
	YooKassaVAT                    int    // 1..6

	CryptoBotToken   string
	CryptoBotTestnet bool
	CryptoBotAssets  string // accepted assets, e.g. "USDT,TON"; empty: all

	HeleketMerchantID, HeleketAPIKey  string
	HeleketToCurrency, HeleketNetwork string // optional fixed asset / network

	StarsRate float64 // base-currency units per Telegram Star; > 0 enables Stars
}

func (p PayConfig) YooKassaOn() bool  { return p.YooKassaShopID != "" && p.YooKassaSecret != "" }
func (p PayConfig) CryptoBotOn() bool { return p.CryptoBotToken != "" }
func (p PayConfig) HeleketOn() bool   { return p.HeleketMerchantID != "" && p.HeleketAPIKey != "" }
func (p PayConfig) StarsOn() bool     { return p.StarsRate > 0 }

// WebAppURL is where Telegram opens the WebApp.
func (c Config) WebAppURL() string { return c.PublicURL + c.WebAppPath }

// ManualEnabled: receipts need somewhere to go, someone to decide and
// details to pay by.
func (c Config) ManualEnabled() bool {
	return c.AdminChatID != 0 && len(c.AdminIDs) > 0 && c.ManualPayDetails != ""
}

func (c Config) IsAdmin(id int64) bool { return slices.Contains(c.AdminIDs, id) }

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

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
	c.BaseCurrency = strings.ToUpper(or("BASE_CURRENCY", "RUB"))
	c.BaseCurrencySet = strings.TrimSpace(get("BASE_CURRENCY")) != ""
	if !currencyRe.MatchString(c.BaseCurrency) {
		return Config{}, fmt.Errorf("config: BASE_CURRENCY must be a 3-letter ISO 4217 code like RUB, got %q", c.BaseCurrency)
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
	if v := or("ADMIN_CHAT_ID", ""); v != "" {
		if c.AdminChatID, err = strconv.ParseInt(v, 10, 64); err != nil {
			return Config{}, fmt.Errorf("config: ADMIN_CHAT_ID must be a chat id")
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
	c.ManualPayDetails = strings.ReplaceAll(or("MANUAL_PAY_DETAILS", ""), `\n`, "\n")
	if c.OrderTTL, err = time.ParseDuration(or("ORDER_TTL", "48h")); err != nil || c.OrderTTL <= 0 {
		return Config{}, fmt.Errorf("config: ORDER_TTL must be a positive duration like 48h")
	}
	c.Pay = PayConfig{
		YooKassaShopID:       or("YOOKASSA_SHOP_ID", ""),
		YooKassaSecret:       or("YOOKASSA_SECRET_KEY", ""),
		YooKassaReturnURL:    or("YOOKASSA_RETURN_URL", c.WebAppURL()+"/"),
		YooKassaReceiptEmail: or("YOOKASSA_RECEIPT_EMAIL", ""),
		CryptoBotToken:       or("CRYPTOBOT_TOKEN", ""),
		CryptoBotAssets:      or("CRYPTOBOT_ASSETS", ""),
		HeleketMerchantID:    or("HELEKET_MERCHANT_ID", ""),
		HeleketAPIKey:        or("HELEKET_API_KEY", ""),
		HeleketToCurrency:    or("HELEKET_TO_CURRENCY", ""),
		HeleketNetwork:       or("HELEKET_NETWORK", ""),
	}
	if (c.Pay.YooKassaShopID == "") != (c.Pay.YooKassaSecret == "") {
		return Config{}, fmt.Errorf("config: YOOKASSA_SHOP_ID and YOOKASSA_SECRET_KEY go together")
	}
	if (c.Pay.HeleketMerchantID == "") != (c.Pay.HeleketAPIKey == "") {
		return Config{}, fmt.Errorf("config: HELEKET_MERCHANT_ID and HELEKET_API_KEY go together")
	}
	if c.Pay.YooKassaVAT, err = strconv.Atoi(or("YOOKASSA_VAT_CODE", "1")); err != nil || c.Pay.YooKassaVAT < 1 || c.Pay.YooKassaVAT > 6 {
		return Config{}, fmt.Errorf("config: YOOKASSA_VAT_CODE must be 1..6")
	}
	if c.Pay.CryptoBotTestnet, err = strconv.ParseBool(or("CRYPTOBOT_TESTNET", "false")); err != nil {
		return Config{}, fmt.Errorf("config: CRYPTOBOT_TESTNET must be true or false")
	}
	if c.Pay.StarsRate, err = strconv.ParseFloat(or("STARS_RATE", "0"), 64); err != nil || c.Pay.StarsRate < 0 || math.IsNaN(c.Pay.StarsRate) || math.IsInf(c.Pay.StarsRate, 0) {
		return Config{}, fmt.Errorf("config: STARS_RATE must be a number ≥ 0 (base currency per Telegram Star)")
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
