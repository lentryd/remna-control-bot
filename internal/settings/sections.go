// Package settings keeps the operational settings admins edit in the web app:
// typed sections stored as JSON in SQLite, secrets encrypted, published as
// immutable snapshots so every reader sees a consistent configuration.
package settings

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"vpn-control-bot/internal/config"
)

const (
	KeyGeneral   = "general"
	KeyManual    = "pay.manual"
	KeyYooKassa  = "pay.yookassa"
	KeyCryptoBot = "pay.cryptobot"
	KeyHeleket   = "pay.heleket"
	KeyPal24     = "pay.pal24"
	KeyPlatega   = "pay.platega"
	KeyStars     = "pay.stars"
)

var Keys = []string{KeyGeneral, KeyManual, KeyYooKassa, KeyCryptoBot, KeyHeleket, KeyPal24, KeyPlatega, KeyStars}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// Section is one independently saved group of settings. Secrets returns
// pointers to the fields that are encrypted at rest and masked in the API.
type Section interface {
	Key() string
	Validate() error
	Secrets() []*string
}

func required(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.TrimSpace(pairs[i+1]) == "" {
			return fmt.Errorf("%s is required", pairs[i])
		}
	}
	return nil
}

// General: how the bot behaves, independent of payment methods.
type General struct {
	Mode            config.Mode    `json:"mode"`
	BaseCurrency    string         `json:"base_currency"` // "" takes the panel's
	PayMode         config.PayMode `json:"pay_mode"`
	NotifyMinutes   int            `json:"notify_minutes"` // 0 turns notifications off
	TrafficAlertPct int            `json:"traffic_alert_pct"`
	RemindDays      []int          `json:"remind_days"`
	OrderTTLHours   int            `json:"order_ttl_hours"`
}

func (General) Key() string                  { return KeyGeneral }
func (*General) Secrets() []*string          { return nil }
func (g General) NotifyEvery() time.Duration { return time.Duration(g.NotifyMinutes) * time.Minute }
func (g General) OrderTTL() time.Duration    { return time.Duration(g.OrderTTLHours) * time.Hour }

// RemindDescending is what notify.Rules wants: the largest lead time first.
func (g General) RemindDescending() []int {
	d := slices.Clone(g.RemindDays)
	sort.Sort(sort.Reverse(sort.IntSlice(d)))
	return d
}

func (g General) Validate() error {
	switch {
	case g.Mode != config.ModeOpen && g.Mode != config.ModeClosed:
		return errors.New("mode must be open or closed")
	case g.BaseCurrency != "" && !currencyRe.MatchString(g.BaseCurrency):
		return errors.New("base_currency must be a 3-letter upper-case ISO 4217 code like RUB, or empty")
	case g.PayMode != config.PayMixed && g.PayMode != config.PayReferralFull:
		return errors.New("pay_mode must be mixed or referral_full")
	case g.NotifyMinutes < 0 || g.NotifyMinutes > 7*24*60:
		return errors.New("notify_minutes must be 0..10080")
	case g.TrafficAlertPct < 1 || g.TrafficAlertPct > 100:
		return errors.New("traffic_alert_pct must be 1..100")
	case g.OrderTTLHours < 1 || g.OrderTTLHours > 720:
		return errors.New("order_ttl_hours must be 1..720")
	case len(g.RemindDays) == 0:
		return errors.New("remind_days needs at least one day count like 3,1")
	}
	for _, d := range g.RemindDays {
		if d < 1 || d > 365 {
			return errors.New("remind_days must be positive day counts like 3,1")
		}
	}
	return nil
}

// Manual is payment by transfer with a receipt an admin checks.
type Manual struct {
	Enabled bool   `json:"enabled"`
	Details string `json:"details"` // where to send money, shown with the amount
}

func (Manual) Key() string         { return KeyManual }
func (*Manual) Secrets() []*string { return nil }
func (m Manual) Active() bool      { return m.Enabled && m.Validate() == nil }
func (m Manual) Validate() error {
	if m.Enabled {
		return required("details", m.Details)
	}
	return nil
}

type YooKassa struct {
	Enabled      bool   `json:"enabled"`
	ShopID       string `json:"shop_id"`
	SecretKey    string `json:"secret_key"`
	ReturnURL    string `json:"return_url"`    // "" = the WebApp
	ReceiptEmail string `json:"receipt_email"` // fiscal receipt customer; none when empty
	VATCode      int    `json:"vat_code"`      // 1..6
}

func (YooKassa) Key() string           { return KeyYooKassa }
func (y *YooKassa) Secrets() []*string { return []*string{&y.SecretKey} }
func (y YooKassa) Active() bool        { return y.Enabled && y.Validate() == nil }
func (y YooKassa) Complete() bool      { y.Enabled = true; return y.Validate() == nil }
func (y YooKassa) Validate() error {
	if y.VATCode < 1 || y.VATCode > 6 {
		return errors.New("vat_code must be 1..6")
	}
	if y.Enabled {
		return required("shop_id", y.ShopID, "secret_key", y.SecretKey)
	}
	return nil
}

type CryptoBot struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	Testnet bool   `json:"testnet"`
	Assets  string `json:"assets"` // accepted assets, e.g. "USDT,TON"; empty: all
}

func (CryptoBot) Key() string           { return KeyCryptoBot }
func (c *CryptoBot) Secrets() []*string { return []*string{&c.Token} }
func (c CryptoBot) Active() bool        { return c.Enabled && c.Validate() == nil }
func (c CryptoBot) Complete() bool      { c.Enabled = true; return c.Validate() == nil }
func (c CryptoBot) Validate() error {
	if c.Enabled {
		return required("token", c.Token)
	}
	return nil
}

type Heleket struct {
	Enabled    bool   `json:"enabled"`
	MerchantID string `json:"merchant_id"`
	APIKey     string `json:"api_key"`
	ToCurrency string `json:"to_currency"` // optional fixed asset
	Network    string `json:"network"`     // optional fixed network
}

func (Heleket) Key() string           { return KeyHeleket }
func (h *Heleket) Secrets() []*string { return []*string{&h.APIKey} }
func (h Heleket) Active() bool        { return h.Enabled && h.Validate() == nil }
func (h Heleket) Complete() bool      { h.Enabled = true; return h.Validate() == nil }
func (h Heleket) Validate() error {
	if h.Enabled {
		return required("merchant_id", h.MerchantID, "api_key", h.APIKey)
	}
	return nil
}

// Pal24 is PayPalych.
type Pal24 struct {
	Enabled        bool   `json:"enabled"`
	APIToken       string `json:"api_token"`
	ShopID         string `json:"shop_id"`
	SignatureToken string `json:"signature_token"` // postback signing token; empty: the API token
}

func (Pal24) Key() string           { return KeyPal24 }
func (p *Pal24) Secrets() []*string { return []*string{&p.APIToken, &p.SignatureToken} }
func (p Pal24) Active() bool        { return p.Enabled && p.Validate() == nil }
func (p Pal24) Complete() bool      { p.Enabled = true; return p.Validate() == nil }
func (p Pal24) Validate() error {
	if p.Enabled {
		return required("api_token", p.APIToken, "shop_id", p.ShopID)
	}
	return nil
}

type Platega struct {
	Enabled       bool   `json:"enabled"`
	MerchantID    string `json:"merchant_id"`
	Secret        string `json:"secret"`
	PaymentMethod int    `json:"payment_method"` // 2 SBP QR, 11 cards, 12 international cards, 13 crypto
	APIVersion    string `json:"api_version"`    // v1 | v2
	ReturnURL     string `json:"return_url"`     // "" = the WebApp
	FailedURL     string `json:"failed_url"`
}

func (Platega) Key() string           { return KeyPlatega }
func (p *Platega) Secrets() []*string { return []*string{&p.Secret} }
func (p Platega) Active() bool        { return p.Enabled && p.Validate() == nil }
func (p Platega) Complete() bool      { p.Enabled = true; return p.Validate() == nil }
func (p Platega) Validate() error {
	if p.PaymentMethod < 1 {
		return errors.New("payment_method must be a positive integer (2 SBP QR, 11 cards, 12 international cards, 13 crypto)")
	}
	if p.APIVersion != "v1" && p.APIVersion != "v2" {
		return errors.New("api_version must be v1 or v2")
	}
	if p.Enabled {
		return required("merchant_id", p.MerchantID, "secret", p.Secret)
	}
	return nil
}

type Stars struct {
	Enabled bool    `json:"enabled"`
	Rate    float64 `json:"rate"` // base-currency units per Telegram Star
}

func (Stars) Key() string         { return KeyStars }
func (*Stars) Secrets() []*string { return nil }
func (s Stars) Active() bool      { return s.Enabled && s.Validate() == nil }
func (s Stars) Complete() bool    { s.Enabled = true; return s.Validate() == nil }
func (s Stars) Validate() error {
	if s.Enabled && (s.Rate <= 0 || math.IsNaN(s.Rate) || math.IsInf(s.Rate, 0)) {
		return errors.New("rate must be a number > 0 (base currency per Telegram Star)")
	}
	return nil
}

// newSection returns the section for key filled with its defaults (nil for an unknown key).
func newSection(key string) Section {
	switch key {
	case KeyGeneral:
		return &General{Mode: config.ModeClosed, PayMode: config.PayMixed, NotifyMinutes: 60, TrafficAlertPct: 80,
			RemindDays: []int{3, 1}, OrderTTLHours: 48}
	case KeyManual:
		return &Manual{}
	case KeyYooKassa:
		return &YooKassa{VATCode: 1}
	case KeyCryptoBot:
		return &CryptoBot{}
	case KeyHeleket:
		return &Heleket{}
	case KeyPal24:
		return &Pal24{}
	case KeyPlatega:
		return &Platega{PaymentMethod: 2, APIVersion: "v1"}
	case KeyStars:
		return &Stars{}
	}
	return nil
}

// Snapshot is one consistent, immutable view of every section. Replace
// sections with With; never mutate a published one.
type Snapshot struct{ m map[string]Section }

func Defaults() Snapshot {
	m := make(map[string]Section, len(Keys))
	for _, k := range Keys {
		m[k] = newSection(k)
	}
	return Snapshot{m: m}
}

// With returns a copy of the snapshot with sec in place of the section of the same key.
func (s Snapshot) With(sec Section) Snapshot {
	m := make(map[string]Section, len(s.m))
	for k, v := range s.m {
		m[k] = v
	}
	m[sec.Key()] = sec
	return Snapshot{m: m}
}

func (s Snapshot) section(key string) Section { return s.m[key] }

func (s Snapshot) General() General   { return *s.m[KeyGeneral].(*General) }
func (s Snapshot) Manual() Manual     { return *s.m[KeyManual].(*Manual) }
func (s Snapshot) YooKassa() YooKassa { return *s.m[KeyYooKassa].(*YooKassa) }
func (s Snapshot) CryptoBot() CryptoBot {
	return *s.m[KeyCryptoBot].(*CryptoBot)
}
func (s Snapshot) Heleket() Heleket { return *s.m[KeyHeleket].(*Heleket) }
func (s Snapshot) Pal24() Pal24     { return *s.m[KeyPal24].(*Pal24) }
func (s Snapshot) Platega() Platega { return *s.m[KeyPlatega].(*Platega) }
func (s Snapshot) Stars() Stars     { return *s.m[KeyStars].(*Stars) }
