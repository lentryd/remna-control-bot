# Admin web app and DB-backed settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move order decisions and all operational settings out of Telegram chats and env into an admin part of the Mini App; Telegram only DMs admins a card with an "open" button.

**Architecture:** A new `internal/settings` package keeps typed sections (JSON in SQLite, secrets AES-GCM-encrypted with a key derived from `REF_SECRET`) and publishes immutable snapshots; subscribers (`main.go`) rebuild the payment registry and `orders.Live` on every save. A new `/api/admin/*` Fiber group (initData + `ADMIN_IDS`) wraps `orders.Service` and the settings store; the SPA gets `/admin/*` routes. Notifications become one `Notifier.Card` call that DMs/edits a card per admin, tracked in an `order_notices` table.

**Tech Stack:** Go 1.26 (stdlib `crypto/hkdf`, `crypto/cipher`), Fiber v2, go-telegram/bot v1.23, modernc SQLite; React 19 + Mantine 9 + TanStack Query + react-router 7 (HashRouter), bun.

**Spec:** `docs/superpowers/specs/2026-10-09-admin-webapp-settings-design.md`

## Global Constraints

- Env after the change keeps only: `ADMIN_IDS`, `BOT_TOKEN`, `REMNACTL_URL`, `REMNACTL_TOKEN`, `PUBLIC_URL`, `WEBAPP_PATH`, `HTTP_ADDR`, `DB_PATH`, `REF_SECRET`, `LOG_LEVEL` (+ dev-only `LOCAL_DOMAIN`). `ADMIN_CHAT_ID` is removed (in Task 7; the bot keeps working with it until then).
- No backward compatibility: the old env variables are simply dropped; the dev database `data/bot.db` may be deleted (the `orders.admin_msg_id` column and `admin_prompts` table disappear from the schema).
- Secrets: AES-GCM, key = HKDF-SHA256(`REF_SECRET`); no new env variable. Rotating `REF_SECRET` makes stored secrets unreadable — they load as empty and are listed in `lost`.
- Admin routes live under `{WEBAPP_PATH}/api/admin/*`; admin = valid `initData` AND `telegramID ∈ ADMIN_IDS`; no panel-customer lookup on those routes.
- Notifications: private message to each id in `ADMIN_IDS`; button is always `web_app`.
- First start with an empty DB: every payment method (manual included) off, `mode=closed`, other values = the defaults currently in `config.go` (`mixed`, 60 min, 80 %, `3,1`, 48 h).
- Existing conventions: comments only where the "why" is not obvious; error strings lower-case; tests next to code; `go vet ./...` and `go test ./... -race` must pass after every task.
- **Commits carry no `Co-Authored-By`/Claude attribution (user rule).**

## Review Focus

Failure modes the spec implies that no single task's happy path would catch (each is pinned by a named test in the owning task):

1. A provider is switched off in the admin while customers hold unpaid invoices → its webhook/poll must still resolve (money arrives later) but it must not issue new invoices. (Task 2 `TestRetiredProviderStillResolves`, Task 3 `TestLiveSwap`.)
2. `REF_SECRET` rotated → the bot still starts, affected sections load empty and inactive, `lost` reports them, re-saving clears it. (Task 1 `TestLostSecrets`.)
3. The admin UI echoes a masked secret back (or sends an empty one) → the stored secret survives. (Task 1 `TestSecretKeptOnMask`, Task 5 `TestAdminSettings`.)
4. An admin never pressed `/start` (Telegram answers 403) → order creation/approval is unaffected, the failure is logged and surfaced in the Orders screen. (Task 7 `TestCardFanOut`.)
5. Two admins decide the same order, a non-admin opens the deep link, an admin who is not a panel customer opens the app, or `initData` is expired. (Task 5 `TestAdminAuth`, `TestAdminApproveTwice`.)

---

## File Structure

Create:
- `internal/settings/box.go` — AES-GCM seal/open with HKDF key.
- `internal/settings/sections.go` — section types, defaults, validation, `Snapshot`.
- `internal/settings/store.go` — SQLite persistence, `Put`/`View`/`OnChange`/`Lost`.
- `internal/settings/settings_test.go`
- `internal/payments/build.go` (+ `build_test.go`) — registry from a snapshot; `Checker` lives in `payments.go`.
- `internal/orders/live.go` (+ `live_test.go`) — hot-swappable runtime config.
- `internal/orders/admin_test.go` — `ListAll` test.
- `internal/telegram/receipt.go` — receipt streaming.
- `internal/telegram/adminnotify.go` (+ `adminnotify_test.go`) — DM cards, alerts, `/admin`.
- `internal/webapp/admin.go` (+ `admin_test.go`) — admin API.
- `web/src/adminApi.ts`, `web/src/AdminApp.tsx`, `web/src/pages/admin/{AdminOrders,AdminOrder,AdminMethods,AdminGeneral,SectionForm,Receipt}.tsx`, `web/src/pages/admin/sections.ts`, `web/src/pages/admin/text.ts`.

Modify: `internal/config/config.go` (+test), `internal/payments/{payments,cryptobot,yookassa}.go`, `internal/orders/{service,provider,fulfill,repo,order}.go` (+tests), `internal/access/access.go` (+test), `internal/currency/source.go` (+test), `internal/notify/runner.go` (+test), `internal/store/store.go` (+test), `internal/webapp/{server,orders,pay}.go` (+tests), `internal/telegram/{bot,orders,private,admin,route}.go` and `view/{screen,texts,orders}.go` (+tests), `cmd/bot/main.go`, `web/src/{api,types,App}.ts(x)`, `web/src/pages/Overview.tsx`, `.env.example`, `README.md`.

Delete (Task 7): `internal/telegram/admin.go`, `internal/telegram/admin_test.go`.

---

### Task 1: `internal/settings` — sections, encryption, store

**Files:**
- Create: `internal/settings/box.go`, `internal/settings/sections.go`, `internal/settings/store.go`
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Produces (used by every later task):
  - `type Section interface { Key() string; Validate() error; Secrets() []*string }`
  - Section structs (JSON tags are the admin API contract): `General`, `Manual`, `YooKassa`, `CryptoBot`, `Heleket`, `Pal24`, `Platega`, `Stars`; pay sections have `Active() bool` (enabled and complete) and `Complete() bool` (credentials present regardless of `Enabled`).
  - `General` methods: `NotifyEvery() time.Duration`, `OrderTTL() time.Duration`, `RemindDescending() []int`.
  - Keys: `KeyGeneral="general"`, `KeyManual="pay.manual"`, `KeyYooKassa="pay.yookassa"`, `KeyCryptoBot="pay.cryptobot"`, `KeyHeleket="pay.heleket"`, `KeyPal24="pay.pal24"`, `KeyPlatega="pay.platega"`, `KeyStars="pay.stars"`; `var Keys []string`.
  - `type Snapshot` (immutable; value type): accessors `General() General`, `Manual() Manual`, `YooKassa() YooKassa`, `CryptoBot() CryptoBot`, `Heleket() Heleket`, `Pal24() Pal24`, `Platega() Platega`, `Stars() Stars`; `With(Section) Snapshot`; `func Defaults() Snapshot`.
  - `func Open(ctx context.Context, db *sql.DB, secret string) (*Store, error)`; `(*Store).Snapshot() Snapshot`; `Put(ctx, key string, raw []byte) error` (errors: `ErrUnknownSection`, `*ValidationError`); `View(key string) (Section, bool)` (secrets masked); `OnChange(fn func(Snapshot))`; `Lost() []string`; `func Mask(s string) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/settings/settings_test.go`:

```go
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st.DB()
}

func openStore(t *testing.T, db *sql.DB, secret string) *Store {
	t.Helper()
	s, err := Open(context.Background(), db, secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const yooBody = `{"enabled":true,"shop_id":"123","secret_key":"live_supersecretvalue","vat_code":1}`

func TestDefaults(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	g := s.Snapshot().General()
	if g.Mode != config.ModeClosed || g.PayMode != config.PayMixed || g.NotifyMinutes != 60 || g.TrafficAlertPct != 80 || g.OrderTTLHours != 48 {
		t.Fatalf("defaults: %+v", g)
	}
	if !slices.Equal(g.RemindDescending(), []int{3, 1}) {
		t.Fatalf("remind: %v", g.RemindDescending())
	}
	if s.Snapshot().Manual().Active() || s.Snapshot().YooKassa().Active() || s.Snapshot().Stars().Active() {
		t.Fatal("everything must start off")
	}
}

func TestPutRoundTripEncrypts(t *testing.T) {
	db := openDB(t)
	s := openStore(t, db, "ref-secret")
	if err := s.Put(context.Background(), KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, KeyYooKassa).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "supersecret") {
		t.Fatalf("secret stored in clear: %s", raw)
	}
	again := openStore(t, db, "ref-secret")
	y := again.Snapshot().YooKassa()
	if !y.Active() || y.SecretKey != "live_supersecretvalue" || y.ShopID != "123" {
		t.Fatalf("reloaded: %+v", y)
	}
	v, _ := again.View(KeyYooKassa)
	if got := v.(*YooKassa).SecretKey; got != "••••alue" {
		t.Fatalf("view must mask, got %q", got)
	}
}

func TestSecretKeptOnMask(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	ctx := context.Background()
	if err := s.Put(ctx, KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	for _, echoed := range []string{"••••alue", ""} {
		body, _ := json.Marshal(map[string]any{"enabled": true, "shop_id": "999", "secret_key": echoed, "vat_code": 2})
		if err := s.Put(ctx, KeyYooKassa, body); err != nil {
			t.Fatalf("%q: %v", echoed, err)
		}
		y := s.Snapshot().YooKassa()
		if y.SecretKey != "live_supersecretvalue" || y.ShopID != "999" || y.VATCode != 2 {
			t.Fatalf("%q: %+v", echoed, y)
		}
	}
	body := `{"enabled":true,"shop_id":"999","secret_key":"brand-new-key","vat_code":2}`
	if err := s.Put(ctx, KeyYooKassa, []byte(body)); err != nil || s.Snapshot().YooKassa().SecretKey != "brand-new-key" {
		t.Fatalf("replace: %v %+v", err, s.Snapshot().YooKassa())
	}
}

func TestValidation(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	ctx := context.Background()
	var ve *ValidationError
	for name, c := range map[string]struct{ key, body string }{
		"enabled without keys": {KeyYooKassa, `{"enabled":true,"vat_code":1}`},
		"unknown field":        {KeyCryptoBot, `{"enabled":false,"nope":1}`},
		"not json":             {KeyCryptoBot, `{`},
		"bad mode":             {KeyGeneral, `{"mode":"wide","pay_mode":"mixed","notify_minutes":60,"traffic_alert_pct":80,"remind_days":[3],"order_ttl_hours":48}`},
		"bad currency":         {KeyGeneral, `{"mode":"open","base_currency":"rub","pay_mode":"mixed","notify_minutes":60,"traffic_alert_pct":80,"remind_days":[3],"order_ttl_hours":48}`},
		"stars rate":           {KeyStars, `{"enabled":true,"rate":0}`},
		"manual without text":  {KeyManual, `{"enabled":true,"details":"  "}`},
		"platega version":      {KeyPlatega, `{"enabled":false,"payment_method":2,"api_version":"v9"}`},
	} {
		if err := s.Put(ctx, c.key, []byte(c.body)); !errors.As(err, &ve) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
	if err := s.Put(ctx, "pay.nope", []byte(`{}`)); !errors.Is(err, ErrUnknownSection) {
		t.Fatalf("unknown section: %v", err)
	}
	if s.Snapshot().YooKassa().Enabled {
		t.Fatal("a rejected save must not change the snapshot")
	}
	// A disabled provider may be saved half-filled.
	if err := s.Put(ctx, KeyHeleket, []byte(`{"enabled":false,"merchant_id":"m"}`)); err != nil {
		t.Fatalf("draft: %v", err)
	}
	if s.Snapshot().Heleket().Complete() {
		t.Fatal("half-filled provider is not complete")
	}
}

func TestLostSecrets(t *testing.T) {
	db := openDB(t)
	s := openStore(t, db, "old")
	if err := s.Put(context.Background(), KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	moved := openStore(t, db, "new") // REF_SECRET changed
	if !slices.Equal(moved.Lost(), []string{KeyYooKassa}) {
		t.Fatalf("lost: %v", moved.Lost())
	}
	y := moved.Snapshot().YooKassa()
	if y.SecretKey != "" || y.Active() {
		t.Fatalf("lost secret must load empty and inactive: %+v", y)
	}
	body := `{"enabled":true,"shop_id":"123","secret_key":"fresh","vat_code":1}`
	if err := moved.Put(context.Background(), KeyYooKassa, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if len(moved.Lost()) != 0 || !moved.Snapshot().YooKassa().Active() {
		t.Fatal("re-entering the secret must clear the warning")
	}
}

func TestOnChange(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	var got []Snapshot
	s.OnChange(func(sn Snapshot) { got = append(got, sn) })
	if err := s.Put(context.Background(), KeyManual, []byte(`{"enabled":true,"details":"Card 1"}`)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Manual().Active() || got[0].Manual().Details != "Card 1" {
		t.Fatalf("subscriber: %+v", got)
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "••••", "12345678": "••••", "123456789": "••••6789"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/settings/ 2>&1 | head`
Expected: FAIL — package has no non-test Go files / undefined `Store`, `Open`, ….

- [ ] **Step 3: Write `box.go`**

```go
package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const sealedPrefix = "v1:"

var errCannotOpen = errors.New("settings: cannot decrypt a stored secret")

// box encrypts stored secrets with a key derived from REF_SECRET, so no extra
// env variable is needed.
type box struct{ aead cipher.AEAD }

func newBox(secret string) (*box, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), []byte("remna-control-bot/settings"), "secrets-v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &box{aead: aead}, nil
}

func (b *box) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (b *box) open(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	raw, ok := strings.CutPrefix(s, sealedPrefix)
	if !ok {
		return "", errCannotOpen
	}
	ct, err := base64.RawStdEncoding.DecodeString(raw)
	n := b.aead.NonceSize()
	if err != nil || len(ct) < n {
		return "", errCannotOpen
	}
	plain, err := b.aead.Open(nil, ct[:n], ct[n:], nil)
	if err != nil {
		return "", errCannotOpen
	}
	return string(plain), nil
}
```

- [ ] **Step 4: Write `sections.go`**

```go
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

	"remna-control-bot/internal/config"
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

func (General) Key() string          { return KeyGeneral }
func (*General) Secrets() []*string  { return nil }
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

func (YooKassa) Key() string         { return KeyYooKassa }
func (y *YooKassa) Secrets() []*string { return []*string{&y.SecretKey} }
func (y YooKassa) Active() bool      { return y.Enabled && y.Validate() == nil }
func (y YooKassa) Complete() bool    { y.Enabled = true; return y.Validate() == nil }
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

func (CryptoBot) Key() string          { return KeyCryptoBot }
func (c *CryptoBot) Secrets() []*string { return []*string{&c.Token} }
func (c CryptoBot) Active() bool       { return c.Enabled && c.Validate() == nil }
func (c CryptoBot) Complete() bool     { c.Enabled = true; return c.Validate() == nil }
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

func (Heleket) Key() string          { return KeyHeleket }
func (h *Heleket) Secrets() []*string { return []*string{&h.APIKey} }
func (h Heleket) Active() bool       { return h.Enabled && h.Validate() == nil }
func (h Heleket) Complete() bool     { h.Enabled = true; return h.Validate() == nil }
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

func (Pal24) Key() string          { return KeyPal24 }
func (p *Pal24) Secrets() []*string { return []*string{&p.APIToken, &p.SignatureToken} }
func (p Pal24) Active() bool       { return p.Enabled && p.Validate() == nil }
func (p Pal24) Complete() bool     { p.Enabled = true; return p.Validate() == nil }
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

func (Platega) Key() string          { return KeyPlatega }
func (p *Platega) Secrets() []*string { return []*string{&p.Secret} }
func (p Platega) Active() bool       { return p.Enabled && p.Validate() == nil }
func (p Platega) Complete() bool     { p.Enabled = true; return p.Validate() == nil }
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
```

- [ ] **Step 5: Write `store.go`**

```go
package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrUnknownSection = errors.New("settings: unknown section")

// ValidationError is a rejected save: the admin's input, not a server fault.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

const maskDots = "••••"

// Mask hides a secret for display, keeping its last four characters when it is long enough to stay safe.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return maskDots
	}
	return maskDots + string(r[len(r)-4:])
}

type Store struct {
	db   *sql.DB
	box  *box
	mu   sync.Mutex // serializes Put and guards subs/lost
	snap atomic.Pointer[Snapshot]
	subs []func(Snapshot)
	lost []string
}

// Open creates the table if needed and loads every stored section. A secret
// that can no longer be decrypted (REF_SECRET changed) loads empty and the
// section is reported by Lost instead of failing the start.
func Open(ctx context.Context, db *sql.DB, secret string) (*Store, error) {
	b, err := newBox(secret)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL)`); err != nil {
		return nil, err
	}
	s := &Store{db: db, box: b}
	snap := Defaults()
	for _, key := range Keys {
		var raw string
		err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sec := newSection(key)
		if err := json.Unmarshal([]byte(raw), sec); err != nil {
			return nil, fmt.Errorf("settings: stored %s is unreadable: %w", key, err)
		}
		lost := false
		for _, p := range sec.Secrets() {
			plain, err := b.open(*p)
			if err != nil {
				lost = true
			}
			*p = plain
		}
		if lost {
			s.lost = append(s.lost, key)
		}
		snap = snap.With(sec)
	}
	s.snap.Store(&snap)
	return s, nil
}

func (s *Store) Snapshot() Snapshot { return *s.snap.Load() }

// Lost lists the sections whose secrets could not be decrypted at start and were not re-entered since.
func (s *Store) Lost() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lost)
}

// OnChange registers fn to be called, in order, after every successful Put. Register before serving requests.
func (s *Store) OnChange(fn func(Snapshot)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

// View is the section as the admin API shows it: secrets masked.
func (s *Store) View(key string) (Section, bool) {
	sec := s.Snapshot().section(key)
	if sec == nil {
		return nil, false
	}
	b, _ := json.Marshal(sec)
	c := newSection(key)
	_ = json.Unmarshal(b, c)
	for _, p := range c.Secrets() {
		*p = Mask(*p)
	}
	return c, true
}

// Put replaces one section with the JSON body. Fields missing from the body
// take their defaults; a secret that is empty or still masked keeps the
// stored value. The snapshot changes only when the save succeeds.
func (s *Store) Put(ctx context.Context, key string, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Snapshot()
	old := cur.section(key)
	if old == nil {
		return ErrUnknownSection
	}
	next := newSection(key)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(next); err != nil {
		return &ValidationError{"invalid body: " + err.Error()}
	}
	oldSecrets := old.Secrets()
	for i, p := range next.Secrets() {
		if *p == "" || strings.HasPrefix(*p, maskDots) {
			*p = *oldSecrets[i]
		}
	}
	if err := next.Validate(); err != nil {
		return &ValidationError{err.Error()}
	}
	stored, err := s.seal(next)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, stored, time.Now()); err != nil {
		return err
	}
	snap := cur.With(next)
	s.snap.Store(&snap)
	s.lost = slices.DeleteFunc(s.lost, func(k string) bool { return k == key })
	for _, fn := range s.subs {
		fn(snap)
	}
	return nil
}

// seal marshals sec with its secrets encrypted, leaving sec itself in plain text.
func (s *Store) seal(sec Section) (string, error) {
	ptrs := sec.Secrets()
	plain := make([]string, len(ptrs))
	for i, p := range ptrs {
		plain[i] = *p
	}
	defer func() {
		for i, p := range ptrs {
			*p = plain[i]
		}
	}()
	for _, p := range ptrs {
		enc, err := s.box.seal(*p)
		if err != nil {
			return "", err
		}
		*p = enc
	}
	b, err := json.Marshal(sec)
	return string(b), err
}
```

- [ ] **Step 6: Run the tests**

Run: `gofmt -l internal/settings; go vet ./internal/settings/ && go test ./internal/settings/ -race -count=1`
Expected: no gofmt output; PASS. (If `gofmt -l` lists files, run `gofmt -w internal/settings` — the one-line method columns above are not gofmt-aligned.)

- [ ] **Step 7: Commit**

```bash
git add internal/settings
git commit -m "feat(settings): typed sections stored in SQLite with encrypted secrets"
```

---

### Task 2: `payments.Build`, retired providers, `Checker`

**Files:**
- Create: `internal/payments/build.go`, `internal/payments/build_test.go`
- Modify: `internal/payments/payments.go` (Registry, `Checker`), `internal/payments/cryptobot.go`, `internal/payments/yookassa.go`

**Interfaces:**
- Consumes: Task 1 `settings.Snapshot` and its section accessors.
- Produces:
  - `type BuildInput struct { Settings settings.Snapshot; PublicURL, WebAppURL string; StarsLink LinkFunc }`
  - `func Build(in BuildInput) *Registry` — enabled+complete providers are offered; complete but switched-off ones are *retired*.
  - `(*Registry).Offer(id string) (Provider, bool)` — only offered providers; `Get` also finds retired ones (webhooks, polling).
  - `(*Registry).Retire(ps ...Provider) *Registry`
  - `type Checker interface { Provider; Check(ctx context.Context) (string, error) }`; implemented by `*CryptoBot` and `*YooKassa`.

- [ ] **Step 1: Write the failing tests**

Create `internal/payments/build_test.go`:

```go
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
		&settings.YooKassa{Enabled: true, VATCode: 1},                      // enabled but no keys: invalid, never built
		&settings.CryptoBot{Enabled: false, Token: ""},                     // off and empty
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/payments/ -run 'Build|Retired|Checkers' 2>&1 | head`
Expected: FAIL — undefined `Build`, `Offer`, `Check`.

- [ ] **Step 3: Registry + Checker in `payments.go`**

Replace the `Registry` block (from `// Registry is the set of enabled providers…` through the `Get` method) with:

```go
// Registry is the set of enabled providers, in registration order, plus
// retired ones: switched off for new invoices but still resolvable, because
// customers may pay an invoice issued before the switch-off. All methods are
// safe on a nil Registry.
type Registry struct{ list, retired []Provider }

func NewRegistry(ps ...Provider) *Registry { return &Registry{list: ps} }

// Retire returns a registry that also resolves ps through Get (webhooks, polling) without offering them.
func (r *Registry) Retire(ps ...Provider) *Registry {
	c := Registry{retired: append(slices.Clone(r.retiredOrNil()), ps...)}
	if r != nil {
		c.list = r.list
	}
	return &c
}

func (r *Registry) retiredOrNil() []Provider {
	if r == nil {
		return nil
	}
	return r.retired
}

// Get finds a provider to receive a payment for: offered or retired.
func (r *Registry) Get(id string) (Provider, bool) {
	if p, ok := r.Offer(id); ok {
		return p, true
	}
	for _, p := range r.retiredOrNil() {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// Offer finds a provider that may issue new invoices.
func (r *Registry) Offer(id string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	for _, p := range r.list {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}
```

Add `"slices"` to the imports of `payments.go`. Under the `Poller` interface add:

```go
// Checker can verify its credentials with a harmless authenticated call; the
// message describes what the provider answered ("shop 42").
type Checker interface {
	Provider
	Check(ctx context.Context) (string, error)
}
```

- [ ] **Step 4: `Check` for CryptoBot and YooKassa**

Append to `cryptobot.go`:

```go
var _ Checker = (*CryptoBot)(nil)

// Check calls getMe, which needs a valid token and moves no money.
func (c *CryptoBot) Check(ctx context.Context) (string, error) {
	var r cbReply[struct {
		Name string `json:"name"`
	}]
	if err := c.call(ctx, http.MethodGet, "/getMe", nil, &r); err != nil {
		return "", err
	}
	if !r.OK {
		return "", fmt.Errorf("cryptobot: getMe refused the token")
	}
	return "app " + r.Result.Name, nil
}
```

Append to `yookassa.go`:

```go
var _ Checker = (*YooKassa)(nil)

// Check reads the shop's own account (GET /me), which needs valid credentials and moves no money.
func (y *YooKassa) Check(ctx context.Context) (string, error) {
	var r struct {
		AccountID string `json:"account_id"`
		Status    string `json:"status"`
	}
	if err := y.do(ctx, http.MethodGet, "/me", "", nil, &r); err != nil {
		return "", err
	}
	return "shop " + r.AccountID + " (" + r.Status + ")", nil
}
```

(Ensure `net/http` and `fmt` are imported in each file — both already are.)

- [ ] **Step 5: Write `build.go`**

```go
package payments

import (
	"remna-control-bot/internal/settings"
)

// BuildInput is everything Build needs besides the settings themselves.
type BuildInput struct {
	Settings  settings.Snapshot
	PublicURL string   // the origin providers call back to (webhooks at PublicURL/pay/<id>)
	WebAppURL string   // where customers land after paying unless a return URL is set
	StarsLink LinkFunc // creates Telegram invoice links; may be nil until the bot exists
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// Build turns a settings snapshot into a registry: providers that are on and
// complete are offered, those that are complete but switched off are retired
// (still resolvable for payments already in flight), the rest do not exist.
func Build(in BuildInput) *Registry {
	s := in.Settings
	back := in.WebAppURL + "/"
	var on, off []Provider
	add := func(enabled, complete bool, p Provider) {
		switch {
		case enabled && complete:
			on = append(on, p)
		case complete:
			off = append(off, p)
		}
	}
	if y := s.YooKassa(); true {
		add(y.Enabled, y.Complete(), &YooKassa{ShopID: y.ShopID, Secret: y.SecretKey, ReturnURL: orDefault(y.ReturnURL, back),
			ReceiptEmail: y.ReceiptEmail, VAT: y.VATCode})
	}
	if c := s.CryptoBot(); true {
		add(c.Enabled, c.Complete(), &CryptoBot{Token: c.Token, Testnet: c.Testnet, Assets: c.Assets})
	}
	if h := s.Heleket(); true {
		add(h.Enabled, h.Complete(), &Heleket{MerchantID: h.MerchantID, APIKey: h.APIKey, ToCurrency: h.ToCurrency,
			Network: h.Network, CallbackURL: in.PublicURL + "/pay/heleket"})
	}
	if p := s.Pal24(); true {
		add(p.Enabled, p.Complete(), &Pal24{APIToken: p.APIToken, ShopID: p.ShopID, SignatureToken: p.SignatureToken})
	}
	if p := s.Platega(); true {
		add(p.Enabled, p.Complete(), &Platega{MerchantID: p.MerchantID, Secret: p.Secret, PaymentMethod: p.PaymentMethod,
			APIVersion: p.APIVersion, ReturnURL: orDefault(p.ReturnURL, back), FailedURL: p.FailedURL})
	}
	if st := s.Stars(); true {
		add(st.Enabled, st.Complete(), &Stars{PerStar: st.Rate, Link: in.StarsLink})
	}
	return NewRegistry(on...).Retire(off...)
}
```

The `if x := …; true {` form is only to scope each variable; if that reads badly in review, replace each with a plain block `{ y := s.YooKassa(); add(...) }`.

- [ ] **Step 6: Run the tests**

Run: `gofmt -l internal/payments; go vet ./internal/payments/ && go test ./internal/payments/ -race -count=1`
Expected: PASS (existing provider tests included).

- [ ] **Step 7: Commit**

```bash
git add internal/payments
git commit -m "feat(payments): build the registry from settings; retire switched-off providers; credential checks"
```

---

### Task 3: Runtime configuration plumbing and the slim env

**Goal of the task:** everything that was read from env at start and is moving to settings is read through a live accessor; `main.go` builds it from the settings snapshot and rebuilds on change. After this task the bot still runs with `ADMIN_CHAT_ID` and the old Telegram admin buttons.

**Files:**
- Create: `internal/orders/live.go`, `internal/orders/live_test.go`
- Modify: `internal/orders/{service,provider}.go` and their tests; `internal/access/access.go` (+test); `internal/currency/source.go` (+test); `internal/notify/runner.go` (+test); `internal/config/config.go` (+test, rewritten); `internal/webapp/{server,orders,pay}.go` (+tests); `internal/telegram/{bot,orders}.go`; `cmd/bot/main.go`.

**Interfaces:**
- Consumes: Task 1 (`settings.Store`, `Snapshot`), Task 2 (`payments.Build`, `Registry.Offer`).
- Produces:
  - `orders.Live{PayMode config.PayMode; Manual bool; Details string; TTL time.Duration; Providers *payments.Registry}`; `(*Service).Live() Live`, `SetLive(Live)`, `UpdateLive(func(*Live))`. `Service` loses the fields `PayMode, Manual, Providers, Details, TTL`.
  - `access.New(api, codec, mode func() config.Mode, state)`.
  - `(*currency.Source).SetFallback(code string)`.
  - `notify.Conf{Interval time.Duration; Rules Rules}`; `Runner.Conf func() Conf` replaces `Runner.Interval`/`Runner.Rules`.
  - `webapp.Deps.PayMode` and `telegram.Deps.PayMode` become `func() config.PayMode`.
  - `config.Config` keeps: `BotToken, RemnactlURL, RemnactlToken, PublicURL, WebAppPath, HTTPAddr, DBPath, LogLevel, RefSecret, AdminChatID (until Task 7), AdminIDs`.

- [ ] **Step 1: Failing test for `orders.Live`**

Create `internal/orders/live_test.go`:

```go
package orders

import (
	"context"
	"sync"
	"testing"

	"remna-control-bot/internal/payments"
)

type stubProvider struct{ id string }

func (p stubProvider) ID() string    { return p.id }
func (p stubProvider) Title() string { return p.id }
func (p stubProvider) Invoice(context.Context, payments.InvoiceRequest) (payments.Invoice, error) {
	return payments.Invoice{ExternalID: "x", URL: "https://pay", Amount: 1, Currency: "RUB"}, nil
}

func TestLiveSwap(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	q, err := s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 100})
	if err != nil || len(q.Methods) != 1 || q.Methods[0] != MethodManual {
		t.Fatalf("manual only: %+v %v", q, err)
	}
	s.UpdateLive(func(l *Live) {
		l.Manual = false
		l.Providers = payments.NewRegistry(stubProvider{"yookassa"})
	})
	if q, _ = s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 100}); len(q.Methods) != 1 || q.Methods[0] != "yookassa" {
		t.Fatalf("after the swap only the provider is offered: %+v", q)
	}
	// The provider is switched off while an invoice is outstanding: it is not offered any more, yet resolves.
	s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry().Retire(stubProvider{"yookassa"}) })
	if _, err := s.Pay(ctx, c.ID, "nope", "yookassa"); err == nil {
		t.Fatal("Pay for a missing order must fail")
	}
	if _, ok := s.Live().Providers.Get("yookassa"); !ok {
		t.Fatal("the retired provider must still resolve for webhooks")
	}
	if _, ok := s.Live().Providers.Offer("yookassa"); ok {
		t.Fatal("the retired provider must not issue new invoices")
	}
}

func TestLiveConcurrent(t *testing.T) {
	s, _, _, _ := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.UpdateLive(func(l *Live) { l.Details = "x" }) }()
		go func() { defer wg.Done(); _ = s.Live().Details }()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Create `internal/orders/live.go`**

```go
package orders

import (
	"time"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/payments"
)

// Live is the part of the configuration admins change while the bot runs.
// Read it with Service.Live once per operation so one request sees one
// consistent set of values.
type Live struct {
	PayMode   config.PayMode
	Manual    bool   // payment by transfer with a receipt is offered
	Details   string // where to send money, shown with the amount
	TTL       time.Duration
	Providers *payments.Registry // may be nil: none
}

func (s *Service) Live() Live {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

func (s *Service) SetLive(l Live) {
	s.mu.Lock()
	s.live = l
	s.mu.Unlock()
}

func (s *Service) UpdateLive(fn func(*Live)) {
	s.mu.Lock()
	fn(&s.live)
	s.mu.Unlock()
}
```

- [ ] **Step 3: Edit `Service` and its readers**

In `internal/orders/service.go` replace the struct head:

```go
type Service struct {
	DB     *sql.DB
	Chats  ChatStates
	API    remnactl.Client
	Notify Notifier
	Now    func() time.Time
	Log    *slog.Logger
	NewID  func() string // order id source; nil: UUIDs (tests use a counter)
	// BaseCurrency is the currency of the panel's prices and balances (ISO
	// code); nil or empty means RUB. Invoices are issued in it.
	BaseCurrency func(context.Context) string
	// CurrencyKnown says whether BaseCurrency is confirmed (by the panel or by the operator) rather than a blind
	// default; provider invoices are refused while it is false. nil: always known.
	CurrencyKnown func() bool

	mu   sync.RWMutex
	live Live
}
```

(add `"sync"` to the imports and drop the now-unused `config` and `payments` imports if the compiler says so). Apply these edits:

- `Quote`: at the top of the function body add `lv := s.Live()`; replace `s.PayMode` → `lv.PayMode`; `s.Manual` → `lv.Manual`; `s.Providers.Enabled()` → `lv.Providers.Enabled()`.
- `RequestReceipt`: `s.Details` → `s.Live().Details`.
- `ExpireStale`: `s.TTL` → `s.Live().TTL` (read once into `ttl := s.Live().TTL`).
- `provider.go` `CreateWith`: `s.Providers.Get(string(method))` → `s.Live().Providers.Offer(string(method))`.
- `provider.go` `Pay`: `s.Providers.Get(provider)` → `s.Live().Providers.Offer(provider)`.
- `provider.go` `issue`: `ExpiresIn: s.TTL` → `ExpiresIn: s.Live().TTL`.
- `provider.go` `SweepProviders`: `s.TTL` (two places) → a local `ttl := s.Live().TTL` read at the top.
- `provider.go` `pollPaid`: `s.Providers.Get(...)` → `s.Live().Providers.Get(...)` (retired providers are polled on purpose).

Test updates (mechanical — apply, then `go vet ./internal/orders/` until clean):

| Old (tests) | New |
|---|---|
| `&Service{… PayMode: config.PayMixed, Manual: true, Details: "Карта 2200", TTL: 48 * time.Hour, …}` in `helpers_test.go` `setup` | build the `Service` without those fields, then `s.SetLive(Live{PayMode: config.PayMixed, Manual: true, Details: "Карта 2200", TTL: 48 * time.Hour})` before returning |
| `s.Manual = false` | `s.UpdateLive(func(l *Live) { l.Manual = false })` |
| `s.Providers = payments.NewRegistry(p…)` | `s.UpdateLive(func(l *Live) { l.Providers = payments.NewRegistry(p…) })` |
| `s.TTL = x` / `s.Details = x` | same pattern with `l.TTL` / `l.Details` |

Run: `go test ./internal/orders/ -race -count=1` → PASS (including the new `TestLiveSwap`, `TestLiveConcurrent`).

- [ ] **Step 4: Commit the orders change**

```bash
git add internal/orders
git commit -m "refactor(orders): read pay mode, manual details, TTL and providers through a swappable Live"
```

- [ ] **Step 5: `access` takes the mode as a function**

In `internal/access/access.go`: field `mode func() config.Mode`; `func New(api remnactl.Client, codec *refcode.Codec, mode func() config.Mode, state State) *Service`; the two comparisons `s.mode == config.ModeClosed` become `s.mode() == config.ModeClosed`. In `access_test.go` change `setup`:

```go
func setup(mode config.Mode) (*Service, *remnactl.Fake, memState) {
	f := remnactl.NewFake()
	st := memState{}
	return New(f, codec, func() config.Mode { return mode }, st), f, st
}
```

Run: `go test ./internal/access/ -race -count=1` → PASS.

- [ ] **Step 6: `currency.Source.SetFallback` (failing test first)**

Append to `internal/currency/source_test.go` (reuse the file's existing fake `Reader` — look at how its other tests build a `Source` with a failing panel and copy that construction):

```go
func TestSetFallback(t *testing.T) {
	s := &Source{API: failingReader{}, Fallback: "RUB"}
	if got := s.Get(context.Background()); got != "RUB" {
		t.Fatalf("fallback: %q", got)
	}
	s.SetFallback("USD")
	s.mu.Lock()
	s.next = time.Time{} // let Get look again
	s.mu.Unlock()
	if got := s.Get(context.Background()); got != "USD" {
		t.Fatalf("after SetFallback: %q", got)
	}
}

type failingReader struct{}

func (failingReader) BaseCurrency(context.Context) (string, error) { return "", errors.New("down") }
```

(If the file already defines a failing reader under another name, use it instead of `failingReader`.) Run: FAIL (`SetFallback` undefined). Then add to `source.go`:

```go
// SetFallback changes the currency served while the panel cannot say (the operator's explicit choice).
func (s *Source) SetFallback(code string) {
	s.mu.Lock()
	s.Fallback = code
	s.mu.Unlock()
}
```

Run: `go test ./internal/currency/ -race -count=1` → PASS.

- [ ] **Step 7: `notify.Runner` reads its configuration per pass (failing test first)**

Append to `internal/notify/runner_test.go`:

```go
func TestDue(t *testing.T) {
	now := time.Now()
	for name, c := range map[string]struct {
		last     time.Time
		interval time.Duration
		want     bool
	}{
		"off":            {time.Time{}, 0, false},
		"first pass":     {time.Time{}, time.Hour, true},
		"too early":      {now.Add(-30 * time.Minute), time.Hour, false},
		"interval gone":  {now.Add(-time.Hour), time.Hour, true},
		"turned off now": {now.Add(-time.Hour), 0, false},
	} {
		if got := due(c.last, now, c.interval); got != c.want {
			t.Errorf("%s: due = %v, want %v", name, got, c.want)
		}
	}
}
```

(add `"time"` to the test imports if missing). In `runner.go` replace the `Runner` struct and `Run`, and use `r.Conf().Rules` in `RunOnce`:

```go
// Conf is what the runner reads before every pass, so admins can change it without a restart.
type Conf struct {
	Interval time.Duration // 0: notifications are off
	Rules    Rules
}

// tick is how often Run looks at Conf; Interval decides whether a pass is due.
const tick = time.Minute

type Runner struct {
	API   remnactl.Client
	Store Sent
	Send  func(ctx context.Context, n Notice) error
	Conf  func() Conf
	Log   *slog.Logger
	Sweep func(ctx context.Context) error // housekeeping on every pass (expired orders); optional
}

// due: notifications are on and Interval has passed since the last pass.
func due(last, now time.Time, interval time.Duration) bool {
	return interval > 0 && (last.IsZero() || now.Sub(last) >= interval)
}

// Run checks every minute whether a pass is due, until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	var last time.Time
	for {
		if now := time.Now(); due(last, now, r.Conf().Interval) {
			last = now
			r.RunOnce(ctx, now)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```

and `for _, n := range Plan(customers, subs, tariffs, r.Conf().Rules, now) {`. In the three test constructions replace `Rules: rules` with `Conf: func() Conf { return Conf{Rules: rules} }`. Run: `go test ./internal/notify/ -race -count=1` → PASS.

- [ ] **Step 8: Slim `internal/config`**

Replace `internal/config/config.go` entirely:

```go
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
	RemnactlURL   string
	RemnactlToken string
	PublicURL   string
	WebAppPath  string // "" or "/x[/y]", no trailing slash
	HTTPAddr    string
	DBPath      string
	LogLevel    string
	RefSecret   string // also the root of the key that encrypts stored secrets

	AdminChatID int64   // removed together with the Telegram admin flow
	AdminIDs    []int64 // who may open the admin part and receives order cards
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
		RemnactlURL:   strings.TrimRight(or("REMNACTL_URL", ""), "/"),
		RemnactlToken: or("REMNACTL_TOKEN", ""),
		PublicURL:   strings.TrimRight(or("PUBLIC_URL", ""), "/"),
		WebAppPath:  normPath(or("WEBAPP_PATH", "/bot")),
		HTTPAddr:    or("HTTP_ADDR", ":8090"),
		DBPath:      or("DB_PATH", "/data/bot.db"),
		LogLevel:    or("LOG_LEVEL", "info"),
		RefSecret:   or("REF_SECRET", ""),
	}
	for _, r := range []struct{ k, v string }{
		{"BOT_TOKEN", c.BotToken}, {"REMNACTL_URL", c.RemnactlURL}, {"REMNACTL_TOKEN", c.RemnactlToken},
		{"PUBLIC_URL", c.PublicURL}, {"REF_SECRET", c.RefSecret},
	} {
		if r.v == "" {
			return Config{}, fmt.Errorf("config: %s is required", r.k)
		}
	}
	if v := or("ADMIN_CHAT_ID", ""); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("config: ADMIN_CHAT_ID must be a chat id")
		}
		c.AdminChatID = id
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
	return c, nil
}

func normPath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}
```

Replace `internal/config/config_test.go` entirely:

```go
package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{"BOT_TOKEN": "t", "REMNACTL_URL": "http://p/", "REMNACTL_TOKEN": "x", "PUBLIC_URL": "https://b.example/", "REF_SECRET": "s"}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.RemnactlURL != "http://p" || c.PublicURL != "https://b.example" || c.WebAppPath != "/bot" || c.HTTPAddr != ":8090" ||
		c.DBPath != "/data/bot.db" || c.LogLevel != "info" || len(c.AdminIDs) != 0 {
		t.Fatalf("defaults: %+v", c)
	}
	if c.WebAppURL() != "https://b.example/bot" {
		t.Fatalf("webapp url %q", c.WebAppURL())
	}
}

func TestLoadRequired(t *testing.T) {
	for _, k := range []string{"BOT_TOKEN", "REMNACTL_URL", "REMNACTL_TOKEN", "PUBLIC_URL", "REF_SECRET"} {
		m := base()
		delete(m, k)
		if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s: %v", k, err)
		}
	}
}

func TestWebAppPath(t *testing.T) {
	for in, want := range map[string]string{"/": "", "x": "/x", "/x/y/": "/x/y"} {
		m := base()
		m["WEBAPP_PATH"] = in
		c, err := load(env(m))
		if err != nil || c.WebAppPath != want {
			t.Errorf("%q → %q %v, want %q", in, c.WebAppPath, err, want)
		}
	}
}

func TestAdmins(t *testing.T) {
	m := base()
	m["ADMIN_IDS"] = " 5, 7 ,"
	m["ADMIN_CHAT_ID"] = "-100"
	c, err := load(env(m))
	if err != nil || len(c.AdminIDs) != 2 || !c.IsAdmin(7) || c.IsAdmin(9) || c.AdminChatID != -100 {
		t.Fatalf("%+v %v", c, err)
	}
	for _, k := range []string{"ADMIN_IDS", "ADMIN_CHAT_ID"} {
		m := base()
		m[k] = "x"
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s=x accepted", k)
		}
	}
}
```

Run: `go test ./internal/config/ -count=1` → PASS.

- [ ] **Step 9: `PayMode` as a function in `webapp` and `telegram`**

`webapp/server.go`: `Deps.PayMode func() config.PayMode`; add

```go
func (d Deps) payMode() config.PayMode {
	if d.PayMode == nil {
		return config.PayMixed
	}
	return d.PayMode()
}
```

and in `me`: `profile.Build(cu, …, s.payMode())`. `webapp/orders.go`: `s.Orders.Details` → `s.Orders.Live().Details`. `webapp/pay.go`: `s.Orders.Providers.Get(...)` → `s.Orders.Live().Providers.Get(...)`. `webapp/orders_test.go` `withOrders`: build the service without the removed fields, then

```go
d.Orders.SetLive(orders.Live{PayMode: config.PayMixed, Manual: true, Details: "Карта", TTL: time.Hour})
```

and replace any `d.Orders.Providers = …`/`d.Orders.Manual = …` in the webapp tests with `d.Orders.UpdateLive(...)`.

`telegram/bot.go`: `Deps.PayMode func() config.PayMode`, same `payMode()` helper (on `Deps`), `b.profile` uses `b.payMode()`. `telegram/orders.go`: `b.Orders.Providers.Title(provider)` → `b.Orders.Live().Providers.Title(provider)`; `providerButtons`: `b.Orders.Live().Providers.Enabled()`.

Run: `go build ./... 2>&1 | head -30` — only `cmd/bot` should still fail.

- [ ] **Step 10: Rewrite `cmd/bot/main.go`**

Replace the whole file:

```go
// Command bot runs the Telegram bot and its WebApp server in one process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"remna-control-bot/internal/access"
	"remna-control-bot/internal/config"
	"remna-control-bot/internal/currency"
	"remna-control-bot/internal/notify"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/refcode"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/store"
	"remna-control-bot/internal/telegram"
	"remna-control-bot/internal/telegram/view"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/webapp"
	"remna-control-bot/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	set, err := settings.Open(ctx, st.DB(), cfg.RefSecret)
	if err != nil {
		return err
	}
	for _, k := range set.Lost() {
		log.Warn("stored secrets cannot be decrypted (REF_SECRET changed?): re-enter them in the admin settings", "section", k)
	}

	api := remnactl.New(cfg.RemnactlURL, cfg.RemnactlToken)
	cur := &currency.Source{API: api, Log: log, OnChange: view.SetCurrency}
	cur.SetFallback(fallbackCurrency(set.Snapshot()))
	base := cur.Get(ctx) // resolves once at startup: with the panel down the fallback is used and a warning is logged
	codec := refcode.New(cfg.RefSecret)
	mode := func() config.Mode { return set.Snapshot().General().Mode }
	payMode := func() config.PayMode { return set.Snapshot().General().PayMode }
	acc := access.New(api, codec, mode, st)

	svc := &orders.Service{DB: st.DB(), Chats: st, API: api, Now: time.Now, Log: log}
	svc.BaseCurrency = cur.Get
	// Provider invoices need a confirmed currency: the panel answered, or the operator set one explicitly.
	svc.CurrencyKnown = func() bool { return cur.Known() || set.Snapshot().General().BaseCurrency != "" }

	var bot *telegram.Bot
	starsLink := func(ctx context.Context, title, description, payload string, stars int) (string, error) {
		return bot.InvoiceLink(ctx, title, description, payload, stars)
	}
	apply := func(snap settings.Snapshot) {
		reg := payments.Build(payments.BuildInput{Settings: snap, PublicURL: cfg.PublicURL, WebAppURL: cfg.WebAppURL(), StarsLink: starsLink})
		g, m := snap.General(), snap.Manual()
		cur.SetFallback(fallbackCurrency(snap))
		svc.SetLive(orders.Live{PayMode: g.PayMode, Manual: manualOn(cfg, m), Details: m.Details, TTL: g.OrderTTL(), Providers: reg})
		log.Info("settings applied", "mode", g.Mode, "pay", g.PayMode, "manual", manualOn(cfg, m), "providers", reg.IDs())
		if len(reg.IDs()) > 0 && !strings.HasPrefix(cfg.PublicURL, "https://") {
			log.Warn("payment providers are on but PUBLIC_URL is not https: webhooks will not reach the bot", "url", cfg.PublicURL)
		}
	}
	apply(set.Snapshot())
	set.OnChange(apply)
	if len(svc.Live().Providers.IDs()) > 0 {
		log.Info("payment providers invoice in the base currency", "currency", base, "providers", svc.Live().Providers.IDs())
	}
	unconfirmed := len(svc.Live().Providers.IDs()) > 0 && !svc.CurrencyKnown()
	if unconfirmed {
		logCurrencyUnconfirmed(log, base)
	}

	bot, err = telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Orders: svc, Prompts: st, AdminChatID: cfg.AdminChatID, IsAdmin: cfg.IsAdmin,
		WebAppURL: cfg.WebAppURL(), PayMode: payMode, Log: log,
	})
	if err != nil {
		return err
	}
	svc.Notify = bot
	if svc.Live().Manual {
		bot.CheckAdminChat(ctx)
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, PayMode: payMode, Orders: svc, Currency: cur.Get, Static: web.Dist, Log: log,
	})

	errc := make(chan error, 1)
	go func() { errc <- app.Listen(cfg.HTTPAddr) }()
	go bot.Run(ctx)
	n := &notify.Runner{
		API: api, Store: st, Send: bot.SendNotice, Log: log,
		Conf: func() notify.Conf {
			g := set.Snapshot().General()
			return notify.Conf{Interval: g.NotifyEvery(), Rules: notify.Rules{TrafficPct: g.TrafficAlertPct, RemindDays: g.RemindDescending()}}
		},
	}
	go n.Run(ctx)
	go sweepLoop(ctx, svc, log)
	go providerLoop(ctx, svc, log)
	go currencyLoop(ctx, cur, log, func() bool { return len(svc.Live().Providers.IDs()) > 0 }, svc.CurrencyKnown, unconfirmed)
	log.Info("started", "currency", base, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	return app.Shutdown()
}

// manualOn: transfers need a text to show, and (for now) a chat to send receipts to and someone to decide.
func manualOn(cfg config.Config, m settings.Manual) bool {
	return m.Active() && cfg.AdminChatID != 0 && len(cfg.AdminIDs) > 0
}

// fallbackCurrency is what is shown while the panel cannot say: the operator's explicit choice, else RUB.
func fallbackCurrency(s settings.Snapshot) string {
	if c := s.General().BaseCurrency; c != "" {
		return c
	}
	return "RUB"
}

// sweepLoop expires stale orders and fails interrupted approvals.
func sweepLoop(ctx context.Context, svc *orders.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if err := svc.ExpireStale(ctx); err != nil {
			log.Error("orders: sweep", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// providerLoop recovers lost provider webhooks and expires stale invoices; it also covers retired providers,
// whose invoices may still be paid.
func providerLoop(ctx context.Context, svc *orders.Service, log *slog.Logger) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if err := svc.SweepProviders(ctx); err != nil {
			log.Error("orders: provider sweep", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// logCurrencyUnconfirmed explains why provider payments refuse to invoice.
func logCurrencyUnconfirmed(log *slog.Logger, fallback string) {
	log.Error("provider payments are disabled until the panel reports its base currency: re-issue the bot token with the `stats:settings` scope or set the base currency in the admin settings",
		"fallback", fallback)
}

// currencyLoop keeps the displayed base currency fresh even when no invoice or WebApp request asks for it: Get
// refreshes the Source (whose TTL/backoff decides when the panel is really called) and feeds view.SetCurrency.
// While providers are on it also repeats the "currency not confirmed" error every 10 minutes and says when it
// clears; unconfirmed is the state already reported at startup.
func currencyLoop(ctx context.Context, cur *currency.Source, log *slog.Logger, providers, known func() bool, unconfirmed bool) {
	const repeat = 10 * time.Minute
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		code := cur.Get(ctx)
		if !providers() {
			unconfirmed = false
			continue
		}
		switch {
		case !known():
			if now := time.Now(); !unconfirmed || now.Sub(last) >= repeat {
				logCurrencyUnconfirmed(log, code)
				last = now
			}
			unconfirmed = true
		case unconfirmed:
			unconfirmed = false
			log.Info("provider payments are enabled: the base currency is confirmed", "currency", code)
		}
	}
}
```

- [ ] **Step 11: Verify the whole tree**

Run: `gofmt -l . ; go vet ./... && go test ./... -race -count=1`
Expected: no gofmt output; all packages PASS. Fix any remaining compile error in tests by the table in Step 3.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "refactor: read operational settings from the settings store; slim env config"
```

---

### Task 4: Admin read model — `ListAll`, `Status.Valid`, receipt streaming

**Files:**
- Modify: `internal/orders/order.go`, `internal/orders/repo.go`
- Create: `internal/orders/admin_test.go`, `internal/telegram/receipt.go`

**Interfaces:**
- Consumes: Task 3 `setup`/`submitted` test helpers.
- Produces:
  - `func (s Status) Valid() bool`
  - `func (s *Service) ListAll(ctx context.Context, statuses []Status, limit, offset int) ([]Order, int, error)` — newest first; no statuses = everything except `new` drafts.
  - `func (b *Bot) Receipt(ctx context.Context, fileID string) (io.ReadCloser, string, error)` (consumed by `webapp.ReceiptSource` in Task 5).

- [ ] **Step 1: Failing test**

Create `internal/orders/admin_test.go`:

```go
package orders

import (
	"context"
	"testing"
)

func TestListAll(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	first := submitted(t, s, c) // pending_review
	second, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 50})
	if err != nil {
		t.Fatal(err)
	}

	all, total, err := s.ListAll(ctx, nil, 10, 0)
	if err != nil || total != 2 || len(all) != 2 || all[0].ID != second.ID || all[1].ID != first.ID {
		t.Fatalf("all, newest first: %v %d %+v", err, total, all)
	}
	pending, total, err := s.ListAll(ctx, []Status{StatusPendingReview}, 10, 0)
	if err != nil || total != 1 || len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending: %v %d %+v", err, total, pending)
	}
	if none, total, _ := s.ListAll(ctx, []Status{StatusApproved, StatusFailed}, 10, 0); total != 0 || len(none) != 0 {
		t.Fatalf("approved/failed: %d %+v", total, none)
	}
	page, total, _ := s.ListAll(ctx, nil, 1, 1)
	if total != 2 || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("second page: %d %+v", total, page)
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusNew, StatusPendingReview, StatusFailed, StatusCancelled} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	for _, s := range []Status{"", "pending", "APPROVED", "approved,failed"} {
		if s.Valid() {
			t.Errorf("%q must be invalid", s)
		}
	}
}
```

Run: `go test ./internal/orders/ -run 'ListAll|StatusValid' 2>&1 | head -5` → FAIL (undefined).

- [ ] **Step 2: Implement**

`order.go` — after the `Status` constants:

```go
var allStatuses = []Status{StatusNew, StatusAwaitingReceipt, StatusAwaitingPayment, StatusPendingReview, StatusApproving,
	StatusApproved, StatusCredited, StatusFailed, StatusRejected, StatusExpired, StatusCancelled}

// Valid: s is one of the order statuses (admin filters come from the client).
func (s Status) Valid() bool { return slices.Contains(allStatuses, s) }
```

(add `"slices"` to its imports). `repo.go` — after `List`:

```go
// ListAll is every customer's orders for the admins, newest first. With no
// statuses it hides drafts nobody paid for (StatusNew), like List does.
func (s *Service) ListAll(ctx context.Context, statuses []Status, limit, offset int) ([]Order, int, error) {
	where, args := `status != ?`, []any{StatusNew}
	if len(statuses) > 0 {
		where = `status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		args = args[:0]
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM orders WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *o)
	}
	return out, total, rows.Err()
}
```

Run: `go test ./internal/orders/ -race -count=1` → PASS.

- [ ] **Step 3: Receipt streaming**

Verify the download-link helper exists: `go doc github.com/go-telegram/bot Bot.FileDownloadLink` (expected: `func (b *Bot) FileDownloadLink(f *models.File) string`). Create `internal/telegram/receipt.go`:

```go
package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	tgbot "github.com/go-telegram/bot"
)

const maxReceiptBytes = 20 << 20

var receiptClient = &http.Client{Timeout: 30 * time.Second}

// Receipt opens the file a customer sent as a receipt. The download URL carries
// the bot token, so the bytes are streamed through the server and the URL never
// leaves it. The caller closes the reader.
func (b *Bot) Receipt(ctx context.Context, fileID string) (io.ReadCloser, string, error) {
	f, err := b.tg.GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.tg.FileDownloadLink(f), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := receiptClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("receipt: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("receipt: telegram answered HTTP %d", resp.StatusCode)
	}
	body := struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, maxReceiptBytes), resp.Body}
	return body, resp.Header.Get("Content-Type"), nil
}
```

Run: `go vet ./... && go build ./...` → clean.

- [ ] **Step 4: Commit**

```bash
git add internal/orders internal/telegram/receipt.go
git commit -m "feat(orders): admin order listing and receipt streaming"
```

---

### Task 5: Admin API

**Files:**
- Create: `internal/webapp/admin.go`, `internal/webapp/admin_test.go`
- Modify: `internal/webapp/server.go`, `internal/webapp/orders.go` (`orderErr`), `cmd/bot/main.go`

**Interfaces:**
- Consumes: Task 1 `settings.Store`; Task 2 `payments.Checker`, `Registry.Get`; Task 4 `ListAll`, `Status.Valid`, `Bot.Receipt`.
- Produces (HTTP contract for Task 6), all under `{WEBAPP_PATH}/api/admin`, all behind initData + `ADMIN_IDS`:
  - `GET /me` → `{id, name, currency}`
  - `GET /orders?status=a,b&page=N` → `{orders: AdminOrder[], total, unreachable_admins: number[]}` (20 per page; `status` empty/`all` = everything but drafts)
  - `GET /orders/:id` → `AdminOrder` (+`customer_name`)
  - `POST /orders/:id/approve` body `{decision: "full"|"debt"|"credit", amount?: number}` → `AdminOrder`
  - `POST /orders/:id/reject` body `{reason?: string}` → `AdminOrder`
  - `POST /orders/:id/retry` → `AdminOrder`
  - `GET /orders/:id/receipt` → image/file bytes
  - `GET /settings` → `{sections: {<key>: object}, lost: string[], webhook_base: string}` (secrets masked)
  - `PUT /settings/:section` body = section object → masked section; 422 `{code:"invalid", message}`; 404 unknown
  - `POST /settings/:section/test` → `{ok, supported, message?}`
  - Errors keep the existing `{code}` shape: 401 `unauthorized`, 403 `forbidden`, 404 `not_found`, 409 `state_changed`/`underpaid`/`not_active`, 400 `bad_request`.
- `webapp.Deps` gains `Settings *settings.Store`, `IsAdmin func(int64) bool`, `PublicURL string`, `Receipts ReceiptSource`, `Unreachable func() []int64` (nil-safe).

- [ ] **Step 1: Failing tests**

Create `internal/webapp/admin_test.go`:

```go
package webapp

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/store"
	"remna-control-bot/internal/remnactl"
)

const adminID = 900 // an admin who is not a panel customer

func adminApp(t *testing.T) (*Deps, *remnactl.Fake) {
	t.Helper()
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if d.Settings, err = settings.Open(context.Background(), st.DB(), "ref"); err != nil {
		t.Fatal(err)
	}
	d.IsAdmin = func(id int64) bool { return id == adminID }
	d.PublicURL = "https://pub.example"
	return d, f
}

// pending creates a top-up order of 100 for customer 5 (telegram 77) and hands it to the admins.
func pending(t *testing.T, d *Deps, f *remnactl.Fake) string {
	t.Helper()
	ctx := context.Background()
	c, err := f.GetCustomer(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	o, err := d.Orders.Create(ctx, c, 77, orders.Request{Kind: orders.KindTopup, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Orders.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Orders.AttachReceipt(ctx, 77, orders.ReceiptText, "", "paid"); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func TestAdminAuth(t *testing.T) {
	d, _ := adminApp(t)
	for name, c := range map[string]struct {
		auth string
		want int
	}{
		"no header":            {"", 401},
		"garbage":              {"tma garbage", 401},
		"customer, not admin":  {tma(77), 403},
		"stranger":             {tma(5), 403},
		"admin, not a customer": {tma(adminID), 200},
	} {
		if code, body := get(t, d, "/bot/api/admin/me", c.auth); code != c.want {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if code, _ := get(t, d, "/bot/api/admin/orders", tma(77)); code != 403 {
		t.Fatalf("a customer must not list orders: %d", code)
	}
	// The customer API still works for customers and still refuses non-customers.
	if code, _ := get(t, d, "/bot/api/me", tma(adminID)); code != 403 {
		t.Fatalf("admin who is not a customer on /me: %d", code)
	}
}

func TestAdminApproveTwice(t *testing.T) {
	d, f := adminApp(t)
	id := pending(t, d, f)

	code, body := do(t, d, "GET", "/bot/api/admin/orders?status=pending_review", tma(adminID), "")
	var list struct {
		Orders []struct {
			ID, Status string
		} `json:"orders"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || code != 200 || list.Total != 1 || list.Orders[0].ID != id {
		t.Fatalf("list %d %s", code, body)
	}
	if code, _ := do(t, d, "GET", "/bot/api/admin/orders?status=bogus", tma(adminID), ""); code != 400 {
		t.Fatalf("bad status filter: %d", code)
	}

	path := "/bot/api/admin/orders/" + id + "/approve"
	if code, body := do(t, d, "POST", path, tma(adminID), `{"decision":"full"}`); code != 200 || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("approve %d %s", code, body)
	}
	if code, body := do(t, d, "POST", path, tma(adminID), `{"decision":"full"}`); code != 409 {
		t.Fatalf("second admin must get a conflict, got %d %s", code, body)
	}
	if code, _ := do(t, d, "POST", "/bot/api/admin/orders/not-a-uuid/approve", tma(adminID), `{"decision":"full"}`); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
}

func TestAdminAmountAndReject(t *testing.T) {
	d, f := adminApp(t)
	id := pending(t, d, f)
	approve := "/bot/api/admin/orders/" + id + "/approve"
	// 60 received of 100: "full" is refused, "credit" works and records the corrected amount.
	if code, body := do(t, d, "POST", approve, tma(adminID), `{"decision":"full","amount":60}`); code != 409 || !strings.Contains(body, "underpaid") {
		t.Fatalf("underpaid %d %s", code, body)
	}
	code, body := do(t, d, "POST", approve, tma(adminID), `{"decision":"credit","amount":60}`)
	if code != 200 || !strings.Contains(body, `"status":"approved"`) || !strings.Contains(body, `"paid":60`) {
		t.Fatalf("credit %d %s", code, body)
	}

	id2 := pending(t, d, f)
	code, body = do(t, d, "POST", "/bot/api/admin/orders/"+id2+"/reject", tma(adminID), `{"reason":"no money"}`)
	if code != 200 || !strings.Contains(body, `"status":"rejected"`) || !strings.Contains(body, "no money") {
		t.Fatalf("reject %d %s", code, body)
	}
}

func TestAdminSettings(t *testing.T) {
	d, _ := adminApp(t)
	put := func(section, body string) (int, string) {
		return do(t, d, "PUT", "/bot/api/admin/settings/"+section, tma(adminID), body)
	}
	if code, body := put("pay.yookassa", `{"enabled":true,"vat_code":1}`); code != 422 || !strings.Contains(body, `"code":"invalid"`) {
		t.Fatalf("invalid %d %s", code, body)
	}
	if code, _ := put("pay.nope", `{}`); code != 404 {
		t.Fatalf("unknown section %d", code)
	}
	code, body := put("pay.yookassa", `{"enabled":true,"shop_id":"1","secret_key":"live_supersecretvalue","vat_code":1}`)
	if code != 200 || strings.Contains(body, "supersecret") || !strings.Contains(body, "••••alue") {
		t.Fatalf("save must answer masked: %d %s", code, body)
	}
	// The masked value comes back from the UI: the secret survives.
	if code, _ := put("pay.yookassa", `{"enabled":true,"shop_id":"2","secret_key":"••••alue","vat_code":1}`); code != 200 {
		t.Fatalf("echoed mask: %d", code)
	}
	if got := d.Settings.Snapshot().YooKassa(); got.SecretKey != "live_supersecretvalue" || got.ShopID != "2" {
		t.Fatalf("snapshot: %+v", got)
	}

	code, body = do(t, d, "GET", "/bot/api/admin/settings", tma(adminID), "")
	var got struct {
		Sections    map[string]map[string]any `json:"sections"`
		Lost        []string                  `json:"lost"`
		WebhookBase string                    `json:"webhook_base"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != 200 || len(got.Sections) != len(settings.Keys) ||
		got.WebhookBase != "https://pub.example" || strings.Contains(body, "supersecret") {
		t.Fatalf("get %d %s", code, body)
	}
	if code, _ := do(t, d, "GET", "/bot/api/admin/settings", tma(77), ""); code != 403 {
		t.Fatalf("customer on settings: %d", code)
	}
}

func TestAdminSettingsTest(t *testing.T) {
	d, _ := adminApp(t)
	test := func(section string) (int, string) {
		return do(t, d, "POST", "/bot/api/admin/settings/"+section+"/test", tma(adminID), "")
	}
	if code, _ := test("pay.cryptobot"); code != 409 {
		t.Fatalf("a provider that is not active cannot be tested: %d", code)
	}
	if code, _ := test("general"); code != 400 {
		t.Fatalf("general has no check: %d", code)
	}
}

type fakeReceipts struct{ gotID string }

func (r *fakeReceipts) Receipt(_ context.Context, id string) (io.ReadCloser, string, error) {
	r.gotID = id
	return io.NopCloser(strings.NewReader("JPEGDATA")), "application/octet-stream", nil
}

func TestAdminReceipt(t *testing.T) {
	d, f := adminApp(t)
	rc := &fakeReceipts{}
	d.Receipts = rc
	ctx := context.Background()
	c, _ := f.GetCustomer(ctx, 5)
	o, _ := d.Orders.Create(ctx, c, 77, orders.Request{Kind: orders.KindTopup, Amount: 10})
	d.Orders.RequestReceipt(ctx, c.ID, o.ID)
	if _, err := d.Orders.AttachReceipt(ctx, 77, orders.ReceiptPhoto, "file-1", ""); err != nil {
		t.Fatal(err)
	}
	code, body := do(t, d, "GET", "/bot/api/admin/orders/"+o.ID+"/receipt", tma(adminID), "")
	if code != 200 || body != "JPEGDATA" || rc.gotID != "file-1" {
		t.Fatalf("receipt %d %q %q", code, body, rc.gotID)
	}
	// A text receipt has no file.
	id := pending(t, d, f)
	if code, _ := do(t, d, "GET", "/bot/api/admin/orders/"+id+"/receipt", tma(adminID), ""); code != 404 {
		t.Fatalf("text receipt: %d", code)
	}
}
```

Run: `go test ./internal/webapp/ 2>&1 | head` → FAIL (undefined `Settings`, `IsAdmin`, …).

- [ ] **Step 2: Split authentication in `server.go`**

Add imports `"remna-control-bot/internal/settings"` and `"context"`/`"io"` as needed. Add to `Deps`:

```go
	Settings    *settings.Store
	IsAdmin     func(int64) bool
	PublicURL   string                // the origin providers call back to; shown next to webhook settings
	Receipts    ReceiptSource         // streams receipts from Telegram; nil: receipts are unavailable
	Unreachable func() []int64        // admins the bot could not reach; nil: none
```

Replace the route block and `auth` in `New`/`server.go`:

```go
	api := app.Group(d.Path + "/api")
	api.Use(s.initAuth)

	adm := api.Group("/admin", s.adminOnly)
	adm.Get("/me", s.adminMe)
	adm.Get("/orders", s.adminListOrders)
	adm.Get("/orders/:id", s.adminOrder(s.adminGetOrder))
	adm.Post("/orders/:id/approve", s.adminOrder(s.adminApprove))
	adm.Post("/orders/:id/reject", s.adminOrder(s.adminReject))
	adm.Post("/orders/:id/retry", s.adminOrder(s.adminRetry))
	adm.Get("/orders/:id/receipt", s.adminReceipt)
	adm.Get("/settings", s.adminGetSettings)
	adm.Put("/settings/:section", s.adminPutSettings)
	adm.Post("/settings/:section/test", s.adminTestSettings)

	for _, p := range []string{"/me", "/tariffs", "/orders"} {
		api.Use(p, s.customerAuth)
	}
	api.Get("/me", s.me)
	api.Get("/tariffs", s.tariffs)
	api.Use("/orders", s.ordersOn)
	api.Get("/orders", s.listOrders)
	api.Post("/orders/quote", s.quoteOrder)
	api.Post("/orders", s.createOrder)
	api.Post("/orders/:id/receipt", s.orderAction(s.receipt))
	api.Post("/orders/:id/pay-balance", s.orderAction(s.payBalance))
	api.Post("/orders/:id/cancel", s.orderAction(s.cancel))
```

and replace the old `auth` with:

```go
// initAuth validates the Telegram initData; who the caller is for the bot
// (customer, admin) is decided by the middleware of the route group.
func (s *server) initAuth(c *fiber.Ctx) error {
	raw, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "tma ")
	if !ok {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	u, err := initdata.Validate(raw, s.BotToken, maxInitDataAge, time.Now())
	if err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	c.Locals("tg", u.ID)
	c.Locals("tguser", u)
	return c.Next()
}

// customerAuth resolves the caller to their remna-control customer.
func (s *server) customerAuth(c *fiber.Ctx) error {
	cu, err := s.API.LookupByTelegram(c.UserContext(), c.Locals("tg").(int64))
	switch {
	case errors.Is(err, remnactl.ErrNotFound):
		return fail(c, fiber.StatusForbidden, "no_access")
	case err != nil:
		s.Log.Error("webapp lookup", "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	case cu.Archived:
		return fail(c, fiber.StatusForbidden, "no_access")
	}
	c.Locals("customer", cu)
	return c.Next()
}
```

In `orders.go` `orderErr` add before the default log line:

```go
	case errors.Is(err, orders.ErrUnderpaid):
		return fail(c, fiber.StatusConflict, "underpaid")
```

- [ ] **Step 3: Write `admin.go`**

```go
package webapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/webapp/initdata"
)

// ReceiptSource streams a receipt file kept at Telegram (*telegram.Bot).
type ReceiptSource interface {
	Receipt(ctx context.Context, fileID string) (io.ReadCloser, string, error)
}

const adminPage = 20

func adminName(u initdata.User) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
		return n
	}
	return strconv.FormatInt(u.ID, 10)
}

// adminOnly lets through the Telegram ids listed in ADMIN_IDS; they need not be panel customers.
func (s *server) adminOnly(c *fiber.Ctx) error {
	u, _ := c.Locals("tguser").(initdata.User)
	if s.IsAdmin == nil || u.ID == 0 || !s.IsAdmin(u.ID) {
		return fail(c, fiber.StatusForbidden, "forbidden")
	}
	c.Locals("admin", adminName(u))
	return c.Next()
}

func (s *server) adminMe(c *fiber.Ctx) error {
	u := c.Locals("tguser").(initdata.User)
	cur := "RUB"
	if s.Currency != nil {
		cur = s.Currency(c.UserContext())
	}
	return c.JSON(fiber.Map{"id": u.ID, "name": adminName(u), "currency": cur})
}

type adminOrderView struct {
	orderView
	CustomerID   int      `json:"customer_id"`
	CustomerName string   `json:"customer_name,omitempty"`
	TelegramID   int64    `json:"telegram_id"`
	Comment      string   `json:"comment,omitempty"`
	ReceiptKind  string   `json:"receipt_kind,omitempty"`
	Received     *float64 `json:"received"`
	Admin        string   `json:"admin,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	Error        string   `json:"error,omitempty"`
	ExternalID   string   `json:"external_id,omitempty"`
	PayAmount    float64  `json:"pay_amount,omitempty"`
	PayCurrency  string   `json:"pay_currency,omitempty"`
}

func adminOrderJSON(o orders.Order, customerName string) adminOrderView {
	return adminOrderView{
		orderView: orderJSON(o), CustomerID: o.CustomerID, CustomerName: customerName, TelegramID: o.ChatID,
		Comment: o.Comment, ReceiptKind: string(o.ReceiptKind), Received: o.Received, Admin: o.Admin,
		Decision: string(o.Decision), Error: o.Error, ExternalID: o.ExternalID, PayAmount: o.PayAmount, PayCurrency: o.PayCurrency,
	}
}

func (s *server) adminListOrders(c *fiber.Ctx) error {
	var statuses []orders.Status
	if q := c.Query("status"); q != "" && q != "all" {
		for _, v := range strings.Split(q, ",") {
			st := orders.Status(strings.TrimSpace(v))
			if !st.Valid() {
				return fail(c, fiber.StatusBadRequest, "bad_request")
			}
			statuses = append(statuses, st)
		}
	}
	page := min(max(c.QueryInt("page", 0), 0), 10000)
	list, total, err := s.Orders.ListAll(c.UserContext(), statuses, adminPage, page*adminPage)
	if err != nil {
		return s.orderErr(c, err)
	}
	out := make([]adminOrderView, 0, len(list))
	for _, o := range list {
		out = append(out, adminOrderJSON(o, ""))
	}
	unreachable := []int64{}
	if s.Unreachable != nil {
		unreachable = append(unreachable, s.Unreachable()...)
	}
	return c.JSON(fiber.Map{"orders": out, "total": total, "unreachable_admins": unreachable})
}

// adminOrder checks :id and turns fn's result into the admin order JSON.
func (s *server) adminOrder(fn func(c *fiber.Ctx, id, admin string) (*orders.Order, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
			return fail(c, fiber.StatusNotFound, "not_found")
		}
		o, err := fn(c, id, c.Locals("admin").(string))
		if err != nil {
			return s.orderErr(c, err)
		}
		name := ""
		if cu, err := s.API.GetCustomer(c.UserContext(), o.CustomerID); err == nil {
			name = cu.Name
		}
		return c.JSON(adminOrderJSON(*o, name))
	}
}

func (s *server) adminGetOrder(c *fiber.Ctx, id, _ string) (*orders.Order, error) {
	return s.Orders.Get(c.UserContext(), id)
}

func (s *server) adminApprove(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	var b struct {
		Decision string   `json:"decision"`
		Amount   *float64 `json:"amount"`
	}
	if err := c.BodyParser(&b); err != nil {
		return nil, fmt.Errorf("%w: body", orders.ErrBadRequest)
	}
	if b.Amount != nil {
		if _, err := s.Orders.SetReceived(c.UserContext(), id, *b.Amount, admin); err != nil {
			return nil, err
		}
	}
	return s.Orders.Approve(c.UserContext(), id, admin, orders.Decision(b.Decision))
}

func (s *server) adminReject(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	var b struct {
		Reason string `json:"reason"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&b); err != nil {
			return nil, fmt.Errorf("%w: body", orders.ErrBadRequest)
		}
	}
	return s.Orders.Reject(c.UserContext(), id, b.Reason, admin)
}

func (s *server) adminRetry(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	return s.Orders.Retry(c.UserContext(), id, admin)
}

// adminReceipt streams the customer's receipt file. The stream is closed by the
// HTTP server after the response is written, so it must not be closed here.
func (s *server) adminReceipt(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	o, err := s.Orders.Get(c.UserContext(), id)
	if err != nil {
		return s.orderErr(c, err)
	}
	if o.ReceiptFileID == "" || (o.ReceiptKind != orders.ReceiptPhoto && o.ReceiptKind != orders.ReceiptDocument) {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	if s.Receipts == nil {
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	rc, ctype, err := s.Receipts.Receipt(c.UserContext(), o.ReceiptFileID)
	if err != nil {
		s.Log.Warn("webapp receipt", "order", id, "err", err)
		return fail(c, fiber.StatusBadGateway, "unavailable")
	}
	if o.ReceiptKind == orders.ReceiptPhoto {
		ctype = "image/jpeg" // Telegram photos are JPEG whatever its file server says
	}
	c.Set(fiber.HeaderContentType, ctype)
	c.Set(fiber.HeaderCacheControl, "private, max-age=300")
	return c.SendStream(rc)
}

func (s *server) adminGetSettings(c *fiber.Ctx) error {
	sections := make(map[string]settings.Section, len(settings.Keys))
	for _, k := range settings.Keys {
		v, _ := s.Settings.View(k)
		sections[k] = v
	}
	return c.JSON(fiber.Map{"sections": sections, "lost": s.Settings.Lost(), "webhook_base": s.PublicURL})
}

func (s *server) adminPutSettings(c *fiber.Ctx) error {
	key := c.Params("section")
	err := s.Settings.Put(c.UserContext(), key, c.Body())
	var ve *settings.ValidationError
	switch {
	case errors.Is(err, settings.ErrUnknownSection):
		return fail(c, fiber.StatusNotFound, "not_found")
	case errors.As(err, &ve):
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"code": "invalid", "message": ve.Msg})
	case err != nil:
		s.Log.Error("webapp settings", "section", key, "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	v, _ := s.Settings.View(key)
	return c.JSON(v)
}

// adminTestSettings asks the provider of a saved, active payment section to
// prove its credentials. Sections without a provider have nothing to test.
func (s *server) adminTestSettings(c *fiber.Ctx) error {
	key := c.Params("section")
	id, ok := strings.CutPrefix(key, "pay.")
	if !ok || id == "manual" {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	p, ok := s.Orders.Live().Providers.Get(id)
	if !ok {
		return fail(c, fiber.StatusConflict, "not_active")
	}
	ck, ok := p.(payments.Checker)
	if !ok {
		return c.JSON(fiber.Map{"ok": false, "supported": false})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	msg, err := ck.Check(ctx)
	if err != nil {
		return c.JSON(fiber.Map{"ok": false, "supported": true, "message": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "supported": true, "message": msg})
}
```

- [ ] **Step 4: Run**

Run: `gofmt -l internal/webapp; go vet ./internal/webapp/ && go test ./internal/webapp/ -race -count=1`
Expected: PASS. If `TestAdminAuth` "garbage"/"no header" returns something other than 401, check that `api.Use(s.initAuth)` is registered before every other route.

- [ ] **Step 5: Wire `main.go`**

In the `webapp.New(webapp.Deps{…})` call add `Settings: set, IsAdmin: cfg.IsAdmin, PublicURL: cfg.PublicURL, Receipts: bot,`.

Run: `go build ./... && go test ./... -race -count=1` → PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(webapp): admin API for orders and settings"
```

---

### Task 6: SPA admin screens

**Files:**
- Create: `web/src/adminApi.ts`, `web/src/AdminApp.tsx`, `web/src/pages/admin/{text.ts,sections.ts,Receipt.tsx,SectionForm.tsx,AdminOrders.tsx,AdminOrder.tsx,AdminMethods.tsx,AdminGeneral.tsx}`
- Modify: `web/src/api.ts`, `web/src/types.ts`, `web/src/App.tsx`, `web/src/pages/Overview.tsx`; Go: `internal/profile/profile.go`, `internal/webapp/server.go` (`me` sets `is_admin`)

**Interfaces:**
- Consumes: the Task 5 HTTP contract. The SPA uses `HashRouter`, so the deep link from Telegram is `{WebAppURL}/#/admin/orders/<id>`.
- Produces: routes `/admin` (orders), `/admin/orders/:id`, `/admin/methods`, `/admin/general`; `Profile.is_admin`.

The admin screens are Russian-only strings kept in one file (`text.ts`) instead of the customer locale JSON: the audience is the operator, and it keeps the customer bundle's translations untouched.

- [ ] **Step 1: Backend: `is_admin` on `/me`**

`internal/profile/profile.go`: add to `Profile` after `Currency`: `IsAdmin bool \`json:"is_admin"\``. In `webapp/server.go` `me`, after `p.Currency` is set: `p.IsAdmin = s.IsAdmin != nil && s.IsAdmin(c.Locals("tg").(int64))`. Add to `TestMe` in `server_test.go` an assertion that the field is present and false for a customer (decode `IsAdmin bool \`json:"is_admin"\`` and check it is false). Run `go test ./internal/webapp/ ./internal/profile/ -count=1` → PASS.

- [ ] **Step 2: `api.ts` — expose `call`, carry the server message**

Change `ApiError` and `call`:

```ts
export class ApiError extends Error {
    constructor(public status: number, public code: string, public detail?: string) {
        super(code)
    }
}

export const authHeaders = (): Record<string, string> => ({ Authorization: `tma ${tg?.initData ?? ''}` })

// Relative to the page, so it works under any WEBAPP_PATH.
export async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = authHeaders()
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    const res = await fetch(`./api/${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
    if (!res.ok) {
        const b = await res.json().catch(() => ({}))
        throw new ApiError(res.status, b.code ?? 'error', b.message)
    }
    if (res.status === 204) return undefined as T
    return res.json()
}
```

In `types.ts` add `is_admin: boolean` to `Profile` and append:

```ts
export type Decision = 'full' | 'debt' | 'credit'

export interface AdminOrder extends Order {
    customer_id: number
    customer_name?: string
    telegram_id: number
    comment?: string
    receipt_kind?: 'photo' | 'document' | 'text' | ''
    received: number | null
    admin?: string
    decision?: string
    error?: string
    external_id?: string
    pay_amount?: number
    pay_currency?: string
}

export interface AdminSettings {
    sections: Record<string, Record<string, unknown>>
    lost: string[]
    webhook_base: string
}

export interface CheckResult {
    ok: boolean
    supported: boolean
    message?: string
}
```

(`Order` already exists in `types.ts` — the customer order shape; `AdminOrder` adds fields. Verify its field names match `orderView`: `id, number, kind, title, price, from_balance, to_pay, paid, method, status, reason, until, pay_url, created_at`; if `Order` lacks `from_balance`/`to_pay`/`paid`, add them as optional there.)

- [ ] **Step 3: `adminApi.ts`**

```ts
import { useQuery } from '@tanstack/react-query'

import { ApiError, authHeaders, call } from './api'
import type { AdminOrder, AdminSettings, CheckResult, Decision } from './types'

export interface AdminMe {
    id: number
    name: string
    currency: string
}

const retry = (n: number, e: unknown) => !(e instanceof ApiError && e.status < 500) && n < 2

export const useAdminMe = () => useQuery({ queryKey: ['admin', 'me'], queryFn: () => call<AdminMe>('GET', 'admin/me'), retry })

export const useAdminOrders = (status: string) =>
    useQuery({
        queryKey: ['admin', 'orders', status],
        queryFn: () => call<{ orders: AdminOrder[]; total: number; unreachable_admins: number[] }>('GET', `admin/orders?status=${status}`),
        refetchInterval: 15_000,
        retry
    })

export const useAdminOrder = (id: string) =>
    useQuery({ queryKey: ['admin', 'order', id], queryFn: () => call<AdminOrder>('GET', `admin/orders/${id}`), retry })

export const approveOrder = (id: string, decision: Decision, amount?: number) =>
    call<AdminOrder>('POST', `admin/orders/${id}/approve`, { decision, amount })
export const rejectOrder = (id: string, reason: string) => call<AdminOrder>('POST', `admin/orders/${id}/reject`, { reason })
export const retryOrder = (id: string) => call<AdminOrder>('POST', `admin/orders/${id}/retry`)

export const useAdminSettings = () =>
    useQuery({ queryKey: ['admin', 'settings'], queryFn: () => call<AdminSettings>('GET', 'admin/settings'), retry })
export const saveSection = (key: string, body: Record<string, unknown>) =>
    call<Record<string, unknown>>('PUT', `admin/settings/${key}`, body)
export const checkSection = (key: string) => call<CheckResult>('POST', `admin/settings/${key}/test`)

// The receipt needs the Authorization header, so it is fetched and shown from a blob URL.
export async function fetchReceipt(id: string): Promise<Blob> {
    const res = await fetch(`./api/admin/orders/${id}/receipt`, { headers: authHeaders() })
    if (!res.ok) throw new ApiError(res.status, 'error')
    return res.blob()
}
```

- [ ] **Step 4: `text.ts` and `sections.ts`**

`web/src/pages/admin/text.ts`:

```ts
// Admin screens are for the operator and Russian-only; kept apart from the customer locale files.
export const A = {
    title: 'Админка',
    tabs: { orders: 'Заказы', methods: 'Оплата', general: 'Общее' },
    orders: {
        filters: { review: 'Ждут решения', all: 'Все', failed: 'Ошибки' },
        empty: 'Заказов нет',
        unreachable: (ids: number[]) => `Уведомления не доходят до админов: ${ids.join(', ')}. Пусть нажмут /start в боте.`
    },
    order: {
        back: '← К заказам',
        customer: 'Клиент',
        method: 'Способ',
        comment: 'Комментарий клиента',
        receipt: 'Квитанция',
        receiptOpen: 'Открыть файл',
        amount: 'Получено',
        approve: 'Подтвердить',
        debt: 'Продлить в долг',
        credit: 'Только на баланс',
        reject: 'Отклонить',
        reason: 'Причина отказа (необязательно)',
        retry: 'Повторить',
        failed: 'Ошибка',
        decidedBy: (a: string) => `Решил: ${a}`,
        done: 'Заказ уже обработан'
    },
    status: {
        new: 'Черновик',
        awaiting_receipt: 'Ждёт квитанцию',
        awaiting_payment: 'Ждёт оплату',
        pending_review: 'На проверке',
        approving: 'Проводится',
        approved: 'Оплачен',
        credited: 'На балансе',
        failed: 'Ошибка',
        rejected: 'Отклонён',
        expired: 'Истёк',
        cancelled: 'Отменён'
    } as Record<string, string>,
    settings: {
        save: 'Сохранить',
        saved: 'Сохранено',
        test: 'Проверить',
        lost: 'Сохранённые ключи не расшифровались (изменился REF_SECRET). Введите их заново.',
        webhook: 'Webhook URL',
        noCheck: 'Для этого способа проверка недоступна',
        notActive: 'Сначала включите способ и сохраните'
    }
}
```

`web/src/pages/admin/sections.ts`:

```ts
export type Field = {
    name: string
    label: string
    kind: 'text' | 'secret' | 'number' | 'bool' | 'select' | 'textarea' | 'days'
    options?: string[]
    hint?: string
}

export type SectionDef = { key: string; title: string; fields: Field[]; webhook?: string }

const enabled: Field = { name: 'enabled', label: 'Включено', kind: 'bool' }

export const GENERAL: SectionDef = {
    key: 'general',
    title: 'Общие настройки',
    fields: [
        { name: 'mode', label: 'Режим бота', kind: 'select', options: ['closed', 'open'], hint: 'closed — только клиенты панели, open — любой' },
        { name: 'base_currency', label: 'Базовая валюта', kind: 'text', hint: 'Код из 3 заглавных букв. Пусто — берём из панели' },
        { name: 'pay_mode', label: 'Оплата с баланса', kind: 'select', options: ['mixed', 'referral_full'], hint: 'referral_full — реферальные деньги только если покрывают всю цену' },
        { name: 'notify_minutes', label: 'Проверка уведомлений, мин', kind: 'number', hint: '0 — выключить' },
        { name: 'traffic_alert_pct', label: 'Порог трафика, %', kind: 'number' },
        { name: 'remind_days', label: 'Напоминать за, дней', kind: 'days', hint: 'Через запятую: 3,1' },
        { name: 'order_ttl_hours', label: 'Срок жизни неоплаченного заказа, ч', kind: 'number' }
    ]
}

export const METHODS: SectionDef[] = [
    {
        key: 'pay.manual',
        title: 'Перевод с квитанцией',
        fields: [enabled, { name: 'details', label: 'Реквизиты', kind: 'textarea', hint: 'Показываются клиенту рядом с суммой' }]
    },
    {
        key: 'pay.yookassa',
        title: 'ЮKassa',
        webhook: 'yookassa',
        fields: [
            enabled,
            { name: 'shop_id', label: 'Shop ID', kind: 'text' },
            { name: 'secret_key', label: 'Секретный ключ', kind: 'secret' },
            { name: 'return_url', label: 'Куда вернуть клиента', kind: 'text', hint: 'Пусто — в приложение' },
            { name: 'receipt_email', label: 'Email для чека', kind: 'text', hint: 'Пусто — без чека' },
            { name: 'vat_code', label: 'Код НДС (1–6)', kind: 'number' }
        ]
    },
    {
        key: 'pay.cryptobot',
        title: 'CryptoBot',
        webhook: 'cryptobot',
        fields: [
            enabled,
            { name: 'token', label: 'API-токен', kind: 'secret' },
            { name: 'testnet', label: 'Тестовая сеть', kind: 'bool' },
            { name: 'assets', label: 'Принимаемые активы', kind: 'text', hint: 'USDT,TON — пусто: все' }
        ]
    },
    {
        key: 'pay.heleket',
        title: 'Heleket',
        webhook: 'heleket',
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text' },
            { name: 'api_key', label: 'API-ключ', kind: 'secret' },
            { name: 'to_currency', label: 'Актив (необязательно)', kind: 'text' },
            { name: 'network', label: 'Сеть (необязательно)', kind: 'text' }
        ]
    },
    {
        key: 'pay.pal24',
        title: 'PayPalych',
        webhook: 'pal24',
        fields: [
            enabled,
            { name: 'api_token', label: 'API-токен', kind: 'secret' },
            { name: 'shop_id', label: 'Shop ID', kind: 'text' },
            { name: 'signature_token', label: 'Токен подписи postback', kind: 'secret', hint: 'Пусто — используется API-токен' }
        ]
    },
    {
        key: 'pay.platega',
        title: 'Platega',
        webhook: 'platega',
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text' },
            { name: 'secret', label: 'Секрет', kind: 'secret' },
            { name: 'payment_method', label: 'Метод', kind: 'number', hint: '2 СБП QR, 11 карты, 12 зарубежные карты, 13 крипта' },
            { name: 'api_version', label: 'Версия API', kind: 'select', options: ['v1', 'v2'] },
            { name: 'return_url', label: 'URL успеха', kind: 'text', hint: 'Пусто — в приложение' },
            { name: 'failed_url', label: 'URL ошибки', kind: 'text' }
        ]
    },
    {
        key: 'pay.stars',
        title: 'Telegram Stars',
        fields: [enabled, { name: 'rate', label: 'Базовой валюты за 1 ⭐', kind: 'number' }]
    }
]
```

- [ ] **Step 5: `SectionForm.tsx` and `Receipt.tsx`**

`web/src/pages/admin/SectionForm.tsx`:

```tsx
import { Alert, Button, Card, Group, NumberInput, PasswordInput, Select, Stack, Switch, Text, Textarea, TextInput, Title } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ApiError } from '../../api'
import { checkSection, saveSection } from '../../adminApi'
import { A } from './text'
import type { Field, SectionDef } from './sections'

type Values = Record<string, unknown>

const toForm = (def: SectionDef, v: Values): Values =>
    Object.fromEntries(def.fields.map((f) => [f.name, f.kind === 'days' ? ((v[f.name] as number[]) ?? []).join(',') : v[f.name]]))

function toBody(def: SectionDef, v: Values): Values {
    const out: Values = {}
    for (const f of def.fields) {
        const x = v[f.name]
        if (f.kind === 'number') out[f.name] = Number(x)
        else if (f.kind === 'days') out[f.name] = String(x).split(',').map((d) => Number(d.trim())).filter((d) => !Number.isNaN(d) && d > 0)
        else out[f.name] = x
    }
    return out
}

function Input({ f, v, set }: { f: Field; v: unknown; set: (x: unknown) => void }) {
    const common = { label: f.label, description: f.hint }
    switch (f.kind) {
        case 'bool':
            return <Switch checked={!!v} label={f.label} onChange={(e) => set(e.currentTarget.checked)} />
        case 'secret':
            return <PasswordInput {...common} autoComplete="off" onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
        case 'number':
            return <NumberInput {...common} onChange={set} value={v as number} />
        case 'select':
            return <Select {...common} allowDeselect={false} data={f.options ?? []} onChange={set} value={String(v ?? '')} />
        case 'textarea':
            return <Textarea {...common} autosize minRows={3} onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
        default:
            return <TextInput {...common} onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
    }
}

const errorText = (e: unknown) => (e instanceof ApiError ? (e.detail ?? e.code) : String(e))

export function SectionForm({ def, initial, webhookBase }: { def: SectionDef; initial: Values; webhookBase: string }) {
    const qc = useQueryClient()
    const [v, setV] = useState<Values>(() => toForm(def, initial))
    const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
    const [busy, setBusy] = useState(false)

    const run = async (fn: () => Promise<string>) => {
        setBusy(true)
        try {
            setMsg({ ok: true, text: await fn() })
        } catch (e) {
            setMsg({ ok: false, text: errorText(e) })
        } finally {
            setBusy(false)
        }
    }
    const save = () =>
        run(async () => {
            const saved = await saveSection(def.key, toBody(def, v))
            setV(toForm(def, saved))
            await qc.invalidateQueries({ queryKey: ['admin', 'settings'] })
            return A.settings.saved
        })
    const test = () =>
        run(async () => {
            try {
                const r = await checkSection(def.key)
                if (!r.supported) return A.settings.noCheck
                if (!r.ok) throw new Error(r.message ?? 'error')
                return r.message ?? 'OK'
            } catch (e) {
                if (e instanceof ApiError && e.code === 'not_active') throw new Error(A.settings.notActive)
                throw e
            }
        })

    return (
        <Card padding="md" radius="lg" withBorder>
            <Stack gap="sm">
                <Title order={5}>{def.title}</Title>
                {def.fields.map((f) => (
                    <Input f={f} key={f.name} set={(x) => setV((p) => ({ ...p, [f.name]: x }))} v={v[f.name]} />
                ))}
                {def.webhook && (
                    <Text c="dimmed" size="xs">
                        {A.settings.webhook}: {webhookBase}/pay/{def.webhook}
                    </Text>
                )}
                {msg && <Alert color={msg.ok ? 'teal' : 'red'}>{msg.text}</Alert>}
                <Group>
                    <Button loading={busy} onClick={save}>{A.settings.save}</Button>
                    {def.webhook && <Button loading={busy} onClick={test} variant="default">{A.settings.test}</Button>}
                </Group>
            </Stack>
        </Card>
    )
}
```

`web/src/pages/admin/Receipt.tsx`:

```tsx
import { Anchor, Image, Loader, Text } from '@mantine/core'
import { useEffect, useState } from 'react'

import { fetchReceipt } from '../../adminApi'
import { A } from './text'

// Shows the receipt file of an order: an image inline, any other file as a link.
export function Receipt({ id, kind }: { id: string; kind: 'photo' | 'document' }) {
    const [url, setUrl] = useState<string | null>(null)
    const [failed, setFailed] = useState(false)
    useEffect(() => {
        let made: string | null = null
        let alive = true
        fetchReceipt(id)
            .then((b) => {
                if (!alive) return
                made = URL.createObjectURL(b)
                setUrl(made)
            })
            .catch(() => alive && setFailed(true))
        return () => {
            alive = false
            if (made) URL.revokeObjectURL(made)
        }
    }, [id])
    if (failed) return <Text c="red" size="sm">{A.order.receipt}: ✕</Text>
    if (!url) return <Loader size="sm" />
    return kind === 'photo' ? <Image radius="md" src={url} /> : <Anchor download href={url}>{A.order.receiptOpen}</Anchor>
}
```

- [ ] **Step 6: Screens**

`web/src/pages/admin/AdminOrders.tsx`:

```tsx
import { Alert, Badge, Card, Group, SegmentedControl, Stack, Text } from '@mantine/core'
import { useState } from 'react'
import { useNavigate } from 'react-router'

import { fmtDate, fmtMoney } from '@shared/format'
import { useAdminOrders } from '../../adminApi'
import { Empty } from '../../ui/Empty'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { A } from './text'

const FILTERS: Record<string, string> = { review: 'pending_review,failed', all: 'all', failed: 'failed' }
const COLOR: Record<string, string> = { pending_review: 'yellow', failed: 'red', approved: 'teal', credited: 'teal', rejected: 'gray' }

export function AdminOrders() {
    const [filter, setFilter] = useState('review')
    const navigate = useNavigate()
    const q = useAdminOrders(FILTERS[filter])
    return (
        <Stack gap="md">
            <SegmentedControl
                data={Object.keys(FILTERS).map((k) => ({ value: k, label: A.orders.filters[k as keyof typeof A.orders.filters] }))}
                fullWidth
                onChange={setFilter}
                value={filter}
            />
            {q.data && q.data.unreachable_admins.length > 0 && <Alert color="yellow">{A.orders.unreachable(q.data.unreachable_admins)}</Alert>}
            {q.isPending ? (
                <PageSkeleton />
            ) : q.data && q.data.orders.length > 0 ? (
                q.data.orders.map((o) => (
                    <Card key={o.id} onClick={() => navigate(`/admin/orders/${o.id}`)} padding="md" radius="lg" style={{ cursor: 'pointer' }} withBorder>
                        <Group justify="space-between" wrap="nowrap">
                            <Stack gap={2}>
                                <Text fw={600} size="sm">№{o.number} · {o.title}</Text>
                                <Text c="dimmed" size="xs">{fmtDate(o.created_at)} · клиент #{o.customer_id}</Text>
                            </Stack>
                            <Stack align="flex-end" gap={2}>
                                <Text fw={600}>{fmtMoney(o.paid, 2)}</Text>
                                <Badge color={COLOR[o.status] ?? 'blue'} variant="light">{A.status[o.status] ?? o.status}</Badge>
                            </Stack>
                        </Group>
                    </Card>
                ))
            ) : (
                <Empty title={A.orders.empty} />
            )}
        </Stack>
    )
}
```

(`Empty` takes `title` and an optional `icon` per its usage in `Balance.tsx`; if `icon` is required by its props, pass `icon={<IconInbox />}` from `@tabler/icons-react`.)

`web/src/pages/admin/AdminOrder.tsx`:

```tsx
import { Alert, Badge, Button, Card, Group, NumberInput, Stack, Text, Textarea, Title } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { fmtMoney } from '@shared/format'
import { ApiError } from '../../api'
import { approveOrder, rejectOrder, retryOrder, useAdminOrder } from '../../adminApi'
import type { AdminOrder as Order, Decision } from '../../types'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { Receipt } from './Receipt'
import { A } from './text'

function Actions({ o }: { o: Order }) {
    const qc = useQueryClient()
    const [amount, setAmount] = useState<number>(o.received ?? o.to_pay)
    const [reason, setReason] = useState('')
    const [busy, setBusy] = useState(false)
    const [err, setErr] = useState<string | null>(null)
    const run = async (fn: () => Promise<Order>) => {
        setBusy(true)
        setErr(null)
        try {
            const next = await fn()
            qc.setQueryData(['admin', 'order', o.id], next)
            await qc.invalidateQueries({ queryKey: ['admin', 'orders'] })
        } catch (e) {
            setErr(e instanceof ApiError && e.code === 'state_changed' ? A.order.done : e instanceof ApiError ? e.code : String(e))
            await qc.invalidateQueries({ queryKey: ['admin', 'order', o.id] })
        } finally {
            setBusy(false)
        }
    }
    // Send the amount only when the admin changed it.
    const sent = amount !== (o.received ?? o.to_pay) ? amount : undefined
    const decide = (d: Decision) => run(() => approveOrder(o.id, d, sent))
    const under = amount < o.to_pay

    if (o.status === 'failed') {
        return (
            <Stack>
                {err && <Alert color="red">{err}</Alert>}
                <Button loading={busy} onClick={() => run(() => retryOrder(o.id))}>{A.order.retry}</Button>
            </Stack>
        )
    }
    if (o.status !== 'pending_review') return null
    return (
        <Stack>
            <NumberInput decimalScale={2} label={A.order.amount} min={0} onChange={(v) => setAmount(Number(v))} value={amount} />
            {err && <Alert color="red">{err}</Alert>}
            {!under && <Button loading={busy} onClick={() => decide('full')}>{A.order.approve}</Button>}
            {under && o.kind === 'renew' && <Button loading={busy} onClick={() => decide('debt')}>{A.order.debt}</Button>}
            {under && <Button loading={busy} onClick={() => decide('credit')} variant="light">{A.order.credit}</Button>}
            <Textarea autosize label={A.order.reason} minRows={1} onChange={(e) => setReason(e.currentTarget.value)} value={reason} />
            <Button color="red" loading={busy} onClick={() => run(() => rejectOrder(o.id, reason))} variant="light">{A.order.reject}</Button>
        </Stack>
    )
}

export function AdminOrder() {
    const { id = '' } = useParams()
    const navigate = useNavigate()
    const q = useAdminOrder(id)
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">{q.error instanceof ApiError ? q.error.code : 'error'}</Alert>
    const o = q.data
    return (
        <Stack gap="md">
            <Button onClick={() => navigate('/admin')} variant="subtle" w="fit-content">{A.order.back}</Button>
            <Group justify="space-between">
                <Title order={4}>№{o.number}</Title>
                <Badge variant="light">{A.status[o.status] ?? o.status}</Badge>
            </Group>
            <Card padding="md" radius="lg" withBorder>
                <Stack gap={4}>
                    <Text fw={600}>{o.title}</Text>
                    <Text size="sm">{fmtMoney(o.price, 2)}{o.from_balance > 0 ? ` · с баланса −${fmtMoney(o.from_balance, 2)}` : ''} · к оплате <b>{fmtMoney(o.to_pay, 2)}</b></Text>
                    <Text c="dimmed" size="sm">{A.order.customer}: {o.customer_name ?? ''} #{o.customer_id} · tg {o.telegram_id}</Text>
                    <Text c="dimmed" size="sm">{A.order.method}: {o.method}</Text>
                    {o.admin && <Text c="dimmed" size="sm">{A.order.decidedBy(o.admin)}</Text>}
                    {o.reason && <Text size="sm">{o.reason}</Text>}
                </Stack>
            </Card>
            {o.comment && <Alert title={A.order.comment}>{o.comment}</Alert>}
            {o.error && <Alert color="red" title={A.order.failed}>{o.error}</Alert>}
            {(o.receipt_kind === 'photo' || o.receipt_kind === 'document') && <Receipt id={o.id} kind={o.receipt_kind} />}
            <Actions key={`${o.status}:${o.received}`} o={o} />
        </Stack>
    )
}
```

`web/src/pages/admin/AdminMethods.tsx`:

```tsx
import { Alert, Stack } from '@mantine/core'

import { useAdminSettings } from '../../adminApi'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { SectionForm } from './SectionForm'
import { METHODS } from './sections'
import { A } from './text'

export function AdminMethods() {
    const q = useAdminSettings()
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">error</Alert>
    const s = q.data
    return (
        <Stack gap="md">
            {s.lost.length > 0 && <Alert color="yellow">{A.settings.lost}</Alert>}
            {METHODS.map((def) => (
                <SectionForm def={def} initial={s.sections[def.key]} key={def.key} webhookBase={s.webhook_base} />
            ))}
        </Stack>
    )
}
```

`web/src/pages/admin/AdminGeneral.tsx`:

```tsx
import { Alert } from '@mantine/core'

import { useAdminSettings } from '../../adminApi'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { SectionForm } from './SectionForm'
import { GENERAL } from './sections'

export function AdminGeneral() {
    const q = useAdminSettings()
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">error</Alert>
    return <SectionForm def={GENERAL} initial={q.data.sections[GENERAL.key]} webhookBase={q.data.webhook_base} />
}
```

- [ ] **Step 7: `AdminApp.tsx` and routing**

`web/src/AdminApp.tsx`:

```tsx
import { Box, SegmentedControl, Stack, Title } from '@mantine/core'
import { Route, Routes, useLocation, useNavigate } from 'react-router'

import { setFormat } from '@shared/format'
import { ApiError } from './api'
import { useAdminMe } from './adminApi'
import { AdminGeneral } from './pages/admin/AdminGeneral'
import { AdminMethods } from './pages/admin/AdminMethods'
import { AdminOrder } from './pages/admin/AdminOrder'
import { AdminOrders } from './pages/admin/AdminOrders'
import { A } from './pages/admin/text'
import { tg } from './telegram'
import { ErrorScreen } from './ui/ErrorScreen'
import { PageSkeleton } from './ui/PageSkeleton'

const TABS = [
    { value: '/admin', label: A.tabs.orders },
    { value: '/admin/methods', label: A.tabs.methods },
    { value: '/admin/general', label: A.tabs.general }
]

export function AdminApp() {
    const me = useAdminMe()
    const { pathname } = useLocation()
    const navigate = useNavigate()
    if (!tg) return <ErrorScreen code="no_tg" />
    if (me.isPending) return <PageSkeleton />
    if (me.isError) {
        const e = me.error
        const code = e instanceof ApiError && e.status === 403 ? 'no_access' : e instanceof ApiError && e.status === 401 ? 'no_tg' : 'unavailable'
        return <ErrorScreen code={code} onRetry={code === 'unavailable' ? () => me.refetch() : undefined} />
    }
    setFormat({ base: me.data.currency })
    const tab = TABS.find((t) => t.value !== '/admin' && pathname.startsWith(t.value))?.value ?? '/admin'
    return (
        <Box p="md">
            <Stack gap="md">
                <Title order={3}>{A.title}</Title>
                <SegmentedControl data={TABS} fullWidth onChange={(v) => navigate(v)} value={tab} />
                <Routes>
                    <Route element={<AdminOrders />} path="/admin" />
                    <Route element={<AdminOrder />} path="/admin/orders/:id" />
                    <Route element={<AdminMethods />} path="/admin/methods" />
                    <Route element={<AdminGeneral />} path="/admin/general" />
                    <Route element={<AdminOrders />} path="*" />
                </Routes>
            </Stack>
        </Box>
    )
}
```

In `App.tsx` rename the existing `App` to `CustomerApp` and add the switch:

```tsx
import { useLocation } from 'react-router'
import { AdminApp } from './AdminApp'
// …existing imports…

export function App() {
    const { pathname } = useLocation()
    return pathname.startsWith('/admin') ? <AdminApp /> : <CustomerApp />
}

function CustomerApp() {
    // …the body of the former App, unchanged…
}
```

In `Overview.tsx`, after the greeting `<Title>`, add (admins only):

```tsx
{p.is_admin && (
    <Button leftSection={<IconSettings size={18} />} onClick={() => navigate('/admin')} variant="light">
        Админка
    </Button>
)}
```

with `IconSettings` added to the `@tabler/icons-react` import.

- [ ] **Step 8: Typecheck and build**

Run: `cd web && bun run typecheck && bun run build`
Expected: both succeed. Fix type errors (most likely: `Order` field names, `Empty`/`ErrorScreen` prop names) by reading the referenced component.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat(web): admin screens for orders, payment methods and general settings"
```

---

### Task 7: DM notifications; remove the Telegram admin flow

**Files:**
- Modify: `internal/store/store.go` (+test), `internal/orders/{order,service,provider,fulfill,repo}.go` (+tests), `internal/telegram/{bot,orders,private,route}.go` (+tests), `internal/telegram/view/{screen,texts,orders}.go` (+tests), `internal/config/config.go` (+test), `internal/webapp/orders_test.go`, `cmd/bot/main.go`
- Create: `internal/telegram/adminnotify.go`, `internal/telegram/adminnotify_test.go`
- Delete: `internal/telegram/admin.go`, `internal/telegram/admin_test.go`

**Interfaces:**
- Consumes: `view.Links.Page(hash)` (WebAppInfo), `Bot.Receipt`.
- Produces:
  - `orders.Notifier`: `PayScreen`, **`Card(ctx, o Order) error`** (replaces `PostCard`/`UpdateCard`), `Customer`. `Order.AdminMsgID` and the `orders.admin_msg_id` column are gone.
  - `store.SaveNotice(ctx, orderID string, adminID int64, msgID int) error`, `store.Notices(ctx, orderID string) (map[int64]int, error)`.
  - `telegram.Deps`: `Admins []int64`, `Notices NoticeStore`, `APIBase string` (test hook); removed: `AdminChatID`, `Prompts`.
  - `(*Bot).Card`, `Alert`, `NotifyAdmins(ctx, text, page string)`, `Unreachable() []int64`.
  - `view.AdminCard(o orders.Order, c AdminCustomer, open *models.WebAppInfo) Screen`.

- [ ] **Step 1: Store — failing test, then implementation**

In `internal/store/store_test.go` replace the two `SavePrompt`/`TakePrompt` blocks (lines using `Prompt{…}`) with:

```go
	s.SaveNotice(ctx, "o1", 7, 100)
	s.SaveNotice(ctx, "o1", 8, 200)
	s.SaveNotice(ctx, "o1", 7, 101) // replaces
	got, err := s.Notices(ctx, "o1")
	if err != nil || len(got) != 2 || got[7] != 101 || got[8] != 200 {
		t.Fatalf("notices %v %v", got, err)
	}
	if none, _ := s.Notices(ctx, "other"); len(none) != 0 {
		t.Fatalf("other order: %v", none)
	}
```

Run `go test ./internal/store/ 2>&1 | head -3` → FAIL. In `store.go`: delete the `admin_prompts` table from the schema, the `admin_msg_id INTEGER NOT NULL DEFAULT 0,` line from `orders`, the `Prompt` type and `SavePrompt`/`TakePrompt`; add to the schema

```sql
	CREATE TABLE IF NOT EXISTS order_notices (
		order_id TEXT NOT NULL,
		admin_id INTEGER NOT NULL,
		msg_id INTEGER NOT NULL,
		PRIMARY KEY (order_id, admin_id));
```

and

```go
// SaveNotice remembers which message carries an order's card in an admin's chat.
func (s *Store) SaveNotice(ctx context.Context, orderID string, adminID int64, msgID int) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO order_notices (order_id, admin_id, msg_id) VALUES (?, ?, ?)`, orderID, adminID, msgID)
	return err
}

// Notices maps an admin id to the message that carries the order's card in their chat.
func (s *Store) Notices(ctx context.Context, orderID string) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT admin_id, msg_id FROM order_notices WHERE order_id = ?`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var admin int64
		var msg int
		if err := rows.Scan(&admin, &msg); err != nil {
			return nil, err
		}
		out[admin] = msg
	}
	return out, rows.Err()
}
```

(also fix the package doc comment if it mentions admin prompts). Run `go test ./internal/store/ -count=1` → PASS.

- [ ] **Step 2: Orders — one `Card` call instead of PostCard/UpdateCard**

`order.go`: delete the `AdminMsgID` field; replace the `Notifier` interface with

```go
type Notifier interface {
	PayScreen(ctx context.Context, o Order, details string) error // customer chat: amount + details + buttons
	// Card tells the admins where the order stands: a new card when it needs a decision, an edit of the cards already
	// sent otherwise. Failures are per admin and never fail the order.
	Card(ctx context.Context, o Order) error
	Customer(ctx context.Context, o Order) error // decision notice to the customer
}
```

`repo.go`: remove `admin_msg_id` from `cols` and `&o.AdminMsgID` from `scan`. `fulfill.go`: replace `card()` with

```go
// card keeps the admins' cards current.
func (s *Service) card(ctx context.Context, o *Order) {
	if err := s.Notify.Card(ctx, *o); err != nil {
		s.Log.Warn("orders: admin card", "order", o.ID, "err", err)
	}
}
```

`service.go` `AttachReceipt`: replace the whole `msgID, err := s.Notify.PostCard(...)` block through `return o, nil` with

```go
	s.card(ctx, o)
	return o, nil
```

`provider.go` `Paid`: replace the `msgID, err := s.Notify.PostCard(ctx, *o)` block (through its final `return o, nil`) with `s.card(ctx, o)` / `return o, nil`.

Tests: in `orders/helpers_test.go` change the spy to

```go
type spy struct {
	mu           sync.Mutex
	pay, cards, sent []Order
	alerts       []string
}

func (s *spy) Card(_ context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards = append(s.cards, o)
	return nil
}
```

(remove `PostCard`/`UpdateCard`). In `fulfill_test.go`, `provider_test.go`, `service_test.go` apply: `len(sp.posted) == 1` after a receipt/provider-review → `len(sp.cards) == 1 && sp.cards[0].Status == StatusPendingReview`; assertions on `sp.updated` → `sp.cards[len(sp.cards)-1].Status == X` (every state change now produces one card call); delete assertions about `AdminMsgID`/"card id is saved". In `webapp/orders_test.go` replace `nopNotify`'s two methods by `func (*nopNotify) Card(context.Context, orders.Order) error { return nil }`.

Run: `go test ./internal/orders/ ./internal/webapp/ -race -count=1` → PASS.

- [ ] **Step 3: `view` — text-only admin card with one web-app button**

`view/screen.go`: delete the `CbAdmin*` constants. `view/texts.go`: delete `BtnApprove, BtnDebt, BtnCredit, BtnEditAmount, BtnReject, BtnNoReason, BtnRetry, TxtNoRights, TxtAskAmount, TxtAskReason, TxtBadAmount`; add

```go
	BtnOpenAdmin = "🛠 Открыть в админке"
	TxtAdminOpen = "Админка бота:"
```

`view/orders.go`: delete `adminButtons`; replace `AdminCard` with

```go
// AdminCard is the notification an admin gets in their chat: what the order is
// and where it stands, with a button into the admin part of the WebApp where the
// decision is made (receipts are viewed there).
func AdminCard(o orders.Order, c AdminCustomer, open *models.WebAppInfo) Screen {
	var b strings.Builder
	b.WriteString(summary(o))
	who := Esc(clip(c.Name, 40))
	if c.Username != "" {
		who += " @" + Esc(c.Username)
	}
	fmt.Fprintf(&b, "\n\n👤 %s · клиент #%d", who, o.CustomerID)
	if o.Comment != "" {
		fmt.Fprintf(&b, "\n💬 %s", Esc(clip(o.Comment, 300)))
	}
	if o.ReceiptKind == orders.ReceiptPhoto || o.ReceiptKind == orders.ReceiptDocument {
		b.WriteString("\n📎 квитанция — в приложении")
	}
	if o.Received != nil {
		fmt.Fprintf(&b, "\n✏️ Сумма исправлена %s: %s → %s", Esc(o.Admin), plain(o.ToPay), Money(*o.Received))
	}
	switch o.Status {
	case orders.StatusPendingReview:
		b.WriteString("\n\n⏳ Ждёт решения")
	case orders.StatusApproved:
		fmt.Fprintf(&b, "\n\n✅ Подтвердил %s", Esc(o.Admin))
		if o.ResultUntil != nil {
			fmt.Fprintf(&b, ", действует до %s", Date(o.ResultUntil))
		}
	case orders.StatusCredited:
		fmt.Fprintf(&b, "\n\n💰 %s зачислил на баланс", Esc(o.Admin))
	case orders.StatusRejected:
		fmt.Fprintf(&b, "\n\n❌ Отклонил %s", Esc(o.Admin))
		if o.RejectReason != "" {
			fmt.Fprintf(&b, ": %s", Esc(clip(o.RejectReason, 200)))
		}
	case orders.StatusFailed:
		fmt.Fprintf(&b, "\n\n⚠️ Ошибка: %s", Esc(clip(o.Error, 200)))
	default:
		fmt.Fprintf(&b, "\n\n%s %s", statusIcon[o.Status], statusText[o.Status])
	}
	return Screen{Text: b.String(), Keyboard: kb([]btn{{Text: BtnOpenAdmin, WebApp: open}})}
}

// AdminEntry is a message with a button into a page of the admin part.
func AdminEntry(text string, open *models.WebAppInfo) Screen {
	return Screen{Text: text, Keyboard: kb([]btn{{Text: BtnOpenAdmin, WebApp: open}})}
}
```

In `view/orders_test.go` delete the tests of `adminButtons`/callback data and replace the `AdminCard` test with:

```go
func TestAdminCard(t *testing.T) {
	open := &models.WebAppInfo{URL: "https://x/bot/#/admin/orders/1"}
	o := orders.Order{ID: "12345678-aaaa", CustomerID: 5, Title: "Продление", Price: 700, ToPay: 700, Status: orders.StatusPendingReview,
		ReceiptKind: orders.ReceiptPhoto, Comment: "перевёл"}
	s := AdminCard(o, AdminCustomer{Name: "Ann", Username: "ann"}, open)
	for _, want := range []string{"№12345678", "Ann @ann", "клиент #5", "перевёл", "квитанция — в приложении", "Ждёт решения"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("card lacks %q:\n%s", want, s.Text)
		}
	}
	if len(s.Keyboard.InlineKeyboard) != 1 || s.Keyboard.InlineKeyboard[0][0].WebApp != open || s.Keyboard.InlineKeyboard[0][0].CallbackData != "" {
		t.Fatalf("the only button must open the web app: %+v", s.Keyboard)
	}
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	if s = AdminCard(o, AdminCustomer{}, open); !strings.Contains(s.Text, "Подтвердил @boss") || len(s.Keyboard.InlineKeyboard) != 1 {
		t.Fatalf("resolved card keeps the open button: %+v", s)
	}
}
```

(adjust imports of the test file: `strings`, `models`, `orders`). Run `go test ./internal/telegram/view/ -count=1` → PASS.

- [ ] **Step 4: Telegram — failing test for the fan-out**

Create `internal/telegram/adminnotify_test.go`:

```go
package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/store"
	"remna-control-bot/internal/remnactl"
)

// fakeTelegram answers the Bot API: chat 2 is an admin who never started the bot.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []string // "method chat_id"
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	var in map[string]any
	json.NewDecoder(r.Body).Decode(&in)
	chat, _ := in["chat_id"].(float64)
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+strings.TrimSuffix(strings.TrimSpace(jsonNum(chat)), ".0"))
	f.mu.Unlock()
	switch {
	case method == "getMe":
		io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"b","username":"remnabot"}}`)
	case (method == "sendMessage" || method == "editMessageText") && chat == 2:
		w.WriteHeader(403)
		io.WriteString(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`)
	case method == "sendMessage" || method == "editMessageText":
		io.WriteString(w, `{"ok":true,"result":{"message_id":77,"date":1,"chat":{"id":1,"type":"private"}}}`)
	default:
		io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

func (f *fakeTelegram) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func newCardBot(t *testing.T) (*Bot, *fakeTelegram, *store.Store) {
	t.Helper()
	ft := &fakeTelegram{}
	srv := httptest.NewServer(ft)
	t.Cleanup(srv.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := New(context.Background(), "123:ABC", Deps{
		API: remnactl.NewFake(), WebAppURL: "https://x/bot", Admins: []int64{1, 2, 3}, Notices: st, APIBase: srv.URL,
		IsAdmin: func(id int64) bool { return id >= 1 && id <= 3 },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b, ft, st
}

func TestCardFanOut(t *testing.T) {
	b, ft, st := newCardBot(t)
	ctx := context.Background()
	o := orders.Order{ID: "11111111-2222", CustomerID: 5, ChatID: 77, Title: "t", Price: 10, ToPay: 10, Status: orders.StatusPendingReview}

	err := b.Card(ctx, o)
	if err == nil {
		t.Fatal("one admin is unreachable: the error must say so")
	}
	if got := ft.count("sendMessage"); got != 3 {
		t.Fatalf("every admin is tried, even after one fails: %d sends", got)
	}
	notices, _ := st.Notices(ctx, o.ID)
	if len(notices) != 2 || notices[1] != 77 || notices[3] != 77 || notices[2] != 0 {
		t.Fatalf("only delivered cards are remembered: %v", notices)
	}
	if u := b.Unreachable(); len(u) != 1 || u[0] != 2 {
		t.Fatalf("unreachable: %v", u)
	}

	// A decision edits the cards that exist; it does not send new ones.
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	b.Card(ctx, o)
	if sends, edits := ft.count("sendMessage"), ft.count("editMessageText"); sends != 3 || edits != 2 {
		t.Fatalf("after the decision: %d sends, %d edits", sends, edits)
	}
}

func TestCardSkipsOrdersNobodyHasToDecide(t *testing.T) {
	b, ft, _ := newCardBot(t)
	o := orders.Order{ID: "balance-1", Status: orders.StatusApproved} // paid from the balance: no card was ever sent
	if err := b.Card(context.Background(), o); err != nil || ft.count("sendMessage") != 0 {
		t.Fatalf("approved order without cards must stay silent: %v, %d sends", err, ft.count("sendMessage"))
	}
	o.Status = orders.StatusFailed
	b.Card(context.Background(), o)
	if ft.count("sendMessage") != 3 {
		t.Fatalf("a failed order needs the admins: %d sends", ft.count("sendMessage"))
	}
}
```

Run `go test ./internal/telegram/ 2>&1 | head` → FAIL (undefined `Admins`, `Card`…).

- [ ] **Step 5: Telegram — implement and remove the old flow**

Delete `internal/telegram/admin.go` and `admin_test.go`. In `bot.go`: remove `PromptStore`, `Deps.Prompts`, `Deps.AdminChatID`; add to `Deps`:

```go
	Admins  []int64     // receive order cards; ADMIN_IDS in order
	Notices NoticeStore // where each admin's card for an order lives
	APIBase string      // Bot API base URL; "" is Telegram's own (tests point it at a fake)
```

to `Bot` add `mu sync.Mutex; unreachable map[int64]bool`; in `New`: build the option list

```go
	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(b.onUpdate),
		tgbot.WithMiddlewares(gate()),
		tgbot.WithErrorsHandler(func(err error) { d.Log.Error("telegram", "err", err) }),
	}
	if d.APIBase != "" {
		opts = append(opts, tgbot.WithServerURL(d.APIBase))
	}
	tg, err := tgbot.New(token, opts...)
```

Remove the `tg.RegisterHandler(…"o"…, b.onAdmin)` line; add `tg.RegisterHandler(tgbot.HandlerTypeMessageText, "admin", tgbot.MatchTypeCommand, b.onAdminCmd)`. In `onText` replace the first branch and the `AdminChatID` branch with:

```go
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
```

(delete the `// A private admin chat may answer a prompt…` block). `private.go`:

```go
// allowed lets through private chats with a known sender: in a group the customer screens would post one member's
// data for everyone, and admins no longer work in chats.
func allowed(u *models.Update) bool {
	switch {
	case u.Message != nil:
		return u.Message.From != nil && u.Message.Chat.Type == models.ChatTypePrivate
	case u.CallbackQuery != nil:
		m := u.CallbackQuery.Message.Message
		return m != nil && m.Chat.Type == models.ChatTypePrivate
	case u.PreCheckoutQuery != nil:
		return u.PreCheckoutQuery.From != nil
	}
	return false
}

func gate() tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			if allowed(u) {
				next(ctx, b, u)
			}
		}
	}
}
```

(delete `isCommand` and the `strings` import if unused). Update `private_test.go`: call `allowed(c.u)`/`allowed(u)`; delete the admin-chat cases (keep the private/group/sender/precheckout ones). In `telegram/orders.go` delete `Alert`, `adminCustomer` stays, delete `PostCard`, `UpdateCard`, `cardText` (they are replaced below), and remove the `_ orders.Alerter` line only if `Alert` moves (it does — keep the assertion in the new file).

Create `internal/telegram/adminnotify.go`:

```go
package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/telegram/view"
)

var (
	_ orders.Notifier = (*Bot)(nil)
	_ orders.Alerter  = (*Bot)(nil)
)

// NoticeStore remembers the message that carries an order's card in each admin's chat (*store.Store).
type NoticeStore interface {
	SaveNotice(ctx context.Context, orderID string, adminID int64, msgID int) error
	Notices(ctx context.Context, orderID string) (map[int64]int, error)
}

func (b *Bot) isAdmin(id int64) bool { return b.IsAdmin != nil && b.IsAdmin(id) }

// needsDecision: the states in which admins must act, so a card is sent even to those who have none yet.
func needsDecision(s orders.Status) bool { return s == orders.StatusPendingReview || s == orders.StatusFailed }

// Card sends every admin a card for an order that needs a decision and edits the cards already sent when it
// moves on. Admins are tried independently: one who never pressed /start (Telegram answers 403) does not hold
// back the others; the joined error names who failed.
func (b *Bot) Card(ctx context.Context, o orders.Order) error {
	have, err := b.Notices.Notices(ctx, o.ID)
	if err != nil {
		return err
	}
	if len(have) == 0 && !needsDecision(o.Status) {
		return nil
	}
	s := view.AdminCard(o, b.adminCustomer(ctx, o), b.links.Page("/admin/orders/"+o.ID))
	var errs []error
	for _, id := range b.Admins {
		var err error
		if msgID, ok := have[id]; ok {
			err = b.editCard(ctx, id, msgID, s)
		} else if needsDecision(o.Status) {
			err = b.sendCard(ctx, id, o.ID, s)
		}
		b.track(id, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("admin %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (b *Bot) sendCard(ctx context.Context, adminID int64, orderID string, s view.Screen) error {
	m, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: adminID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	if err != nil {
		return err
	}
	return b.Notices.SaveNotice(ctx, orderID, adminID, m.ID)
}

func (b *Bot) editCard(ctx context.Context, adminID int64, msgID int, s view.Screen) error {
	_, err := b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: adminID, MessageID: msgID, Text: s.Text,
		ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

// track remembers which admins the bot could not reach, for the Orders screen.
func (b *Bot) track(adminID int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		if b.unreachable == nil {
			b.unreachable = map[int64]bool{}
		}
		b.unreachable[adminID] = true
		b.Log.Warn("admin notification failed: the admin must press /start in the bot", "admin", adminID, "err", err)
		return
	}
	delete(b.unreachable, adminID)
}

// Unreachable lists the admins the last send to failed for.
func (b *Bot) Unreachable() []int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int64, 0, len(b.unreachable))
	for id := range b.unreachable {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Alert sends a plain message to every admin (no parse mode: the text may hold any characters).
func (b *Bot) Alert(ctx context.Context, text string) error {
	var errs []error
	for _, id := range b.Admins {
		_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: id, Text: text})
		b.track(id, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("admin %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// NotifyAdmins sends every admin text with a button into the given admin page ("/admin/methods").
func (b *Bot) NotifyAdmins(ctx context.Context, text, page string) {
	s := view.AdminEntry(text, b.links.Page(page))
	for _, id := range b.Admins {
		_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: id, Text: s.Text, ReplyMarkup: s.Keyboard})
		b.track(id, err)
	}
}

// onAdminCmd: /admin opens the admin part for an admin.
func (b *Bot) onAdminCmd(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate || !b.isAdmin(m.From.ID) {
		return
	}
	s := view.AdminEntry(view.TxtAdminOpen, b.links.Page("/admin"))
	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: s.Text, ReplyMarkup: s.Keyboard}); err != nil {
		b.Log.Error("admin command", "err", err)
	}
}
```

`adminCustomer` (kept in `orders.go`) calls `b.tg.GetChat` for `o.ChatID`; in the fake server `getChat` falls into the default branch (`result:true`), which the library cannot decode as a chat — it returns an error and the code already ignores it (`err == nil` guard). Keep as is.

Run: `go vet ./internal/telegram/... && go test ./internal/telegram/... -race -count=1`
Expected: PASS. Remaining compile errors will be deleted-symbol references in `bot.go`/tests (`CheckAdminChat`, `errText`, `alert`) — remove them; `parseOrderID`/`parseIDs` in `route.go` stay.

- [ ] **Step 6: Drop `ADMIN_CHAT_ID`; wire `main.go`**

`config.go`: delete the `AdminChatID` field, its parsing block, and the `strconv` use only if `ADMIN_IDS` parsing no longer needs it (it does — keep the import). `config_test.go`: in `TestAdmins` remove the `ADMIN_CHAT_ID` lines (`m["ADMIN_CHAT_ID"] = "-100"`, the `c.AdminChatID != -100` condition, and `"ADMIN_CHAT_ID"` from the invalid-values loop).

`main.go`:
- `manualOn`: `return m.Active() && len(cfg.AdminIDs) > 0`.
- `telegram.Deps{…}`: remove `Prompts` and `AdminChatID`; add `Admins: cfg.AdminIDs, Notices: st,`.
- Delete the `if svc.Live().Manual { bot.CheckAdminChat(ctx) }` block.
- In the `webapp.Deps` add `Unreachable: bot.Unreachable,`.
- After `svc.Notify = bot` add the first-run reminder:

```go
	if live := svc.Live(); !live.Manual && len(live.Providers.IDs()) == 0 && len(cfg.AdminIDs) > 0 {
		bot.NotifyAdmins(ctx, "Способы оплаты не настроены — настройте их в админке.", "/admin/methods")
	}
```

Run: `gofmt -l .; go vet ./... && go test ./... -race -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: DM order cards to admins with a web-app button; drop the Telegram admin flow"
```

---

### Task 8: Documentation, env and end-to-end verification

**Files:**
- Modify: `.env.example`, `README.md`, `docs/superpowers/specs/2026-10-07-payments-manual-design.md` (one-line note), `docs/superpowers/specs/2026-10-07-payment-providers-design.md` (one-line note)

- [ ] **Step 1: Slim `.env.example`**

Replace the whole file with (keeping the existing comments' tone):

```
# Telegram bot token from @BotFather
BOT_TOKEN=
# remna-control base URL and an API token created with the "bot" preset
REMNACTL_URL=http://vpn-control:8080
REMNACTL_TOKEN=
# Public origin where the WebApp is served; WEBAPP_PATH is appended.
# Payment providers send webhooks to PUBLIC_URL/pay/<provider>, so it must be https and reachable.
PUBLIC_URL=https://control.example.com
WEBAPP_PATH=/bot
# Secret for referral codes. It also derives the key that encrypts payment credentials stored in the database:
# changing it invalidates every referral link and the stored credentials must be entered again.
REF_SECRET=
# Comma-separated Telegram ids of the admins. They get order cards as private messages (each must press /start in the
# bot once) and may open the admin part of the WebApp (/admin in the bot, or the button on a card). Payment methods,
# bot mode, notifications and the rest are configured there, not here.
ADMIN_IDS=
HTTP_ADDR=:8090
DB_PATH=./data/bot.db
LOG_LEVEL=info
```

(Keep `LOCAL_DOMAIN` only if the previous file documented it for `compose.dev.yml` — preserve that existing block verbatim.)

- [ ] **Step 2: README**

Run `grep -n "ADMIN_CHAT_ID\|MANUAL_PAY_DETAILS\|YOOKASSA\|PAL24\|PLATEGA\|HELEKET\|CRYPTOBOT\|STARS_RATE\|BOT_MODE\|BALANCE_PAY_MODE\|NOTIFY_INTERVAL\|ORDER_TTL\|BASE_CURRENCY\|REMIND_DAYS\|TRAFFIC_ALERT" README.md` and rewrite every section that lists those variables so that: the env section lists only the kept variables; a new "Admin" section says that the first start has every payment method off, admins open `/admin` in the bot (or the button on an order card) to enable methods, enter credentials, set webhook URLs shown next to each provider, and change bot mode/notification/TTL settings; credentials are stored encrypted in the bot's database with a key derived from `REF_SECRET`.

- [ ] **Step 3: Mark superseded specs**

Add as the second line of `2026-10-07-payments-manual-design.md` and `2026-10-07-payment-providers-design.md`:

```
> **Superseded in part:** admin decisions and all `*_`-prefixed provider/payment env settings moved to the admin web app — see `2026-10-09-admin-webapp-settings-design.md`.
```

- [ ] **Step 4: Leftover sweep**

Run: `grep -rn "ADMIN_CHAT_ID\|AdminChatID\|AdminMsgID\|admin_prompts\|PostCard\|UpdateCard\|MANUAL_PAY_DETAILS" --include='*.go' --include='*.md' --include='*.yml' --include='*.example' --exclude-dir=node_modules --exclude-dir=.git . | grep -v "docs/superpowers"`
Expected: no output. Anything listed is a stale reference — remove it.

- [ ] **Step 5: Full verification**

Run:

```bash
gofmt -l . ; go vet ./... && go test ./... -race -count=1 && (cd web && bun run typecheck && bun run build)
```

Expected: no gofmt output; all Go packages PASS; typecheck and build succeed.

- [ ] **Step 6: Manual smoke (needs a real bot token; report honestly if skipped)**

With a fresh DB (`rm -f data/bot.db`), `ADMIN_IDS=<your id>` and the bot running:
1. `/admin` in the bot opens the admin app; "Оплата" shows every method off.
2. Enable "Перевод с квитанцией", enter details, Save; as a customer create a top-up in the WebApp and send a receipt in the bot.
3. The admin gets a DM card; the button opens the order; approve; the card in the DM changes to "Подтвердил …" and the customer gets the notice.
4. Enable a provider with credentials, press "Проверить" (CryptoBot/YooKassa) — success message; disable it — new orders no longer offer it.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "docs: admin web app configuration; slim env example"
```

---

## Self-Review (done while writing)

**Spec coverage** — Notifications as DMs to `ADMIN_IDS` with `web_app` button, `order_notices`, 403 handling and `UpdateCard` fan-out: Task 7. Admin check on every request, no panel lookup: Task 5 (`adminOnly`, `customerAuth` split). Settings in SQLite with JSON sections, AES-GCM + HKDF(`REF_SECRET`), masks, keep-on-mask, `OnChange`, immutable snapshot: Task 1. Hot reload of registry/Live/loops/currency fallback: Tasks 2–3. First-run defaults and the "no method configured" DM: Tasks 1, 3, 7. Admin API (orders, approve/reject/retry, receipt proxy, settings get/put/test): Task 5. SPA screens, `isAdmin` in `/me`: Task 6. Deleted Telegram callbacks, `onAdminReply`, `admin_prompts`, admin-chat gate, `ADMIN_CHAT_ID`, trimmed `.env.example`/docs: Tasks 7–8. Delivery order differs slightly from the spec (the spec's step "DM notifications" is last, as specified; admin API/UI are inserted before it) — the bot stays working after every task because `ADMIN_CHAT_ID` and the old callbacks live until Task 7.

**Deviation from the spec, deliberate:** providers that are switched off but still have complete credentials stay resolvable for webhooks and polling (`Registry.Retire`/`Offer`) — the spec says "enable switch" but a hard drop would lose payments for invoices already issued (Review Focus 1).

**Placeholder scan** — none left; Task 3 Step 3/Task 7 Step 2 give explicit replacement rules for mechanical test edits instead of "fix the tests".

**Type consistency** — `orders.Live`/`Service.Live()`/`SetLive`/`UpdateLive` (Task 3) are used in Tasks 5 and 7 as defined; `Registry.Get`/`Offer`/`Retire` (Task 2) match their uses in Tasks 3 and 5; `settings.Snapshot` accessors match `payments.Build` and `main.go`; `Notifier.Card` (Task 7) is the only notifier method used by `orders`; webapp contract fields in Task 5 match `AdminOrder` in Task 6 (`customer_id`, `customer_name`, `telegram_id`, `receipt_kind`, `received`, …).
