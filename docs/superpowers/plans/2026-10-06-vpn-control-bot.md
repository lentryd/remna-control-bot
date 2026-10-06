# vpn-control-bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Telegram bot + Telegram WebApp for vpn-control customers: open/closed access with referral and subscription-link onboarding, read-only views of subscriptions, add-ons, balance, invitees and tariffs.

**Architecture:** One Go process (one container): go-telegram/bot long polling + a Fiber HTTP server that serves the embedded WebApp and its JSON API under `WEBAPP_PATH`. All business data comes from the vpn-control public API (`/api/v1`, bearer token); the bot's own SQLite only keeps "waiting for a subscription link" state. Pure packages (`refcode`, `sublink`, `profile`, `access`, `telegram/view`, `webapp/initdata`) carry the logic and the tests; `telegram` and `webapp` are thin adapters.

**Tech Stack:** Go 1.26.4, `github.com/go-telegram/bot` v1.23.0, `github.com/gofiber/fiber/v2`, `modernc.org/sqlite`, `log/slog`; WebApp: Bun, React 19, TypeScript, Mantine 9, TanStack Query, react-router 7 (HashRouter), i18next, `@tabler/icons-react`, Geist, dayjs (versions copied from `../vpn-control/web/package.json`); Docker (multi-stage), traefik v3, mkcert.

**Spec:** `docs/superpowers/specs/2026-10-06-vpn-control-bot-design.md`

## Global Constraints

- Go module name `vpn-control-bot`, `go 1.26.4`; no CGO (`CGO_ENABLED=0`), SQLite via `modernc.org/sqlite`.
- Single process, single container; no releases/CI — built from `Dockerfile` / `compose.yml` / `compose.dev.yml` (never `docker-compose*`).
- ENV: `BOT_TOKEN`*, `BOT_MODE` (`open`|`closed`, default `closed`), `VPNCTL_URL`*, `VPNCTL_TOKEN`*, `PUBLIC_URL`*, `WEBAPP_PATH` (default `/bot`), `HTTP_ADDR` (default `:8090`), `DB_PATH` (default `/data/bot.db`), `LOG_LEVEL` (default `info`), `REF_SECRET`* (* = required).
- WebApp URL = `PUBLIC_URL + WEBAPP_PATH`.
- vpn-control is used only through `/api/v1` with `Authorization: Bearer <token>`; the only change in that repo is a "bot" token preset (Task 15).
- `customers:update` is a full PUT: always GET the customer, change `telegram_id`, PUT every field back.
- Referral payload format: `/start ref_<code>`, `<code>` from `refcode`, `[0-9A-Za-z]`, no storage.
- Admin fields `notes`, `contact`, `referral_percent` and ledger notes never reach the WebApp or bot screens.
- All user-facing bot texts are Russian and live in `internal/telegram/view/texts.go`; WebApp texts live in `web/public/locales/ru/bot.json`.
- Bot messages use `ParseMode: HTML`; every user-controlled string (names, titles) goes through `html.EscapeString`.
- Commits: conventional messages, **no** `Co-Authored-By` / Claude attribution lines.

## Review Focus

1. A subscription link pasted with surrounding spaces, a query string or a trailing slash (`https://sub.x/AbC123xyz/?a=1`) must still link the account → `sublink` tests in Task 3.
2. A `/start ref_<code>` where the code is valid but the referrer is archived/deleted: closed mode refuses, open mode creates the customer without a referrer — never a 500 → `access` tests in Task 7.
3. Linking a subscription whose customer already has *the same* `telegram_id` (user re-sends their link) must succeed without a PUT, and a customer with another `telegram_id` must be refused → `access` tests in Task 7.
4. Linking must not wipe other customer fields (full-PUT trap): the PUT body keeps `referrer_id`, `notes`, `contact`, `referral_percent` → `access` test `TestLinkKeepsFields` in Task 7.
5. A user whose name contains `<b>` / `&` must not break HTML rendering → view test `TestHomeEscapesName` in Task 9.

---

## File Structure

```
go.mod / go.sum
cmd/bot/main.go                     wiring, graceful shutdown
internal/config/config.go(+_test)   ENV → Config
internal/refcode/refcode.go(+_test) id ⇄ code
internal/sublink/sublink.go(+_test) link → short_uuid
internal/vpnctl/types.go            DTOs mirrored from vpn-control
internal/vpnctl/client.go(+_test)   HTTP client
internal/vpnctl/fake.go             in-memory Client for tests
internal/profile/profile.go(+_test) customer view model + tariffs + expiry state
internal/store/store.go(+_test)     SQLite awaiting-link state
internal/billing/billing.go         payments interface + NotImplemented
internal/access/access.go(+_test)   onboarding policy
internal/telegram/view/*.go(+_test) screens: texts + keyboards (pure), golden files in testdata/
internal/telegram/bot.go            go-telegram/bot adapter
internal/webapp/initdata/*.go       Telegram initData validation
internal/webapp/server.go(+_test)   Fiber app: /api/me, /api/tariffs, static
web/                                React WebApp (embed.go exposes dist/)
Dockerfile, compose.yml, compose.dev.yml, dev/traefik/*.yml, Makefile, .env.example, README.md
```

---

### Task 1: Project scaffold and config

**Files:**
- Create: `go.mod`, `.gitignore`, `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Mode` (`ModeOpen = "open"`, `ModeClosed = "closed"`), `config.Config{BotToken, Mode, VpnctlURL, VpnctlToken, PublicURL, WebAppPath, HTTPAddr, DBPath, LogLevel, RefSecret string/Mode}`, `func Load() (Config, error)`, `func (c Config) WebAppURL() string`.

- [ ] **Step 1: Init module**

```bash
go mod init vpn-control-bot
go mod edit -go=1.26.4
```

`.gitignore`:
```
/.env
/data/
/dev/certs/
/web/node_modules/
/web/dist/*
!/web/dist/.gitkeep
```

- [ ] **Step 2: Write the failing test** — `internal/config/config_test.go`

```go
package config

import "testing"

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
```

- [ ] **Step 3: Run** `go test ./internal/config/` — Expected: FAIL (`undefined: load`).

- [ ] **Step 4: Implement** — `internal/config/config.go`

```go
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
```

- [ ] **Step 5: Run** `go test ./internal/config/` — Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore internal/config
git commit -m "feat: project scaffold and env config"
```

---

### Task 2: Referral codes (`internal/refcode`)

**Files:**
- Create: `internal/refcode/refcode.go`, `internal/refcode/refcode_test.go`

**Interfaces:**
- Produces: `func New(secret string) *Codec`, `func (c *Codec) Encode(id int) string`, `func (c *Codec) Decode(code string) (int, bool)`.

- [ ] **Step 1: Write the failing test**

```go
package refcode

import (
	"regexp"
	"testing"
)

var codeRe = regexp.MustCompile(`^[0-9A-Za-z]{5,10}$`)

func TestRoundTripUniqueShort(t *testing.T) {
	c := New("secret")
	seen := map[string]int{}
	for _, id := range append(seq(1, 5000), 1<<20, 1<<31-1) {
		code := c.Encode(id)
		if !codeRe.MatchString(code) {
			t.Fatalf("id %d: bad code %q", id, code)
		}
		if prev, dup := seen[code]; dup {
			t.Fatalf("ids %d and %d share code %q", prev, id, code)
		}
		seen[code] = id
		got, ok := c.Decode(code)
		if !ok || got != id {
			t.Fatalf("Decode(%q) = %d,%v want %d", code, got, ok, id)
		}
	}
}

func TestTamperAndForeignSecret(t *testing.T) {
	c := New("secret")
	code := c.Encode(42)
	last := code[len(code)-1]
	repl := byte('a')
	if last == 'a' {
		repl = 'b'
	}
	if _, ok := c.Decode(code[:len(code)-1] + string(repl)); ok {
		t.Error("tampered signature accepted")
	}
	if _, ok := New("other").Decode(code); ok {
		t.Error("code from another secret accepted")
	}
}

func TestGarbage(t *testing.T) {
	c := New("secret")
	for _, s := range []string{"", "abcd", "!!!!!!", "ref_abc", "zzzzzzzzzzzzzzzzzzzzzz", "0" + c.Encode(7)} {
		if _, ok := c.Decode(s); ok {
			t.Errorf("Decode(%q) accepted", s)
		}
	}
}

func seq(a, b int) []int {
	out := make([]int, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}
```

- [ ] **Step 2: Run** `go test ./internal/refcode/` — Expected: FAIL (`undefined: New`).

- [ ] **Step 3: Implement** — `internal/refcode/refcode.go`

```go
// Package refcode turns a customer id into a short referral code and back.
// code = base62(id XOR mask) + base62(HMAC(secret, id)) padded to 4 chars;
// nothing is stored, and a forged code has to guess the 4-char signature.
package refcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"math"
	"strings"
)

const (
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sigLen   = 4
	sigSpace = 62 * 62 * 62 * 62
	maxLen   = 6 + sigLen // a 32-bit head is at most 6 base62 chars
)

type Codec struct {
	secret []byte
	mask   uint64
}

func New(secret string) *Codec {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("mask"))
	return &Codec{secret: []byte(secret), mask: uint64(binary.BigEndian.Uint32(m.Sum(nil)[:4]))}
}

// Encode supports ids in [1, MaxInt32].
func (c *Codec) Encode(id int) string { return encode(uint64(id)^c.mask, 1) + c.sig(id) }

func (c *Codec) Decode(code string) (int, bool) {
	if len(code) <= sigLen || len(code) > maxLen {
		return 0, false
	}
	head, sig := code[:len(code)-sigLen], code[len(code)-sigLen:]
	if len(head) > 1 && head[0] == '0' { // only the canonical form is valid
		return 0, false
	}
	v, ok := decode(head)
	if !ok {
		return 0, false
	}
	id := v ^ c.mask
	if id == 0 || id > math.MaxInt32 {
		return 0, false
	}
	if subtle.ConstantTimeCompare([]byte(c.sig(int(id))), []byte(sig)) != 1 {
		return 0, false
	}
	return int(id), true
}

func (c *Codec) sig(id int) string {
	m := hmac.New(sha256.New, c.secret)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(id))
	m.Write(b[:])
	return encode(binary.BigEndian.Uint64(m.Sum(nil)[:8])%sigSpace, sigLen)
}

func encode(v uint64, width int) string {
	var out []byte
	for v > 0 {
		out = append(out, alphabet[v%62])
		v /= 62
	}
	for len(out) < width {
		out = append(out, '0')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func decode(s string) (uint64, bool) {
	var v uint64
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(alphabet, s[i])
		if d < 0 || v > (math.MaxUint64-uint64(d))/62 {
			return 0, false
		}
		v = v*62 + uint64(d)
	}
	return v, true
}
```

- [ ] **Step 4: Run** `go test ./internal/refcode/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/refcode && git commit -m "feat: stateless referral codes"`

---

### Task 3: Subscription link parsing (`internal/sublink`)

**Files:**
- Create: `internal/sublink/sublink.go`, `internal/sublink/sublink_test.go`

**Interfaces:**
- Produces: `func Parse(s string) (shortUUID string, ok bool)`.

- [ ] **Step 1: Write the failing test**

```go
package sublink

import "testing"

func TestParse(t *testing.T) {
	ok := map[string]string{
		"https://sub.example.com/AbC123xyz":           "AbC123xyz",
		"  https://sub.example.com/AbC123xyz/  ":      "AbC123xyz",
		"https://sub.example.com/AbC123xyz/?a=1#frag": "AbC123xyz",
		"https://example.com/sub/path/Zz_-09abc":      "Zz_-09abc",
		"http://1.2.3.4:2096/AbC123xyz":               "AbC123xyz",
		"AbC123xyz":                                   "AbC123xyz",
	}
	for in, want := range ok {
		got, k := Parse(in)
		if !k || got != want {
			t.Errorf("Parse(%q) = %q,%v want %q", in, got, k, want)
		}
	}
	for _, in := range []string{"", "hello there", "abc", "https://example.com/", "ftp://x.com/AbC123xyz", "https://x.com/a b", "<b>AbC123</b>"} {
		if got, k := Parse(in); k {
			t.Errorf("Parse(%q) = %q, want reject", in, got)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/sublink/` — Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// Package sublink extracts a Remnawave short uuid from what a user pastes:
// a subscription URL (the uuid is its last path segment) or the bare uuid.
package sublink

import (
	"net/url"
	"regexp"
	"strings"
)

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

func Parse(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if tokenRe.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if last := segs[len(segs)-1]; tokenRe.MatchString(last) {
		return last, true
	}
	return "", false
}
```

- [ ] **Step 4: Run** `go test ./internal/sublink/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/sublink && git commit -m "feat: parse subscription links"`

---

### Task 4: vpn-control API client (`internal/vpnctl`)

**Files:**
- Create: `internal/vpnctl/types.go`, `internal/vpnctl/client.go`, `internal/vpnctl/client_test.go`, `internal/vpnctl/fake.go`

**Interfaces:**
- Produces:
  - `type Client interface { LookupByTelegram(ctx, int64) (*Customer, error); LookupByShortUUID(ctx, string) (*Customer, error); GetCustomer(ctx, int) (*Customer, error); CreateCustomer(ctx, CustomerInput) (int, error); UpdateCustomer(ctx, int, CustomerInput) error; ListTariffs(ctx) ([]Tariff, error) }`
  - `var ErrNotFound, ErrUnavailable error`; `type APIError struct{Status int; Code, Message string}` (`errors.Is(err, ErrNotFound)` works for 404, `ErrUnavailable` for 5xx and transport errors).
  - Types `Customer`, `Referee`, `Accrual`, `LedgerEntry`, `Subscription`, `Addon`, `RwUser`, `Tariff`, `Period`, `CustomerInput`; `func (c *Customer) Input() CustomerInput`.
  - `func New(baseURL, token string) *HTTPClient`; `func NewFake() *Fake` with `Add(Customer) *Customer`, fields `Tariffs []Tariff`, `Updates []Update` (`Update{ID int; In CustomerInput}`), `Creates []CustomerInput`, `Err error` (returned by every call when set).

- [ ] **Step 1: Types** — `internal/vpnctl/types.go` (JSON names copied from `../vpn-control/internal/api/handlers/{customers,views,tariffs}.go`)

```go
package vpnctl

import "time"

// Customer mirrors vpn-control's CustomerDetail (only the fields the bot uses).
type Customer struct {
	ID              int        `json:"id"`
	Name            string     `json:"name"`
	Contact         string     `json:"contact"`
	TelegramID      *int64     `json:"telegram_id"`
	Notes           string     `json:"notes"`
	ReferrerID      *int       `json:"referrer_id"`
	ReferralPercent *float64   `json:"referral_percent"`
	Archived        bool       `json:"archived"`
	Balance         float64    `json:"balance"`
	BalanceOwn      float64    `json:"balance_own"`
	BalanceReferral float64    `json:"balance_referral"`
	Monthly         float64    `json:"monthly"`
	NearestExpireAt *time.Time `json:"nearest_expire_at"`
	TotalPaid       float64    `json:"total_paid"`

	Subscriptions []Subscription `json:"subscriptions"`
	Ledger        []LedgerEntry  `json:"ledger"`
	Referrals     []Referee      `json:"referrals"`
	Accruals      []Accrual      `json:"accruals"`
}

type Referee struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Archived  bool    `json:"archived"`
	Monthly   float64 `json:"monthly"`
	TotalPaid float64 `json:"total_paid"`
}

type Accrual struct {
	ReferrerID int     `json:"referrer_id"`
	RefereeID  int     `json:"referee_id"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
}

type LedgerEntry struct {
	ID     int       `json:"id"`
	Type   string    `json:"type"` // payment|charge|adjustment|refund|referral
	Amount float64   `json:"amount"`
	Date   time.Time `json:"date"`
	Note   string    `json:"note"`
}

type Subscription struct {
	ID         int     `json:"id"`
	Title      string  `json:"title"`
	Label      string  `json:"label"`
	TariffName string  `json:"tariff_name"`
	Price      float64 `json:"price"`
	RwUser     *RwUser `json:"rw_user"`
	Addons     []Addon `json:"addons"`
}

type Addon struct {
	ID         int     `json:"id"`
	AddonName  string  `json:"addon_name"`
	TariffName string  `json:"tariff_name"`
	Price      float64 `json:"price"`
	Included   bool    `json:"included"`
	RwUser     *RwUser `json:"rw_user"`
}

type RwUser struct {
	ShortUUID         string     `json:"short_uuid"`
	Status            string     `json:"status"`
	ExpireAt          *time.Time `json:"expire_at"`
	Unlimited         bool       `json:"unlimited"`
	UsedTrafficBytes  int64      `json:"used_traffic_bytes"`
	TrafficLimitBytes int64      `json:"traffic_limit_bytes"`
	HwidDeviceLimit   *int       `json:"hwid_device_limit"`
	SubscriptionURL   string     `json:"subscription_url"`
	Deleted           bool       `json:"deleted"`
}

type Tariff struct {
	ID                int      `json:"id"`
	Kind              string   `json:"kind"` // base|addon
	AddonName         string   `json:"addon_name"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	MonthlyPrice      float64  `json:"monthly_price"`
	Active            bool     `json:"active"`
	SortOrder         int      `json:"sort_order"`
	TrafficLimitBytes int64    `json:"traffic_limit_bytes"`
	HwidLimit         *int     `json:"hwid_limit"`
	Periods           []Period `json:"periods"`
}

type Period struct {
	Months int     `json:"months"`
	Days   int     `json:"days"`
	Price  float64 `json:"price"`
}

// CustomerInput is vpn-control's customerInput. PUT replaces every field.
type CustomerInput struct {
	Name            string   `json:"name"`
	Contact         string   `json:"contact"`
	TelegramID      *int64   `json:"telegram_id"`
	Notes           string   `json:"notes"`
	ReferrerID      *int     `json:"referrer_id"`
	ReferralPercent *float64 `json:"referral_percent"`
	Archived        bool     `json:"archived"`
}

// Input copies every writable field, so a PUT built from it changes only
// what the caller modifies afterwards.
func (c *Customer) Input() CustomerInput {
	return CustomerInput{
		Name: c.Name, Contact: c.Contact, TelegramID: c.TelegramID, Notes: c.Notes,
		ReferrerID: c.ReferrerID, ReferralPercent: c.ReferralPercent, Archived: c.Archived,
	}
}
```

- [ ] **Step 2: Write the failing test** — `internal/vpnctl/client_test.go`

```go
package vpnctl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func server(t *testing.T, h http.HandlerFunc) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "tok")
}

func TestLookupByTelegram(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/customers/lookup" || r.URL.Query().Get("telegram_id") != "77" {
			t.Errorf("unexpected %s", r.URL)
		}
		w.Write([]byte(`{"id":5,"name":"Ann","telegram_id":77,"balance":120.5,"subscriptions":[{"id":1,"title":"Main","rw_user":{"short_uuid":"abc","subscription_url":"https://s/abc"},"addons":[]}]}`))
	})
	cu, err := c.LookupByTelegram(context.Background(), 77)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ID != 5 || cu.Balance != 120.5 || cu.Subscriptions[0].RwUser.SubscriptionURL != "https://s/abc" {
		t.Fatalf("bad decode: %+v", cu)
	}
}

func TestErrors(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("short_uuid") {
		case "missing":
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"customer not found","code":"customer.not_found"}`))
		default:
			w.WriteHeader(502)
		}
	})
	_, err := c.LookupByShortUUID(context.Background(), "missing")
	var ae *APIError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &ae) || ae.Code != "customer.not_found" {
		t.Fatalf("want not found APIError, got %v", err)
	}
	if _, err := c.LookupByShortUUID(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
	if _, err := New("http://127.0.0.1:1", "tok").GetCustomer(context.Background(), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("transport error: want unavailable, got %v", err)
	}
}

func TestCreateAndUpdate(t *testing.T) {
	var got CustomerInput
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/customers":
			w.WriteHeader(201)
			w.Write([]byte(`{"id":9}`))
		case r.Method == "PUT" && r.URL.Path == "/api/v1/customers/9":
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	tg, ref := int64(5), 3
	id, err := c.CreateCustomer(context.Background(), CustomerInput{Name: "N", TelegramID: &tg, ReferrerID: &ref})
	if err != nil || id != 9 || *got.ReferrerID != 3 {
		t.Fatalf("create: id=%d err=%v body=%+v", id, err, got)
	}
	if err := c.UpdateCustomer(context.Background(), 9, CustomerInput{Name: "M", Notes: "keep"}); err != nil || got.Notes != "keep" {
		t.Fatalf("update: err=%v body=%+v", err, got)
	}
}
```

- [ ] **Step 3: Run** `go test ./internal/vpnctl/` — Expected: FAIL (`undefined: New`).

- [ ] **Step 4: Implement** — `internal/vpnctl/client.go`

```go
// Package vpnctl is a client for vpn-control's public API (/api/v1).
package vpnctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("vpnctl: not found")
	ErrUnavailable = errors.New("vpnctl: panel unavailable")
)

type Client interface {
	LookupByTelegram(ctx context.Context, tgID int64) (*Customer, error)
	LookupByShortUUID(ctx context.Context, short string) (*Customer, error)
	GetCustomer(ctx context.Context, id int) (*Customer, error)
	CreateCustomer(ctx context.Context, in CustomerInput) (int, error)
	UpdateCustomer(ctx context.Context, id int, in CustomerInput) error
	ListTariffs(ctx context.Context) ([]Tariff, error)
}

// APIError is a non-2xx answer; vpn-control sends {"message","code"}.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vpnctl: %d %s %s", e.Status, e.Code, e.Message)
}

func (e *APIError) Is(target error) bool {
	return (target == ErrNotFound && e.Status == http.StatusNotFound) ||
		(target == ErrUnavailable && e.Status >= 500)
}

type HTTPClient struct {
	base  string
	token string
	hc    *http.Client
}

func New(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		base:  strings.TrimRight(baseURL, "/") + "/api/v1",
		token: token,
		hc:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *HTTPClient) LookupByTelegram(ctx context.Context, tgID int64) (*Customer, error) {
	return c.customer(ctx, "/customers/lookup?telegram_id="+strconv.FormatInt(tgID, 10))
}

func (c *HTTPClient) LookupByShortUUID(ctx context.Context, short string) (*Customer, error) {
	return c.customer(ctx, "/customers/lookup?short_uuid="+url.QueryEscape(short))
}

func (c *HTTPClient) GetCustomer(ctx context.Context, id int) (*Customer, error) {
	return c.customer(ctx, "/customers/"+strconv.Itoa(id))
}

func (c *HTTPClient) CreateCustomer(ctx context.Context, in CustomerInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/customers", in, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *HTTPClient) UpdateCustomer(ctx context.Context, id int, in CustomerInput) error {
	return c.do(ctx, http.MethodPut, "/customers/"+strconv.Itoa(id), in, nil)
}

func (c *HTTPClient) ListTariffs(ctx context.Context) ([]Tariff, error) {
	var out []Tariff
	return out, c.do(ctx, http.MethodGet, "/tariffs", nil, &out)
}

func (c *HTTPClient) customer(ctx context.Context, path string) (*Customer, error) {
	var out Customer
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return &APIError{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

- [ ] **Step 5: Fake** — `internal/vpnctl/fake.go` (used by `access` and `webapp` tests)

```go
package vpnctl

import (
	"context"
	"sort"
	"sync"
)

// Fake is an in-memory Client that behaves like vpn-control's lookup rules.
type Fake struct {
	mu        sync.Mutex
	customers map[int]*Customer
	nextID    int
	Tariffs   []Tariff
	Creates   []CustomerInput
	Updates   []Update
	Err       error // returned by every call when set
}

type Update struct {
	ID int
	In CustomerInput
}

var _ Client = (*Fake)(nil)

func NewFake() *Fake { return &Fake{customers: map[int]*Customer{}, nextID: 1} }

// Add stores c (assigning an id when zero) and returns the stored copy.
func (f *Fake) Add(c Customer) *Customer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == 0 {
		c.ID = f.nextID
	}
	if c.ID >= f.nextID {
		f.nextID = c.ID + 1
	}
	f.customers[c.ID] = &c
	return f.copy(&c)
}

func (f *Fake) copy(c *Customer) *Customer { cp := *c; return &cp }

// find mirrors LookupCustomer: active customers first, then the oldest.
func (f *Fake) find(match func(*Customer) bool) (*Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	ids := make([]int, 0, len(f.customers))
	for id := range f.customers {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var archived *Customer
	for _, id := range ids {
		c := f.customers[id]
		if !match(c) {
			continue
		}
		if !c.Archived {
			return f.copy(c), nil
		}
		if archived == nil {
			archived = c
		}
	}
	if archived != nil {
		return f.copy(archived), nil
	}
	return nil, &APIError{Status: 404, Code: "customer.not_found"}
}

func (f *Fake) LookupByTelegram(_ context.Context, tg int64) (*Customer, error) {
	return f.find(func(c *Customer) bool { return c.TelegramID != nil && *c.TelegramID == tg })
}

func (f *Fake) LookupByShortUUID(_ context.Context, short string) (*Customer, error) {
	return f.find(func(c *Customer) bool {
		for _, s := range c.Subscriptions {
			if s.RwUser != nil && s.RwUser.ShortUUID == short {
				return true
			}
			for _, a := range s.Addons {
				if a.RwUser != nil && a.RwUser.ShortUUID == short {
					return true
				}
			}
		}
		return false
	})
}

func (f *Fake) GetCustomer(_ context.Context, id int) (*Customer, error) {
	return f.find(func(c *Customer) bool { return c.ID == id })
}

func (f *Fake) CreateCustomer(_ context.Context, in CustomerInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	f.Creates = append(f.Creates, in)
	c := &Customer{ID: f.nextID}
	f.nextID++
	apply(c, in)
	f.customers[c.ID] = c
	return c.ID, nil
}

func (f *Fake) UpdateCustomer(_ context.Context, id int, in CustomerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	c, ok := f.customers[id]
	if !ok {
		return &APIError{Status: 404}
	}
	f.Updates = append(f.Updates, Update{ID: id, In: in})
	apply(c, in)
	return nil
}

func (f *Fake) ListTariffs(context.Context) ([]Tariff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Tariffs, f.Err
}

func apply(c *Customer, in CustomerInput) {
	c.Name, c.Contact, c.TelegramID, c.Notes = in.Name, in.Contact, in.TelegramID, in.Notes
	c.ReferrerID, c.ReferralPercent, c.Archived = in.ReferrerID, in.ReferralPercent, in.Archived
}
```

- [ ] **Step 6: Run** `go test ./internal/vpnctl/` — Expected: PASS.

- [ ] **Step 7: Commit** `git add internal/vpnctl && git commit -m "feat: vpn-control API client and fake"`

---

### Task 5: Customer profile model (`internal/profile`)

**Files:**
- Create: `internal/profile/profile.go`, `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: `vpnctl.Customer`, `vpnctl.Tariff`.
- Produces:
  - `type Profile struct{ ID int; Name string; Balance, BalanceOwn, BalanceReferral, Monthly float64; NearestExpireAt *time.Time; Subscriptions []Subscription; Ledger []LedgerEntry; Referrals []Referral; ReferralsEarned float64; RefLink string }` (JSON snake_case, see code).
  - `type Subscription struct{ ID int; Title, Tariff string; Price float64; Status string; ExpireAt *time.Time; Unlimited bool; UsedBytes, LimitBytes int64; DeviceLimit *int; URL string; Addons []Addon }`
  - `type Addon struct{ ID int; Name, Tariff string; Price float64; Included bool; Status string; ExpireAt *time.Time; Unlimited bool; UsedBytes, LimitBytes int64 }`
  - `type LedgerEntry struct{ Type string; Amount float64; Date time.Time }`
  - `type Referral struct{ Name string; Active bool; Monthly, TotalPaid, Earned float64 }`
  - `func Build(c *vpnctl.Customer, refLink string) Profile`
  - `type Tariff struct{ ID int; Kind, Name, Description, AddonName string; MonthlyPrice float64; TrafficLimitBytes int64; HwidLimit *int; Periods []vpnctl.Period }`, `func Tariffs(in []vpnctl.Tariff) []Tariff` (active only; base first, then by `SortOrder`, then name).
  - `type State int` (`StateNone`, `StateActive`, `StateSoon`, `StateExpired`), `func Expiry(exp *time.Time, unlimited bool, now time.Time) State` (soon = < 72h left).

- [ ] **Step 1: Write the failing test**

```go
package profile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

func ptr[T any](v T) *T { return &v }

func TestBuild(t *testing.T) {
	d1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &vpnctl.Customer{
		ID: 1, Name: "Ann", Notes: "SECRET-NOTE", Contact: "SECRET-CONTACT", Balance: 10,
		Subscriptions: []vpnctl.Subscription{
			{ID: 1, Title: "Main", TariffName: "Std", RwUser: &vpnctl.RwUser{SubscriptionURL: "https://s/a", Status: "ACTIVE"},
				Addons: []vpnctl.Addon{{ID: 3, AddonName: "YouTube", RwUser: &vpnctl.RwUser{Status: "ACTIVE"}}}},
			{ID: 2, Title: "Gone", RwUser: &vpnctl.RwUser{Deleted: true}},
		},
		Ledger: []vpnctl.LedgerEntry{{Type: "payment", Amount: 100, Date: d1, Note: "SECRET-LEDGER"}, {Type: "charge", Amount: -50, Date: d1.AddDate(0, 1, 0)}},
		Referrals: []vpnctl.Referee{{ID: 7, Name: "Bob", TotalPaid: 300}, {ID: 8, Name: "Eve", Archived: true}},
		Accruals: []vpnctl.Accrual{
			{ReferrerID: 1, RefereeID: 7, Amount: 30}, {ReferrerID: 1, RefereeID: 7, Amount: 15},
			{ReferrerID: 1, RefereeID: 8, Amount: 50}, {ReferrerID: 99, RefereeID: 1, Amount: 1000},
		},
	}
	p := Build(c, "https://t.me/bot?start=ref_x")

	if len(p.Subscriptions) != 1 || p.Subscriptions[0].URL != "https://s/a" || len(p.Subscriptions[0].Addons) != 1 {
		t.Fatalf("subscriptions: %+v", p.Subscriptions)
	}
	if p.Ledger[0].Amount != -50 { // newest first
		t.Fatalf("ledger order: %+v", p.Ledger)
	}
	if p.ReferralsEarned != 95 || p.Referrals[0].Name != "Eve" || p.Referrals[0].Earned != 50 || p.Referrals[1].Earned != 45 || p.Referrals[0].Active {
		t.Fatalf("referrals: %+v earned=%v", p.Referrals, p.ReferralsEarned)
	}
	raw, _ := json.Marshal(p)
	for _, secret := range []string{"SECRET-NOTE", "SECRET-CONTACT", "SECRET-LEDGER", "referral_percent"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("profile JSON leaks %q", secret)
		}
	}
}

func TestTariffs(t *testing.T) {
	got := Tariffs([]vpnctl.Tariff{
		{ID: 1, Kind: "addon", Name: "A", Active: true},
		{ID: 2, Kind: "base", Name: "B", Active: true, SortOrder: 2},
		{ID: 3, Kind: "base", Name: "C", Active: false},
		{ID: 4, Kind: "base", Name: "D", Active: true, SortOrder: 1},
	})
	ids := []int{}
	for _, t := range got {
		ids = append(ids, t.ID)
	}
	if len(ids) != 3 || ids[0] != 4 || ids[1] != 2 || ids[2] != 1 {
		t.Fatalf("order = %v", ids)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		exp       *time.Time
		unlimited bool
		want      State
	}{
		{nil, false, StateNone},
		{ptr(now.Add(-time.Hour)), false, StateExpired},
		{ptr(now.Add(48 * time.Hour)), false, StateSoon},
		{ptr(now.Add(100 * time.Hour)), false, StateActive},
		{ptr(now.Add(-time.Hour)), true, StateActive},
	}
	for i, c := range cases {
		if got := Expiry(c.exp, c.unlimited, now); got != c.want {
			t.Errorf("case %d: %v want %v", i, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/profile/` — Expected: FAIL.

- [ ] **Step 3: Implement** — `internal/profile/profile.go`

```go
// Package profile is the customer's own view of their data: what the bot
// screens and the WebApp show. Admin-only fields never get here.
package profile

import (
	"sort"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

type Profile struct {
	ID              int            `json:"id"`
	Name            string         `json:"name"`
	Balance         float64        `json:"balance"`
	BalanceOwn      float64        `json:"balance_own"`
	BalanceReferral float64        `json:"balance_referral"`
	Monthly         float64        `json:"monthly"`
	NearestExpireAt *time.Time     `json:"nearest_expire_at"`
	Subscriptions   []Subscription `json:"subscriptions"`
	Ledger          []LedgerEntry  `json:"ledger"`
	Referrals       []Referral     `json:"referrals"`
	ReferralsEarned float64        `json:"referrals_earned"`
	RefLink         string         `json:"ref_link"`
}

type Subscription struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Tariff      string     `json:"tariff"`
	Price       float64    `json:"price"`
	Status      string     `json:"status"`
	ExpireAt    *time.Time `json:"expire_at"`
	Unlimited   bool       `json:"unlimited"`
	UsedBytes   int64      `json:"used_bytes"`
	LimitBytes  int64      `json:"limit_bytes"`
	DeviceLimit *int       `json:"device_limit"`
	URL         string     `json:"url"`
	Addons      []Addon    `json:"addons"`
}

type Addon struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Tariff     string     `json:"tariff"`
	Price      float64    `json:"price"`
	Included   bool       `json:"included"`
	Status     string     `json:"status"`
	ExpireAt   *time.Time `json:"expire_at"`
	Unlimited  bool       `json:"unlimited"`
	UsedBytes  int64      `json:"used_bytes"`
	LimitBytes int64      `json:"limit_bytes"`
}

type LedgerEntry struct {
	Type   string    `json:"type"`
	Amount float64   `json:"amount"`
	Date   time.Time `json:"date"`
}

type Referral struct {
	Name      string  `json:"name"`
	Active    bool    `json:"active"`
	Monthly   float64 `json:"monthly"`
	TotalPaid float64 `json:"total_paid"`
	Earned    float64 `json:"earned"`
}

func Build(c *vpnctl.Customer, refLink string) Profile {
	p := Profile{
		ID: c.ID, Name: c.Name, Balance: c.Balance, BalanceOwn: c.BalanceOwn, BalanceReferral: c.BalanceReferral,
		Monthly: c.Monthly, NearestExpireAt: c.NearestExpireAt, RefLink: refLink,
		Subscriptions: []Subscription{}, Ledger: []LedgerEntry{}, Referrals: []Referral{},
	}
	for _, s := range c.Subscriptions {
		if s.RwUser != nil && s.RwUser.Deleted {
			continue
		}
		v := Subscription{ID: s.ID, Title: s.Title, Tariff: s.TariffName, Price: s.Price, Status: "PENDING", Addons: []Addon{}}
		if u := s.RwUser; u != nil {
			v.Status, v.ExpireAt, v.Unlimited = u.Status, u.ExpireAt, u.Unlimited
			v.UsedBytes, v.LimitBytes, v.DeviceLimit, v.URL = u.UsedTrafficBytes, u.TrafficLimitBytes, u.HwidDeviceLimit, u.SubscriptionURL
		}
		for _, a := range s.Addons {
			if a.RwUser != nil && a.RwUser.Deleted {
				continue
			}
			av := Addon{ID: a.ID, Name: a.AddonName, Tariff: a.TariffName, Price: a.Price, Included: a.Included, Status: "PENDING"}
			if u := a.RwUser; u != nil {
				av.Status, av.ExpireAt, av.Unlimited = u.Status, u.ExpireAt, u.Unlimited
				av.UsedBytes, av.LimitBytes = u.UsedTrafficBytes, u.TrafficLimitBytes
			}
			v.Addons = append(v.Addons, av)
		}
		p.Subscriptions = append(p.Subscriptions, v)
	}
	for _, l := range c.Ledger {
		p.Ledger = append(p.Ledger, LedgerEntry{Type: l.Type, Amount: l.Amount, Date: l.Date})
	}
	sort.SliceStable(p.Ledger, func(i, j int) bool { return p.Ledger[i].Date.After(p.Ledger[j].Date) })

	earned := map[int]float64{}
	for _, a := range c.Accruals {
		if a.ReferrerID == c.ID {
			earned[a.RefereeID] += a.Amount
			p.ReferralsEarned += a.Amount
		}
	}
	for _, r := range c.Referrals {
		p.Referrals = append(p.Referrals, Referral{Name: r.Name, Active: !r.Archived, Monthly: r.Monthly, TotalPaid: r.TotalPaid, Earned: earned[r.ID]})
	}
	sort.SliceStable(p.Referrals, func(i, j int) bool { return p.Referrals[i].Earned > p.Referrals[j].Earned })
	return p
}

type Tariff struct {
	ID                int             `json:"id"`
	Kind              string          `json:"kind"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	AddonName         string          `json:"addon_name"`
	MonthlyPrice      float64         `json:"monthly_price"`
	TrafficLimitBytes int64           `json:"traffic_limit_bytes"`
	HwidLimit         *int            `json:"hwid_limit"`
	Periods           []vpnctl.Period `json:"periods"`
}

func Tariffs(in []vpnctl.Tariff) []Tariff {
	out := []Tariff{}
	for _, t := range in {
		if !t.Active {
			continue
		}
		out = append(out, Tariff{
			ID: t.ID, Kind: t.Kind, Name: t.Name, Description: t.Description, AddonName: t.AddonName,
			MonthlyPrice: t.MonthlyPrice, TrafficLimitBytes: t.TrafficLimitBytes, HwidLimit: t.HwidLimit, Periods: t.Periods,
		})
	}
	order := map[string]int{}
	for _, t := range in {
		order[t.Name] = t.SortOrder
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Kind == "base") != (b.Kind == "base") {
			return a.Kind == "base"
		}
		if order[a.Name] != order[b.Name] {
			return order[a.Name] < order[b.Name]
		}
		return a.Name < b.Name
	})
	return out
}

type State int

const (
	StateNone State = iota
	StateActive
	StateSoon
	StateExpired
)

func Expiry(exp *time.Time, unlimited bool, now time.Time) State {
	switch {
	case unlimited:
		return StateActive
	case exp == nil:
		return StateNone
	case !exp.After(now):
		return StateExpired
	case exp.Sub(now) < 72*time.Hour:
		return StateSoon
	}
	return StateActive
}
```

- [ ] **Step 4: Run** `go test ./internal/profile/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/profile && git commit -m "feat: customer profile view model"`

---

### Task 6: State store and billing stub

**Files:**
- Create: `internal/store/store.go`, `internal/store/store_test.go`, `internal/billing/billing.go`

**Interfaces:**
- Produces: `store.Open(path string) (*Store, error)`, `(*Store).SetAwaitingLink(ctx, tgID int64, on bool) error`, `(*Store).AwaitingLink(ctx, tgID int64) (bool, error)`, `(*Store).Close() error`.
- Produces: `billing.Item{Kind string; ID int}`, `billing.Quote{Amount float64}`, `billing.Billing` interface `{Quote(ctx, customerID int, item Item) (Quote, error); Pay(ctx, customerID int, item Item) error}`, `billing.ErrNotImplemented`, `billing.NotImplemented{}`.

- [ ] **Step 1: Add dependency** `go get modernc.org/sqlite@latest`

- [ ] **Step 2: Write the failing test** — `internal/store/store_test.go`

```go
package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAwaitingLink(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sub", "bot.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if on, _ := s.AwaitingLink(ctx, 1); on {
		t.Fatal("fresh store: want false")
	}
	if err := s.SetAwaitingLink(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAwaitingLink(ctx, 1, true); err != nil { // idempotent
		t.Fatal(err)
	}
	s.Close()

	s, _ = Open(path) // survives restart
	defer s.Close()
	if on, _ := s.AwaitingLink(ctx, 1); !on {
		t.Fatal("want true after reopen")
	}
	s.SetAwaitingLink(ctx, 1, false)
	if on, _ := s.AwaitingLink(ctx, 1); on {
		t.Fatal("want false after clear")
	}
}
```

- [ ] **Step 3: Run** `go test ./internal/store/` — Expected: FAIL.

- [ ] **Step 4: Implement** — `internal/store/store.go`

```go
// Package store keeps the bot's own state: who is expected to send a
// subscription link. Customer data lives in vpn-control.
package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS awaiting_link (
		tg_id INTEGER PRIMARY KEY,
		since TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SetAwaitingLink(ctx context.Context, tgID int64, on bool) error {
	q := `DELETE FROM awaiting_link WHERE tg_id = ?`
	if on {
		q = `INSERT OR IGNORE INTO awaiting_link (tg_id) VALUES (?)`
	}
	_, err := s.db.ExecContext(ctx, q, tgID)
	return err
}

func (s *Store) AwaitingLink(ctx context.Context, tgID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM awaiting_link WHERE tg_id = ?`, tgID).Scan(&n)
	return n > 0, err
}
```

- [ ] **Step 5: Billing stub** — `internal/billing/billing.go`

```go
// Package billing is where paying for tariffs/extensions will live. For now
// every call answers ErrNotImplemented and the UI says "coming soon".
package billing

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("billing: not implemented")

// Item is what is being paid for: a tariff to buy or a subscription/add-on
// to extend (Kind: "tariff" | "subscription" | "addon").
type Item struct {
	Kind string
	ID   int
}

type Quote struct {
	Amount float64
}

type Billing interface {
	Quote(ctx context.Context, customerID int, item Item) (Quote, error)
	Pay(ctx context.Context, customerID int, item Item) error
}

type NotImplemented struct{}

func (NotImplemented) Quote(context.Context, int, Item) (Quote, error) { return Quote{}, ErrNotImplemented }
func (NotImplemented) Pay(context.Context, int, Item) error            { return ErrNotImplemented }
```

- [ ] **Step 6: Run** `go test ./internal/store/ && go build ./...` — Expected: PASS / no errors.

- [ ] **Step 7: Commit** `git add go.mod go.sum internal/store internal/billing && git commit -m "feat: sqlite state store and billing stub"`

---

### Task 7: Access policy and onboarding (`internal/access`)

**Files:**
- Create: `internal/access/access.go`, `internal/access/access_test.go`

**Interfaces:**
- Consumes: `vpnctl.Client`, `vpnctl.ErrNotFound`, `refcode.Codec`, `sublink.Parse`, `config.Mode`.
- Produces:
  - `type User struct{ ID int64; Name string }`
  - `type Outcome int` with `Member, Created, NeedLink, BadReferral, Archived, Linked, BadLink, NotFound, Taken`
  - `type Result struct{ Outcome Outcome; Customer *vpnctl.Customer }` (`Customer` set for `Member`, `Created`, `Linked`, `Archived`)
  - `type State interface{ SetAwaitingLink(ctx, int64, bool) error; AwaitingLink(ctx, int64) (bool, error) }` (satisfied by `*store.Store`)
  - `func New(api vpnctl.Client, codec *refcode.Codec, mode config.Mode, state State) *Service`
  - `(*Service).Start(ctx, u User, payload string) (Result, error)` — used for `/start` and for every other update with `payload == ""`.
  - `(*Service).Link(ctx, u User, text string) (Result, error)`
  - `(*Service).AwaitingLink(ctx, tgID int64) (bool, error)`
  - Errors other than "not found" (e.g. `vpnctl.ErrUnavailable`) are returned unchanged.

- [ ] **Step 1: Write the failing test** — `internal/access/access_test.go`

```go
package access

import (
	"context"
	"errors"
	"testing"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/vpnctl"
)

type memState map[int64]bool

func (m memState) SetAwaitingLink(_ context.Context, id int64, on bool) error { m[id] = on; return nil }
func (m memState) AwaitingLink(_ context.Context, id int64) (bool, error)     { return m[id], nil }

func ptr[T any](v T) *T { return &v }

var codec = refcode.New("s")

func setup(mode config.Mode) (*Service, *vpnctl.Fake, memState) {
	f := vpnctl.NewFake()
	st := memState{}
	return New(f, codec, mode, st), f, st
}

var ann = User{ID: 100, Name: "Ann"}

func TestStart(t *testing.T) {
	type tc struct {
		name     string
		mode     config.Mode
		seed     []vpnctl.Customer
		payload  string
		want     Outcome
		created  bool
		referrer *int
		awaiting bool
	}
	ref := vpnctl.Customer{ID: 10, Name: "Ref"}
	archivedRef := vpnctl.Customer{ID: 11, Name: "Old", Archived: true}
	member := vpnctl.Customer{ID: 20, Name: "Ann", TelegramID: ptr(int64(100))}
	gone := vpnctl.Customer{ID: 21, Name: "Ann", TelegramID: ptr(int64(100)), Archived: true}
	for _, c := range []tc{
		{name: "member closed", mode: config.ModeClosed, seed: []vpnctl.Customer{member}, want: Member},
		{name: "member ignores ref", mode: config.ModeOpen, seed: []vpnctl.Customer{member, ref}, payload: "ref_" + codec.Encode(10), want: Member},
		{name: "archived member", mode: config.ModeOpen, seed: []vpnctl.Customer{gone}, want: Archived},
		{name: "closed no payload", mode: config.ModeClosed, want: NeedLink, awaiting: true},
		{name: "open no payload", mode: config.ModeOpen, want: Created, created: true},
		{name: "closed valid ref", mode: config.ModeClosed, seed: []vpnctl.Customer{ref}, payload: "ref_" + codec.Encode(10), want: Created, created: true, referrer: ptr(10)},
		{name: "open valid ref", mode: config.ModeOpen, seed: []vpnctl.Customer{ref}, payload: "ref_" + codec.Encode(10), want: Created, created: true, referrer: ptr(10)},
		{name: "closed forged ref", mode: config.ModeClosed, seed: []vpnctl.Customer{ref}, payload: "ref_zzzzzz", want: BadReferral, awaiting: true},
		{name: "open forged ref", mode: config.ModeOpen, payload: "ref_zzzzzz", want: Created, created: true},
		{name: "closed ref to missing", mode: config.ModeClosed, payload: "ref_" + codec.Encode(999), want: BadReferral, awaiting: true},
		{name: "closed ref to archived", mode: config.ModeClosed, seed: []vpnctl.Customer{archivedRef}, payload: "ref_" + codec.Encode(11), want: BadReferral, awaiting: true},
		{name: "open ref to archived", mode: config.ModeOpen, seed: []vpnctl.Customer{archivedRef}, payload: "ref_" + codec.Encode(11), want: Created, created: true},
		{name: "closed unknown payload", mode: config.ModeClosed, payload: "hello", want: NeedLink, awaiting: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, st := setup(c.mode)
			for _, cu := range c.seed {
				f.Add(cu)
			}
			r, err := s.Start(context.Background(), ann, c.payload)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.want {
				t.Fatalf("outcome = %v, want %v", r.Outcome, c.want)
			}
			if got := len(f.Creates) == 1; got != c.created {
				t.Fatalf("created = %v, want %v", got, c.created)
			}
			if c.created {
				in := f.Creates[0]
				if *in.TelegramID != 100 || in.Name != "Ann" {
					t.Fatalf("create input %+v", in)
				}
				if (c.referrer == nil) != (in.ReferrerID == nil) || (c.referrer != nil && *c.referrer != *in.ReferrerID) {
					t.Fatalf("referrer = %v, want %v", in.ReferrerID, c.referrer)
				}
				if r.Customer == nil || r.Customer.ID == 0 {
					t.Fatal("created result without customer")
				}
			}
			if st[100] != c.awaiting {
				t.Fatalf("awaiting = %v, want %v", st[100], c.awaiting)
			}
		})
	}
}

func TestStartNameFallback(t *testing.T) {
	s, f, _ := setup(config.ModeOpen)
	s.Start(context.Background(), User{ID: 5}, "")
	if f.Creates[0].Name != "Telegram 5" {
		t.Fatalf("name = %q", f.Creates[0].Name)
	}
}

func TestStartUnavailable(t *testing.T) {
	s, f, _ := setup(config.ModeOpen)
	f.Err = vpnctl.ErrUnavailable
	if _, err := s.Start(context.Background(), ann, ""); !errors.Is(err, vpnctl.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func withSub(c vpnctl.Customer, short string) vpnctl.Customer {
	c.Subscriptions = []vpnctl.Subscription{{ID: 1, RwUser: &vpnctl.RwUser{ShortUUID: short}}}
	return c
}

func TestLink(t *testing.T) {
	for _, c := range []struct {
		name    string
		seed    vpnctl.Customer
		text    string
		want    Outcome
		updates int
	}{
		{"link free", withSub(vpnctl.Customer{ID: 1, Name: "X"}, "AbC123xyz"), " https://sub.x/AbC123xyz/ ", Linked, 1},
		{"already mine", withSub(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(100))}, "AbC123xyz"), "AbC123xyz", Linked, 0},
		{"someone else's", withSub(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(555))}, "AbC123xyz"), "AbC123xyz", Taken, 0},
		{"archived", withSub(vpnctl.Customer{ID: 1, Archived: true}, "AbC123xyz"), "AbC123xyz", Archived, 0},
		{"unknown", withSub(vpnctl.Customer{ID: 1}, "AbC123xyz"), "Zzz999zzz", NotFound, 0},
		{"garbage", withSub(vpnctl.Customer{ID: 1}, "AbC123xyz"), "hi!", BadLink, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, st := setup(config.ModeClosed)
			st[100] = true
			f.Add(c.seed)
			r, err := s.Link(context.Background(), ann, c.text)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.want || len(f.Updates) != c.updates {
				t.Fatalf("outcome=%v updates=%d", r.Outcome, len(f.Updates))
			}
			if c.want == Linked && (st[100] || r.Customer == nil || *r.Customer.TelegramID != 100) {
				t.Fatalf("linked: awaiting=%v customer=%+v", st[100], r.Customer)
			}
			if c.want != Linked && !st[100] {
				t.Fatal("must keep waiting for a link")
			}
		})
	}
}

func TestLinkKeepsFields(t *testing.T) {
	s, f, _ := setup(config.ModeClosed)
	f.Add(withSub(vpnctl.Customer{ID: 1, Name: "X", Contact: "@x", Notes: "vip", ReferrerID: ptr(7), ReferralPercent: ptr(15.0)}, "AbC123xyz"))
	if _, err := s.Link(context.Background(), ann, "AbC123xyz"); err != nil {
		t.Fatal(err)
	}
	in := f.Updates[0].In
	if in.Name != "X" || in.Contact != "@x" || in.Notes != "vip" || *in.ReferrerID != 7 || *in.ReferralPercent != 15 || *in.TelegramID != 100 {
		t.Fatalf("PUT body lost fields: %+v", in)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/access/` — Expected: FAIL.

- [ ] **Step 3: Implement** — `internal/access/access.go`

```go
// Package access decides who may use the bot and turns a Telegram user
// into a vpn-control customer (spec: "Access and onboarding").
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/sublink"
	"vpn-control-bot/internal/vpnctl"
)

type User struct {
	ID   int64
	Name string
}

type Outcome int

const (
	Member      Outcome = iota // known, active customer
	Created                    // new customer created in vpn-control
	NeedLink                   // closed mode: send your subscription link
	BadReferral                // closed mode: the referral link is invalid
	Archived                   // customer exists but is archived
	Linked                     // subscription link accepted, telegram id set
	BadLink                    // text is not a subscription link
	NotFound                   // no subscription with this link
	Taken                      // subscription belongs to another Telegram account
)

type Result struct {
	Outcome  Outcome
	Customer *vpnctl.Customer
}

type State interface {
	SetAwaitingLink(ctx context.Context, tgID int64, on bool) error
	AwaitingLink(ctx context.Context, tgID int64) (bool, error)
}

type Service struct {
	api   vpnctl.Client
	codec *refcode.Codec
	mode  config.Mode
	state State
}

func New(api vpnctl.Client, codec *refcode.Codec, mode config.Mode, state State) *Service {
	return &Service{api: api, codec: codec, mode: mode, state: state}
}

func (s *Service) AwaitingLink(ctx context.Context, tgID int64) (bool, error) {
	return s.state.AwaitingLink(ctx, tgID)
}

func (s *Service) Start(ctx context.Context, u User, payload string) (Result, error) {
	c, err := s.api.LookupByTelegram(ctx, u.ID)
	if err == nil {
		if c.Archived {
			return Result{Outcome: Archived, Customer: c}, nil
		}
		return Result{Outcome: Member, Customer: c}, nil
	}
	if !errors.Is(err, vpnctl.ErrNotFound) {
		return Result{}, err
	}

	var referrer *int
	if code, ok := strings.CutPrefix(strings.TrimSpace(payload), "ref_"); ok {
		id, ok, err := s.referrer(ctx, code)
		if err != nil {
			return Result{}, err
		}
		if ok {
			referrer = &id
		} else if s.mode == config.ModeClosed {
			return Result{Outcome: BadReferral}, s.state.SetAwaitingLink(ctx, u.ID, true)
		}
	}
	if referrer == nil && s.mode == config.ModeClosed {
		return Result{Outcome: NeedLink}, s.state.SetAwaitingLink(ctx, u.ID, true)
	}
	return s.create(ctx, u, referrer)
}

// referrer resolves a referral code to an active customer id.
func (s *Service) referrer(ctx context.Context, code string) (int, bool, error) {
	id, ok := s.codec.Decode(code)
	if !ok {
		return 0, false, nil
	}
	c, err := s.api.GetCustomer(ctx, id)
	if errors.Is(err, vpnctl.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, !c.Archived, nil
}

func (s *Service) create(ctx context.Context, u User, referrer *int) (Result, error) {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = fmt.Sprintf("Telegram %d", u.ID)
	}
	tg := u.ID
	id, err := s.api.CreateCustomer(ctx, vpnctl.CustomerInput{Name: name, TelegramID: &tg, ReferrerID: referrer})
	if err != nil {
		return Result{}, err
	}
	c, err := s.api.GetCustomer(ctx, id)
	if err != nil {
		return Result{}, err
	}
	return Result{Outcome: Created, Customer: c}, s.state.SetAwaitingLink(ctx, u.ID, false)
}

func (s *Service) Link(ctx context.Context, u User, text string) (Result, error) {
	short, ok := sublink.Parse(text)
	if !ok {
		return Result{Outcome: BadLink}, nil
	}
	c, err := s.api.LookupByShortUUID(ctx, short)
	if errors.Is(err, vpnctl.ErrNotFound) {
		return Result{Outcome: NotFound}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if c.TelegramID != nil && *c.TelegramID != u.ID {
		return Result{Outcome: Taken}, nil
	}
	if c.Archived {
		return Result{Outcome: Archived, Customer: c}, nil
	}
	if c.TelegramID == nil {
		// Full PUT: start from the current customer so nothing else is cleared.
		in := c.Input()
		tg := u.ID
		in.TelegramID = &tg
		if err := s.api.UpdateCustomer(ctx, c.ID, in); err != nil {
			return Result{}, err
		}
		c.TelegramID = &tg
	}
	return Result{Outcome: Linked, Customer: c}, s.state.SetAwaitingLink(ctx, u.ID, false)
}
```

- [ ] **Step 4: Run** `go test ./internal/access/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/access && git commit -m "feat: access policy and onboarding"`

---

### Task 8: Telegram view helpers and texts (`internal/telegram/view`)

**Files:**
- Create: `internal/telegram/view/screen.go`, `internal/telegram/view/format.go`, `internal/telegram/view/texts.go`, `internal/telegram/view/format_test.go`

**Interfaces:**
- Produces:
  - `type Screen struct{ Text string; Keyboard *models.InlineKeyboardMarkup }`
  - `type Links struct{ WebApp string }` with `func (l Links) Page(hash string) *models.WebAppInfo` (`hash` like `"/balance"` → `WebApp + "/#/balance"`)
  - `func Rub(v float64) string` (`1234.5` → `"1 234,50 ₽"`, `100` → `"100 ₽"`), `func Date(t *time.Time) string` (`"02.01.2006"`, nil → `"—"`, year ≥ 2090 → `"∞"`), `func Bytes(n int64) string` (`0` → `"∞"`, otherwise GiB/MiB with one decimal), `func Esc(s string) string` (= `html.EscapeString`).
  - Callback constants `CbHome="m:home"`, `CbSubs="m:subs"`, `CbBalance="m:bal"`, `CbRefs="m:ref"`, `CbTariffs="m:tar"`, prefixes `CbSub="s:"`, `CbTariff="t:"`.

- [ ] **Step 1: Write the failing test** — `internal/telegram/view/format_test.go`

```go
package view

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	for in, want := range map[float64]string{0: "0 ₽", 100: "100 ₽", 1234.5: "1 234,50 ₽", -50: "−50 ₽", 1000000: "1 000 000 ₽"} {
		if got := Rub(in); got != want {
			t.Errorf("Rub(%v) = %q, want %q", in, got, want)
		}
	}
	d := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	far := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if Date(&d) != "05.03.2026" || Date(nil) != "—" || Date(&far) != "∞" {
		t.Error("Date")
	}
	if Bytes(0) != "∞" || Bytes(50*1<<30) != "50 ГБ" || Bytes(1536*1<<20) != "1.5 ГБ" || Bytes(300*1<<20) != "300 МБ" {
		t.Errorf("Bytes: %q %q %q", Bytes(50*1<<30), Bytes(1536*1<<20), Bytes(300*1<<20))
	}
	if (Links{WebApp: "https://x/bot"}).Page("/balance").URL != "https://x/bot/#/balance" {
		t.Error("Page")
	}
}
```

- [ ] **Step 2: Run** `go get github.com/go-telegram/bot@v1.23.0 && go test ./internal/telegram/view/` — Expected: FAIL.

- [ ] **Step 3: Implement** — `internal/telegram/view/screen.go`

```go
// Package view renders bot screens (HTML text + inline keyboard) from data.
// It is pure: no network, so screens are covered by golden tests.
package view

import "github.com/go-telegram/bot/models"

type Screen struct {
	Text     string
	Keyboard *models.InlineKeyboardMarkup
}

type Links struct {
	WebApp string // PUBLIC_URL + WEBAPP_PATH
}

// Page opens a WebApp section, e.g. Page("/subscriptions/3").
func (l Links) Page(hash string) *models.WebAppInfo {
	return &models.WebAppInfo{URL: l.WebApp + "/#" + hash}
}

const (
	CbHome    = "m:home"
	CbSubs    = "m:subs"
	CbBalance = "m:bal"
	CbRefs    = "m:ref"
	CbTariffs = "m:tar"
	CbSub     = "s:" // + subscription id
	CbTariff  = "t:" // + tariff id
)

type btn = models.InlineKeyboardButton

func kb(rows ...[]btn) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func cb(text, data string) btn { return btn{Text: text, CallbackData: data} }

func back(to string) []btn { return []btn{cb(BtnBack, to)} }
```

`internal/telegram/view/format.go`:

```go
package view

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"
)

func Esc(s string) string { return html.EscapeString(s) }

// Rub formats rubles Russian-style: "1 234,50 ₽", whole amounts without kopecks.
func Rub(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "−", -v
	}
	cents := int64(math.Round(v * 100))
	whole := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if frac := cents % 100; frac != 0 {
		return fmt.Sprintf("%s%s,%02d ₽", sign, b.String(), frac)
	}
	return sign + b.String() + " ₽"
}

const unlimitedYear = 2090 // vpn-control's "never expires" dates

func Date(t *time.Time) string {
	switch {
	case t == nil:
		return "—"
	case t.Year() >= unlimitedYear:
		return "∞"
	}
	return t.Format("02.01.2006")
}

func Bytes(n int64) string {
	const gb, mb = 1 << 30, 1 << 20
	switch {
	case n <= 0:
		return "∞"
	case n >= gb:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/gb), ".0") + " ГБ"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/mb), ".0") + " МБ"
}
```

`internal/telegram/view/texts.go` (every Russian string in one place):

```go
package view

const (
	BtnBack     = "← Назад"
	BtnSubs     = "📦 Подписки"
	BtnBalance  = "💰 Баланс"
	BtnRefs     = "👥 Рефералы"
	BtnTariffs  = "🏷 Тарифы"
	BtnApp      = "🌐 Открыть приложение"
	BtnDetails  = "Подробнее в приложении"
	BtnHistory  = "📜 Вся история"
	BtnAllRefs  = "Все рефералы в приложении"
	BtnShare    = "📤 Поделиться ссылкой"
	BtnCopyLink = "📋 Скопировать ссылку"
	BtnPick     = "Выбрать"
	BtnHome     = "🏠 Главная"

	TxtNeedLink = "👋 Этот бот доступен только клиентам.\n\n" +
		"Отправьте ссылку на вашу подписку (её можно скопировать в VPN-приложении) — " +
		"и я найду ваш аккаунт.\n\nЕсли вас пригласили, откройте бота по реферальной ссылке."
	TxtBadReferral = "⚠️ Реферальная ссылка недействительна.\n\nПопросите пригласившего прислать её ещё раз " +
		"или отправьте ссылку на вашу подписку."
	TxtBadLink  = "🤔 Не похоже на ссылку подписки. Пришлите ссылку целиком, например <code>https://sub.example.com/AbC123xyz</code>."
	TxtNotFound = "🔍 Подписка с такой ссылкой не найдена. Проверьте ссылку и попробуйте ещё раз."
	TxtTaken    = "🔒 Эта подписка уже привязана к другому Telegram-аккаунту. Если это ошибка — напишите в поддержку."
	TxtArchived = "⏸ Доступ к аккаунту приостановлен. Напишите в поддержку, если это ошибка."
	TxtDown     = "😔 Сервис временно недоступен. Попробуйте чуть позже."
	TxtSoon     = "Оплата прямо в боте скоро появится 🙌"
	TxtLinked   = "✅ Подписка привязана к вашему Telegram!"
	TxtWelcome  = "🎉 Добро пожаловать!"
	TxtHelp     = "Я показываю ваши подписки, баланс и приглашённых друзей.\n\n" +
		"/menu — главное меню\n/start — начать заново\n\n" +
		"Полная информация — в приложении: кнопка «Открыть» рядом с полем ввода."
)
```

- [ ] **Step 4: Run** `go test ./internal/telegram/view/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add go.mod go.sum internal/telegram/view && git commit -m "feat: bot view formatting and texts"`

---

### Task 9: Bot screens with golden tests

**Files:**
- Create: `internal/telegram/view/screens.go`, `internal/telegram/view/screens_test.go`, `internal/telegram/view/testdata/*.golden` (generated)

**Interfaces:**
- Consumes: `profile.Profile`, `profile.Tariff`, `profile.Expiry`, helpers from Task 8.
- Produces: `func Home(p profile.Profile, l Links, now time.Time) Screen`, `func Subscriptions(p profile.Profile, now time.Time) Screen`, `func Subscription(p profile.Profile, id int, l Links, now time.Time) (Screen, bool)`, `func Balance(p profile.Profile, l Links) Screen`, `func Referrals(p profile.Profile, l Links) Screen`, `func Tariffs(ts []profile.Tariff, l Links) Screen`, `func Notice(text string) Screen` (text + "🏠 Главная" button), `func Plain(text string) Screen` (no keyboard).

- [ ] **Step 1: Write the failing test** — `internal/telegram/view/screens_test.go`

```go
package view

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/profile"
)

var update = flag.Bool("update", false, "rewrite golden files")

var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func fixture() profile.Profile {
	return profile.Profile{
		ID: 1, Name: "Ann", Balance: 1234.5, BalanceOwn: 1000, BalanceReferral: 234.5, Monthly: 300,
		NearestExpireAt: ptr(now.Add(48 * time.Hour)),
		Subscriptions: []profile.Subscription{{
			ID: 3, Title: "Основная", Tariff: "Стандарт", Price: 250, Status: "ACTIVE", ExpireAt: ptr(now.Add(48 * time.Hour)),
			UsedBytes: 10 << 30, LimitBytes: 100 << 30, DeviceLimit: ptr(3), URL: "https://sub.x/AbC123xyz",
			Addons: []profile.Addon{{ID: 9, Name: "YouTube", Tariff: "YT", Price: 50, Status: "ACTIVE", ExpireAt: ptr(now.AddDate(0, 1, 0))}},
		}},
		Ledger: []profile.LedgerEntry{
			{Type: "charge", Amount: -250, Date: now.AddDate(0, 0, -1)},
			{Type: "payment", Amount: 500, Date: now.AddDate(0, 0, -2)},
		},
		Referrals:       []profile.Referral{{Name: "Bob", Active: true, Monthly: 250, TotalPaid: 750, Earned: 75}},
		ReferralsEarned: 75,
		RefLink:         "https://t.me/vpnbot?start=ref_Ab12cd",
	}
}

var links = Links{WebApp: "https://x.dev/bot"}

func render(s Screen) string {
	var b strings.Builder
	b.WriteString(s.Text)
	b.WriteString("\n---\n")
	if s.Keyboard != nil {
		for _, row := range s.Keyboard.InlineKeyboard {
			for i, k := range row {
				if i > 0 {
					b.WriteString(" | ")
				}
				switch {
				case k.WebApp != nil:
					fmt.Fprintf(&b, "[%s → app %s]", k.Text, k.WebApp.URL)
				case k.URL != "":
					fmt.Fprintf(&b, "[%s → %s]", k.Text, k.URL)
				case k.CopyText != nil:
					fmt.Fprintf(&b, "[%s ⧉ %s]", k.Text, k.CopyText.Text)
				default:
					fmt.Fprintf(&b, "[%s · %s]", k.Text, k.CallbackData)
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func golden(t *testing.T, name string, s Screen) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got := render(s)
	if *update {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(path, []byte(got), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestScreens(t *testing.T) {
	p := fixture()
	golden(t, "home", Home(p, links, now))
	golden(t, "subscriptions", Subscriptions(p, now))
	sub, ok := Subscription(p, 3, links, now)
	if !ok {
		t.Fatal("subscription 3 not found")
	}
	golden(t, "subscription", sub)
	golden(t, "balance", Balance(p, links))
	golden(t, "referrals", Referrals(p, links))
	golden(t, "tariffs", Tariffs([]profile.Tariff{
		{ID: 1, Kind: "base", Name: "Стандарт", Description: "Все страны", MonthlyPrice: 250, TrafficLimitBytes: 100 << 30, HwidLimit: ptr(3)},
		{ID: 2, Kind: "addon", Name: "YT", AddonName: "YouTube", MonthlyPrice: 50},
	}, links))
}

func TestEmptyStates(t *testing.T) {
	p := profile.Profile{Name: "New", RefLink: "https://t.me/vpnbot?start=ref_x"}
	golden(t, "home_empty", Home(p, links, now))
	golden(t, "subscriptions_empty", Subscriptions(p, now))
	golden(t, "referrals_empty", Referrals(p, links))
	if _, ok := Subscription(p, 1, links, now); ok {
		t.Error("missing subscription reported as found")
	}
}

func TestHomeEscapesName(t *testing.T) {
	p := fixture()
	p.Name = "<b>A&B</b>"
	if txt := Home(p, links, now).Text; !strings.Contains(txt, "&lt;b&gt;A&amp;B&lt;/b&gt;") {
		t.Fatalf("name not escaped: %s", txt)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/telegram/view/` — Expected: FAIL (`undefined: Home`).

- [ ] **Step 3: Implement** — `internal/telegram/view/screens.go`

```go
package view

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vpn-control-bot/internal/profile"
)

func stateIcon(s profile.State) string {
	switch s {
	case profile.StateActive:
		return "🟢"
	case profile.StateSoon:
		return "🟡"
	case profile.StateExpired:
		return "🔴"
	}
	return "⚪️"
}

func until(exp *time.Time, unlimited bool) string {
	if unlimited {
		return "∞"
	}
	return Date(exp)
}

func Home(p profile.Profile, l Links, now time.Time) Screen {
	var b strings.Builder
	fmt.Fprintf(&b, "👋 <b>%s</b>\n\n", Esc(p.Name))
	fmt.Fprintf(&b, "💰 Баланс: <b>%s</b>\n", Rub(p.Balance))
	if len(p.Subscriptions) == 0 {
		b.WriteString("📦 Подписок пока нет — загляните в «Тарифы».")
	} else {
		fmt.Fprintf(&b, "📦 Подписок: <b>%d</b>", len(p.Subscriptions))
		if p.Monthly > 0 {
			fmt.Fprintf(&b, " · %s/мес", Rub(p.Monthly))
		}
		if p.NearestExpireAt != nil {
			fmt.Fprintf(&b, "\n%s Ближайшее окончание: <b>%s</b>", stateIcon(profile.Expiry(p.NearestExpireAt, false, now)), Date(p.NearestExpireAt))
		}
	}
	return Screen{Text: b.String(), Keyboard: kb(
		[]btn{cb(BtnSubs, CbSubs), cb(BtnBalance, CbBalance)},
		[]btn{cb(BtnRefs, CbRefs), cb(BtnTariffs, CbTariffs)},
		[]btn{{Text: BtnApp, WebApp: l.Page("/")}},
	)}
}

func Subscriptions(p profile.Profile, now time.Time) Screen {
	if len(p.Subscriptions) == 0 {
		return Screen{Text: "📦 <b>Подписки</b>\n\nУ вас пока нет подписок. Выберите тариф — и она появится здесь.",
			Keyboard: kb([]btn{cb(BtnTariffs, CbTariffs)}, back(CbHome))}
	}
	var b strings.Builder
	b.WriteString("📦 <b>Подписки</b>\n")
	rows := [][]btn{}
	for _, s := range p.Subscriptions {
		icon := stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now))
		fmt.Fprintf(&b, "\n%s <b>%s</b> — до %s", icon, Esc(s.Title), until(s.ExpireAt, s.Unlimited))
		if n := len(s.Addons); n > 0 {
			fmt.Fprintf(&b, " · аддонов: %d", n)
		}
		rows = append(rows, []btn{cb(icon+" "+s.Title, CbSub+strconv.Itoa(s.ID))})
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(CbHome))...)}
}

func Subscription(p profile.Profile, id int, l Links, now time.Time) (Screen, bool) {
	for _, s := range p.Subscriptions {
		if s.ID != id {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s <b>%s</b>\n\n", stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now)), Esc(s.Title))
		if s.Tariff != "" {
			fmt.Fprintf(&b, "🏷 Тариф: %s · %s/мес\n", Esc(s.Tariff), Rub(s.Price))
		}
		fmt.Fprintf(&b, "📅 Действует до: <b>%s</b>\n", until(s.ExpireAt, s.Unlimited))
		fmt.Fprintf(&b, "📊 Трафик: %s из %s\n", Bytes(s.UsedBytes), Bytes(s.LimitBytes))
		if s.DeviceLimit != nil {
			fmt.Fprintf(&b, "📱 Устройств: до %d\n", *s.DeviceLimit)
		}
		if len(s.Addons) > 0 {
			b.WriteString("\n<b>Аддоны</b>\n")
			for _, a := range s.Addons {
				fmt.Fprintf(&b, "%s %s — до %s", stateIcon(profile.Expiry(a.ExpireAt, a.Unlimited, now)), Esc(a.Name), until(a.ExpireAt, a.Unlimited))
				if a.Included {
					b.WriteString(" (входит в тариф)")
				}
				b.WriteString("\n")
			}
		}
		rows := [][]btn{}
		if s.URL != "" {
			fmt.Fprintf(&b, "\n🔗 <code>%s</code>", Esc(s.URL))
			rows = append(rows, []btn{{Text: BtnCopyLink, CopyText: &models.CopyTextButton{Text: s.URL}}})
		}
		rows = append(rows, []btn{{Text: BtnDetails, WebApp: l.Page("/subscriptions/" + strconv.Itoa(s.ID))}}, back(CbSubs))
		return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}, true
	}
	return Screen{}, false
}

var ledgerTypes = map[string]string{
	"payment": "Пополнение", "charge": "Списание", "adjustment": "Корректировка", "refund": "Возврат", "referral": "Реферальное начисление",
}

func Balance(p profile.Profile, l Links) Screen {
	var b strings.Builder
	fmt.Fprintf(&b, "💰 <b>Баланс: %s</b>\n", Rub(p.Balance))
	if p.BalanceReferral != 0 {
		fmt.Fprintf(&b, "из них реферальных: %s\n", Rub(p.BalanceReferral))
	}
	if p.Monthly > 0 {
		fmt.Fprintf(&b, "Ежемесячно: %s\n", Rub(p.Monthly))
	}
	if len(p.Ledger) == 0 {
		b.WriteString("\nОпераций пока не было.")
	} else {
		b.WriteString("\n<b>Последние операции</b>\n")
		for i, e := range p.Ledger {
			if i == 5 {
				break
			}
			name := ledgerTypes[e.Type]
			if name == "" {
				name = e.Type
			}
			d := e.Date
			fmt.Fprintf(&b, "%s · %s · <b>%s</b>\n", Date(&d), name, Rub(e.Amount))
		}
	}
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: BtnHistory, WebApp: l.Page("/balance")}},
		back(CbHome),
	)}
}

func Referrals(p profile.Profile, l Links) Screen {
	var b strings.Builder
	b.WriteString("👥 <b>Рефералы</b>\n\n")
	fmt.Fprintf(&b, "Ваша ссылка:\n<code>%s</code>\n\n", Esc(p.RefLink))
	if len(p.Referrals) == 0 {
		b.WriteString("Пока никого. Поделитесь ссылкой — за оплаты приглашённых вы получаете процент на баланс.")
	} else {
		fmt.Fprintf(&b, "Приглашено: <b>%d</b> · заработано: <b>%s</b>\n\n<b>Топ по доходу</b>\n", len(p.Referrals), Rub(p.ReferralsEarned))
		for i, r := range p.Referrals {
			if i == 5 {
				break
			}
			icon := "🟢"
			if !r.Active {
				icon = "⚪️"
			}
			fmt.Fprintf(&b, "%s %s — %s\n", icon, Esc(r.Name), Rub(r.Earned))
		}
	}
	share := "https://t.me/share/url?url=" + url.QueryEscape(p.RefLink) + "&text=" + url.QueryEscape("Подключайся к VPN по моей ссылке")
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: BtnShare, URL: share}},
		[]btn{{Text: BtnCopyLink, CopyText: &models.CopyTextButton{Text: p.RefLink}}},
		[]btn{{Text: BtnAllRefs, WebApp: l.Page("/referrals")}},
		back(CbHome),
	)}
}

func Tariffs(ts []profile.Tariff, l Links) Screen {
	if len(ts) == 0 {
		return Screen{Text: "🏷 <b>Тарифы</b>\n\nСейчас нет доступных тарифов.", Keyboard: kb(back(CbHome))}
	}
	var b strings.Builder
	b.WriteString("🏷 <b>Тарифы</b>\n")
	rows := [][]btn{}
	for _, t := range ts {
		name := t.Name
		if t.Kind == "addon" && t.AddonName != "" {
			name = t.AddonName + " · " + t.Name
		}
		fmt.Fprintf(&b, "\n<b>%s</b> — %s/мес", Esc(name), Rub(t.MonthlyPrice))
		if t.Description != "" {
			fmt.Fprintf(&b, "\n%s", Esc(t.Description))
		}
		var limits []string
		if t.TrafficLimitBytes > 0 {
			limits = append(limits, "трафик "+Bytes(t.TrafficLimitBytes))
		}
		if t.HwidLimit != nil {
			limits = append(limits, fmt.Sprintf("до %d устройств", *t.HwidLimit))
		}
		if len(limits) > 0 {
			fmt.Fprintf(&b, "\n<i>%s</i>", strings.Join(limits, " · "))
		}
		b.WriteString("\n")
		rows = append(rows, []btn{cb(BtnPick+": "+name, CbTariff+strconv.Itoa(t.ID))})
	}
	rows = append(rows, []btn{{Text: BtnApp, WebApp: l.Page("/tariffs")}}, back(CbHome))
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}
}

func Notice(text string) Screen { return Screen{Text: text, Keyboard: kb([]btn{cb(BtnHome, CbHome)})} }

func Plain(text string) Screen { return Screen{Text: text} }
```

Add `"github.com/go-telegram/bot/models"` to the imports of `screens.go`.

- [ ] **Step 4: Generate goldens** `go test ./internal/telegram/view/ -run 'TestScreens|TestEmptyStates' -update` — then **read every `testdata/*.golden` file** and check: Russian text reads naturally, prices like `1 234,50 ₽`, status icons match the fixture (home shows 🟡 for an expiry 48h away), every keyboard ends with a back/home button, WebApp URLs look like `https://x.dev/bot/#/subscriptions/3`. Fix `screens.go` if anything is off and regenerate.

- [ ] **Step 5: Run** `go test ./internal/telegram/view/` — Expected: PASS.

- [ ] **Step 6: Commit** `git add internal/telegram/view && git commit -m "feat: bot screens with golden tests"`

---

### Task 10: Telegram adapter (`internal/telegram`)

**Files:**
- Create: `internal/telegram/bot.go`, `internal/telegram/route.go`, `internal/telegram/route_test.go`

**Interfaces:**
- Consumes: `access.Service` (`Start`, `Link`, `AwaitingLink`), `vpnctl.Client.ListTariffs`, `refcode.Codec.Encode`, `billing.Billing`, `profile.Build/Tariffs`, `view.*`.
- Produces:
  - `type Deps struct{ Access *access.Service; API vpnctl.Client; Codec *refcode.Codec; Billing billing.Billing; WebAppURL string; Log *slog.Logger }`
  - `func New(ctx context.Context, token string, d Deps) (*Bot, error)` — creates the go-telegram bot, calls `GetMe`, registers handlers, sets commands and the WebApp menu button.
  - `(*Bot).Run(ctx)` (blocks until ctx is done), `(*Bot).Username() string`.
  - `func RefLink(username string, codec *refcode.Codec, customerID int) string` = `https://t.me/<username>?start=ref_<code>` (also used by the WebApp).
  - `func parseRoute(data string) (route, bool)` (internal).

- [ ] **Step 1: Write the failing test** — `internal/telegram/route_test.go`

```go
package telegram

import (
	"testing"

	"vpn-control-bot/internal/refcode"
)

func TestParseRoute(t *testing.T) {
	cases := map[string]route{
		"m:home": {kind: "m:home"}, "m:subs": {kind: "m:subs"}, "m:bal": {kind: "m:bal"},
		"m:ref": {kind: "m:ref"}, "m:tar": {kind: "m:tar"},
		"s:12": {kind: "s:", id: 12}, "t:3": {kind: "t:", id: 3},
	}
	for in, want := range cases {
		got, ok := parseRoute(in)
		if !ok || got != want {
			t.Errorf("parseRoute(%q) = %+v,%v", in, got, ok)
		}
	}
	for _, in := range []string{"", "x", "s:", "s:abc", "s:-1", "m:nope"} {
		if _, ok := parseRoute(in); ok {
			t.Errorf("parseRoute(%q) accepted", in)
		}
	}
}

func TestRefLink(t *testing.T) {
	c := refcode.New("s")
	if got := RefLink("vpnbot", c, 5); got != "https://t.me/vpnbot?start=ref_"+c.Encode(5) {
		t.Fatal(got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/telegram/` — Expected: FAIL.

- [ ] **Step 3: Implement routes** — `internal/telegram/route.go`

```go
package telegram

import (
	"strconv"
	"strings"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram/view"
)

type route struct {
	kind string // a view.Cb* constant
	id   int
}

func parseRoute(data string) (route, bool) {
	switch data {
	case view.CbHome, view.CbSubs, view.CbBalance, view.CbRefs, view.CbTariffs:
		return route{kind: data}, true
	}
	for _, p := range []string{view.CbSub, view.CbTariff} {
		if rest, ok := strings.CutPrefix(data, p); ok {
			id, err := strconv.Atoi(rest)
			if err != nil || id <= 0 {
				return route{}, false
			}
			return route{kind: p, id: id}, true
		}
	}
	return route{}, false
}

func RefLink(username string, codec *refcode.Codec, customerID int) string {
	return "https://t.me/" + username + "?start=ref_" + codec.Encode(customerID)
}
```

- [ ] **Step 4: Run** `go test ./internal/telegram/` — Expected: PASS.

- [ ] **Step 5: Implement the adapter** — `internal/telegram/bot.go`

```go
// Package telegram connects go-telegram/bot updates to access + view.
package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/profile"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram/view"
	"vpn-control-bot/internal/vpnctl"
)

type Deps struct {
	Access    *access.Service
	API       vpnctl.Client
	Codec     *refcode.Codec
	Billing   billing.Billing
	WebAppURL string
	Log       *slog.Logger
}

type Bot struct {
	Deps
	tg       *tgbot.Bot
	username string
	links    view.Links
}

func New(ctx context.Context, token string, d Deps) (*Bot, error) {
	b := &Bot{Deps: d, links: view.Links{WebApp: d.WebAppURL}}
	tg, err := tgbot.New(token,
		tgbot.WithDefaultHandler(b.onText),
		tgbot.WithErrorsHandler(func(err error) { d.Log.Error("telegram", "err", err) }),
	)
	if err != nil {
		return nil, err
	}
	b.tg = tg
	me, err := tg.GetMe(ctx)
	if err != nil {
		return nil, err
	}
	b.username = me.Username

	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand, b.onStart)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "menu", tgbot.MatchTypeCommand, b.onMenu)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "help", tgbot.MatchTypeCommand, b.onHelp)
	for _, p := range []string{"m:", view.CbSub, view.CbTariff} {
		tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, p, tgbot.MatchTypePrefix, b.onCallback)
	}

	if _, err := tg.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: []models.BotCommand{
		{Command: "menu", Description: "Главное меню"},
		{Command: "start", Description: "Начать заново"},
		{Command: "help", Description: "Помощь"},
	}}); err != nil {
		d.Log.Warn("set commands", "err", err)
	}
	if _, err := tg.SetChatMenuButton(ctx, &tgbot.SetChatMenuButtonParams{MenuButton: models.MenuButtonWebApp{
		Type: models.MenuButtonTypeWebApp, Text: "Открыть", WebApp: models.WebAppInfo{URL: d.WebAppURL + "/"},
	}}); err != nil {
		d.Log.Warn("set menu button", "err", err)
	}
	return b, nil
}

func (b *Bot) Username() string      { return b.username }
func (b *Bot) Run(ctx context.Context) { b.tg.Start(ctx) }

func user(u *models.User) access.User {
	return access.User{ID: u.ID, Name: strings.TrimSpace(u.FirstName + " " + u.LastName)}
}

// show edits msgID in place, or sends a new message when msgID is 0.
func (b *Bot) show(ctx context.Context, chatID int64, msgID int, s view.Screen) {
	var markup models.ReplyMarkup
	if s.Keyboard != nil {
		markup = s.Keyboard
	}
	if msgID != 0 {
		_, err := b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID: chatID, MessageID: msgID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup,
		})
		if err == nil || strings.Contains(err.Error(), "message is not modified") {
			return
		}
		b.Log.Warn("edit message, sending new", "err", err)
	}
	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup,
	}); err != nil {
		b.Log.Error("send message", "err", err)
	}
}

// outcome turns a non-member access result into a screen. ok=false means
// the user is a member and the caller should render the requested page.
func (b *Bot) outcome(r access.Result, err error) (view.Screen, bool) {
	if err != nil {
		b.Log.Error("access", "err", err)
		return view.Plain(view.TxtDown), true
	}
	switch r.Outcome {
	case access.NeedLink:
		return view.Plain(view.TxtNeedLink), true
	case access.BadReferral:
		return view.Plain(view.TxtBadReferral), true
	case access.Archived:
		return view.Plain(view.TxtArchived), true
	case access.BadLink:
		return view.Plain(view.TxtBadLink), true
	case access.NotFound:
		return view.Plain(view.TxtNotFound), true
	case access.Taken:
		return view.Plain(view.TxtTaken), true
	}
	return view.Screen{}, false
}

func (b *Bot) profile(c *vpnctl.Customer) profile.Profile {
	return profile.Build(c, RefLink(b.username, b.Codec, c.ID))
}

func (b *Bot) home(r access.Result, prefix string) view.Screen {
	s := view.Home(b.profile(r.Customer), b.links, time.Now())
	if prefix != "" {
		s.Text = prefix + "\n\n" + s.Text
	}
	return s
}

func (b *Bot) onStart(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	payload := strings.TrimSpace(strings.TrimPrefix(m.Text, "/start"))
	r, err := b.Access.Start(ctx, user(m.From), payload)
	if s, done := b.outcome(r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	prefix := ""
	if r.Outcome == access.Created {
		prefix = view.TxtWelcome
	}
	b.show(ctx, m.Chat.ID, 0, b.home(r, prefix))
}

func (b *Bot) onMenu(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	r, err := b.Access.Start(ctx, user(m.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	b.show(ctx, m.Chat.ID, 0, b.home(r, ""))
}

func (b *Bot) onHelp(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	b.show(ctx, u.Message.Chat.ID, 0, view.Notice(view.TxtHelp))
}

// onText handles any other message: a subscription link while we wait for
// one, otherwise it just brings the menu back.
func (b *Bot) onText(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m == nil || m.From == nil || m.Chat.Type != models.ChatTypePrivate {
		return
	}
	who := user(m.From)
	waiting, err := b.Access.AwaitingLink(ctx, who.ID)
	if err != nil {
		b.Log.Error("state", "err", err)
	}
	if waiting {
		r, err := b.Access.Link(ctx, who, m.Text)
		if s, done := b.outcome(r, err); done {
			b.show(ctx, m.Chat.ID, 0, s)
			return
		}
		b.show(ctx, m.Chat.ID, 0, b.home(r, view.TxtLinked))
		return
	}
	b.onMenu(ctx, b.tg, u)
}

func (b *Bot) onCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()

	if q.Message.Message == nil {
		return // too old to edit
	}
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	rt, ok := parseRoute(q.Data)
	if !ok {
		return
	}
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	p := b.profile(r.Customer)
	now := time.Now()
	switch rt.kind {
	case view.CbHome:
		b.show(ctx, chatID, msgID, view.Home(p, b.links, now))
	case view.CbSubs:
		b.show(ctx, chatID, msgID, view.Subscriptions(p, now))
	case view.CbSub:
		s, found := view.Subscription(p, rt.id, b.links, now)
		if !found {
			s = view.Subscriptions(p, now)
		}
		b.show(ctx, chatID, msgID, s)
	case view.CbBalance:
		b.show(ctx, chatID, msgID, view.Balance(p, b.links))
	case view.CbRefs:
		b.show(ctx, chatID, msgID, view.Referrals(p, b.links))
	case view.CbTariffs:
		ts, err := b.API.ListTariffs(ctx)
		if err != nil {
			b.Log.Error("tariffs", "err", err)
			b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
			return
		}
		b.show(ctx, chatID, msgID, view.Tariffs(profile.Tariffs(ts), b.links))
	case view.CbTariff:
		_, err := b.Billing.Quote(ctx, r.Customer.ID, billing.Item{Kind: "tariff", ID: rt.id})
		if errors.Is(err, billing.ErrNotImplemented) {
			answer.Text, answer.ShowAlert = view.TxtSoon, true
		}
	}
}
```

- [ ] **Step 6: Run** `go vet ./... && go test ./...` — Expected: no vet errors; all PASS.

- [ ] **Step 7: Commit** `git add internal/telegram && git commit -m "feat: telegram bot adapter"`

---

### Task 11: WebApp initData validation (`internal/webapp/initdata`)

**Files:**
- Create: `internal/webapp/initdata/initdata.go`, `internal/webapp/initdata/initdata_test.go`

**Interfaces:**
- Produces: `type User struct{ ID int64; FirstName, LastName, Username string }`, `var ErrInvalid, ErrExpired error`, `func Validate(raw, botToken string, maxAge time.Duration, now time.Time) (User, error)`, `func Sign(v url.Values, botToken string) string` (adds `hash`, returns the encoded query — used by tests and the dev mock).

- [ ] **Step 1: Write the failing test**

```go
package initdata

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func signed(authDate time.Time) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))
	v.Set("query_id", "AAH")
	v.Set("user", `{"id":42,"first_name":"Ann","username":"ann"}`)
	return Sign(v, "123:ABC")
}

func TestValid(t *testing.T) {
	u, err := Validate(signed(now.Add(-time.Minute)), "123:ABC", 24*time.Hour, now)
	if err != nil || u.ID != 42 || u.FirstName != "Ann" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
}

func TestRejects(t *testing.T) {
	good := signed(now.Add(-time.Minute))
	cases := map[string]struct {
		raw, token string
		want       error
	}{
		"other token": {good, "999:XYZ", ErrInvalid},
		"tampered":    {strings.Replace(good, "Ann", "Bob", 1), "123:ABC", ErrInvalid},
		"no hash":     {"auth_date=1&user=%7B%7D", "123:ABC", ErrInvalid},
		"garbage":     {"%%%", "123:ABC", ErrInvalid},
		"expired":     {signed(now.Add(-25 * time.Hour)), "123:ABC", ErrExpired},
	}
	for name, c := range cases {
		if _, err := Validate(c.raw, c.token, 24*time.Hour, now); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/webapp/initdata/` — Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// Package initdata validates Telegram WebApp initData
// (https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app).
package initdata

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid = errors.New("initdata: invalid")
	ErrExpired = errors.New("initdata: expired")
)

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func Validate(raw, botToken string, maxAge time.Duration, now time.Time) (User, error) {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return User{}, ErrInvalid
	}
	hash := v.Get("hash")
	if hash == "" || !hmac.Equal([]byte(hash), []byte(sum(v, botToken))) {
		return User{}, ErrInvalid
	}
	auth, err := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	if err != nil {
		return User{}, ErrInvalid
	}
	if now.Sub(time.Unix(auth, 0)) > maxAge {
		return User{}, ErrExpired
	}
	var u User
	if err := json.Unmarshal([]byte(v.Get("user")), &u); err != nil || u.ID == 0 {
		return User{}, ErrInvalid
	}
	return u, nil
}

// Sign returns v encoded with a valid hash for botToken.
func Sign(v url.Values, botToken string) string {
	v.Del("hash")
	v.Set("hash", sum(v, botToken))
	return v.Encode()
}

// sum is hex(HMAC_SHA256(HMAC_SHA256("WebAppData", token), data_check_string)).
func sum(v url.Values, botToken string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + v.Get(k)
	}
	secret := mac([]byte("WebAppData"), []byte(botToken))
	return hex.EncodeToString(mac(secret, []byte(strings.Join(lines, "\n"))))
}

func mac(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}
```

- [ ] **Step 4: Run** `go test ./internal/webapp/initdata/` — Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/webapp/initdata && git commit -m "feat: telegram initData validation"`

---

### Task 12: WebApp HTTP server (`internal/webapp`) and `web/embed.go`

**Files:**
- Create: `internal/webapp/server.go`, `internal/webapp/server_test.go`, `web/embed.go`, `web/dist/.gitkeep`

**Interfaces:**
- Consumes: `initdata.Validate`, `vpnctl.Client`, `profile.Build/Tariffs`, `telegram.RefLink`, `refcode.Codec`.
- Produces: `type Deps struct{ API vpnctl.Client; Codec *refcode.Codec; BotToken, BotUsername, Path string; Static fs.FS; Log *slog.Logger }`, `func New(d Deps) *fiber.App`; `web.Dist fs.FS`.
- HTTP contract (used by the frontend, Task 13):
  - `GET {path}/api/me` with `Authorization: tma <initData>` → `200 profile.Profile`; `401 {"code":"unauthorized"}`; `403 {"code":"no_access"}` (no/archived customer); `503 {"code":"unavailable"}`.
  - `GET {path}/api/tariffs` → `200 []profile.Tariff` (same auth).
  - `GET {path}` → `301` to `{path}/`; `GET {path}/` → `index.html`; other files from `Static`.

- [ ] **Step 1: Embed** — `web/embed.go` (and `touch web/dist/.gitkeep`)

```go
// Package web embeds the WebApp build (web/dist). Run `bun run build` in
// web/ first; with only .gitkeep the Go binary still builds.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

var Dist = func() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()
```

- [ ] **Step 2: Write the failing test** — `internal/webapp/server_test.go`

```go
package webapp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp/initdata"
)

const token = "123:ABC"

func ptr[T any](v T) *T { return &v }

func app(f *vpnctl.Fake) *Deps {
	return &Deps{
		API: f, Codec: refcode.New("s"), BotToken: token, BotUsername: "vpnbot", Path: "/bot",
		Static: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/a.js": {Data: []byte("js")}},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func tma(id int64) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	v.Set("user", `{"id":`+strconv.FormatInt(id, 10)+`,"first_name":"Ann"}`)
	return "tma " + initdata.Sign(v, token)
}

func get(t *testing.T, d *Deps, path, auth string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMe(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, Name: "Ann", TelegramID: ptr(int64(42)), Notes: "SECRET", Balance: 10})
	d := app(f)

	if code, _ := get(t, d, "/bot/api/me", ""); code != 401 {
		t.Fatalf("no auth: %d", code)
	}
	if code, _ := get(t, d, "/bot/api/me", "tma garbage"); code != 401 {
		t.Fatalf("bad auth: %d", code)
	}
	if code, _ := get(t, d, "/bot/api/me", tma(7)); code != 403 {
		t.Fatalf("unknown user: %d", code)
	}
	code, body := get(t, d, "/bot/api/me", tma(42))
	if code != 200 || strings.Contains(body, "SECRET") {
		t.Fatalf("me: %d %s", code, body)
	}
	var p struct {
		Name    string `json:"name"`
		RefLink string `json:"ref_link"`
	}
	json.Unmarshal([]byte(body), &p)
	if p.Name != "Ann" || !strings.HasPrefix(p.RefLink, "https://t.me/vpnbot?start=ref_") {
		t.Fatalf("profile %+v", p)
	}
}

func TestArchivedAndDown(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(42)), Archived: true})
	d := app(f)
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 403 {
		t.Fatalf("archived: %d", code)
	}
	f.Err = vpnctl.ErrUnavailable
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 503 {
		t.Fatalf("down: %d", code)
	}
}

func TestTariffsAndStatic(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(42))})
	f.Tariffs = []vpnctl.Tariff{{ID: 1, Kind: "base", Name: "Std", Active: true}, {ID: 2, Name: "Off"}}
	d := app(f)
	code, body := get(t, d, "/bot/api/tariffs", tma(42))
	if code != 200 || !strings.Contains(body, "Std") || strings.Contains(body, "Off") {
		t.Fatalf("tariffs: %d %s", code, body)
	}
	if code, _ := get(t, d, "/bot", ""); code != 301 {
		t.Fatalf("/bot redirect: %d", code)
	}
	if code, body := get(t, d, "/bot/", ""); code != 200 || !strings.Contains(body, "app") {
		t.Fatalf("index: %d %s", code, body)
	}
	if code, body := get(t, d, "/bot/assets/a.js", ""); code != 200 || body != "js" {
		t.Fatalf("asset: %d %s", code, body)
	}
	if code, _ := get(t, d, "/other", ""); code != 404 {
		t.Fatalf("outside path: %d", code)
	}
}
```

- [ ] **Step 3: Run** `go get github.com/gofiber/fiber/v2@latest && go test ./internal/webapp/` — Expected: FAIL.

- [ ] **Step 4: Implement** — `internal/webapp/server.go`

```go
// Package webapp serves the Telegram WebApp: its static files and a small
// JSON API authenticated by Telegram initData.
package webapp

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	"vpn-control-bot/internal/profile"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp/initdata"
)

const maxInitDataAge = 24 * time.Hour

type Deps struct {
	API         vpnctl.Client
	Codec       *refcode.Codec
	BotToken    string
	BotUsername string
	Path        string // "" or "/bot"
	Static      fs.FS
	Log         *slog.Logger
}

func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true, StrictRouting: true})
	s := &server{Deps: d}

	api := app.Group(d.Path + "/api")
	api.Use(s.auth)
	api.Get("/me", s.me)
	api.Get("/tariffs", s.tariffs)

	if d.Path != "" {
		app.Get(d.Path, func(c *fiber.Ctx) error { return c.Redirect(d.Path+"/", fiber.StatusMovedPermanently) })
	}
	app.Use(d.Path+"/", filesystem.New(filesystem.Config{Root: http.FS(d.Static), Index: "index.html"}))
	return app
}

type server struct{ Deps }

func fail(c *fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{"code": code})
}

// auth resolves the caller to their vpn-control customer.
func (s *server) auth(c *fiber.Ctx) error {
	raw, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "tma ")
	if !ok {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	u, err := initdata.Validate(raw, s.BotToken, maxInitDataAge, time.Now())
	if err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	cu, err := s.API.LookupByTelegram(c.UserContext(), u.ID)
	switch {
	case errors.Is(err, vpnctl.ErrNotFound):
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

func (s *server) me(c *fiber.Ctx) error {
	cu := c.Locals("customer").(*vpnctl.Customer)
	return c.JSON(profile.Build(cu, telegram.RefLink(s.BotUsername, s.Codec, cu.ID)))
}

func (s *server) tariffs(c *fiber.Ctx) error {
	ts, err := s.API.ListTariffs(c.UserContext())
	if err != nil {
		s.Log.Error("webapp tariffs", "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	return c.JSON(profile.Tariffs(ts))
}
```

If `filesystem` with `app.Use(d.Path+"/", …)` does not strip the prefix for `/bot/assets/a.js` (test fails with 404), set `PathPrefix: ""` and mount with `app.Use(d.Path, …)` plus `filesystem.Config{Root: http.FS(d.Static), Index: "index.html", Browse: false}` — the test is the source of truth; keep whichever form passes all three static assertions.

- [ ] **Step 5: Run** `go test ./internal/webapp/...` — Expected: PASS.

- [ ] **Step 6: Commit** `git add go.mod go.sum web/embed.go web/dist/.gitkeep internal/webapp && git commit -m "feat: webapp API and static server"`

---

### Task 13: Entry point and local run (`cmd/bot`)

**Files:**
- Create: `cmd/bot/main.go`, `.env.example`, `Makefile`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Implement** — `cmd/bot/main.go`

```go
// Command bot runs the Telegram bot and its WebApp server in one process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp"
	"vpn-control-bot/web"
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

	api := vpnctl.New(cfg.VpnctlURL, cfg.VpnctlToken)
	codec := refcode.New(cfg.RefSecret)
	acc := access.New(api, codec, cfg.Mode, st)

	bot, err := telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Billing: billing.NotImplemented{}, WebAppURL: cfg.WebAppURL(), Log: log,
	})
	if err != nil {
		return err
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, Static: web.Dist, Log: log,
	})

	errc := make(chan error, 1)
	go func() { errc <- app.Listen(cfg.HTTPAddr) }()
	go bot.Run(ctx)
	log.Info("started", "mode", cfg.Mode, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	return app.Shutdown()
}
```

- [ ] **Step 2: `.env.example`**

```dotenv
# Telegram bot token from @BotFather
BOT_TOKEN=
# open — anyone can join; closed — only panel customers / subscription link / referral link
BOT_MODE=closed
# vpn-control base URL and an API token created with the "bot" preset
VPNCTL_URL=http://vpn-control:8080
VPNCTL_TOKEN=
# Public origin where the WebApp is served; WEBAPP_PATH is appended
PUBLIC_URL=https://control.example.com
WEBAPP_PATH=/bot
# Secret for referral codes. Changing it invalidates every issued referral link.
REF_SECRET=
HTTP_ADDR=:8090
DB_PATH=./data/bot.db
LOG_LEVEL=info

# Local development (compose.dev.yml): a subdomain of your own domain that
# resolves to 127.0.0.1 via /etc/hosts, e.g. local.example.com
LOCAL_DOMAIN=
```

- [ ] **Step 3: `Makefile`** (dev targets are completed in Task 16; recipes use tabs)

```makefile
.PHONY: test build run web

test:
	go vet ./... && go test ./...

web:
	cd web && bun install && bun run build

build: web
	CGO_ENABLED=0 go build -o bin/bot ./cmd/bot

run:
	set -a && . ./.env && set +a && go run ./cmd/bot
```

- [ ] **Step 4: Verify** `go build ./... && make test` — Expected: builds, all tests PASS.

- [ ] **Step 5: Commit** `git add cmd .env.example Makefile && git commit -m "feat: bot entry point"`

---

### Task 14: WebApp frontend

**Files:**
- Copy from `../vpn-control/web` (each copied source file gets a first-line comment `// Copied from vpn-control/web/<path>`; CSS files get `/* Copied from … */`): `bunfig.toml`, `postcss.config.cjs`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `scripts/postcss-plugin.ts`, `scripts/env.d.ts`, `src/env.d.ts`, `src/app/vendor.css`, `src/app/global.css`, `src/shared/constants/theme/*`, `src/shared/ui/{section-card,stat-strip,copyable-field,shimmer-skeleton,info-field}/*`, `src/components/format.ts` → `src/shared/format.ts`.
- Create: `web/package.json`, `web/index.html`, `web/scripts/build.ts`, `web/scripts/dev.ts`, `web/public/locales/ru/bot.json`, `web/src/main.tsx`, `web/src/i18n.ts`, `web/src/telegram.ts`, `web/src/api.ts`, `web/src/types.ts`, `web/src/App.tsx`, `web/src/layout/AppShell.tsx`, `web/src/layout/app-shell.module.css`, `web/src/pages/{Overview,Subscriptions,SubscriptionDetails,Balance,Referrals,Tariffs}.tsx`, `web/src/ui/{StateBadge,Empty,PageSkeleton,ErrorScreen}.tsx`.

**Interfaces:**
- Consumes: HTTP contract from Task 12; `profile` JSON field names from Task 5.
- Produces: `web/dist/` with `index.html` that loads assets by **relative** URLs (works under any `WEBAPP_PATH`); hash routes `#/`, `#/subscriptions`, `#/subscriptions/:id`, `#/balance`, `#/referrals`, `#/tariffs` (the bot links to them).

- [ ] **Step 1: Copy the panel's tooling and shared UI**

```bash
P=../vpn-control/web
mkdir -p web/scripts web/src/app web/src/shared/constants web/src/shared/ui web/public/locales/ru
cp $P/bunfig.toml $P/postcss.config.cjs $P/tsconfig.json $P/tsconfig.app.json $P/tsconfig.node.json web/
cp $P/scripts/postcss-plugin.ts $P/scripts/env.d.ts web/scripts/
cp $P/src/env.d.ts web/src/
cp $P/src/app/vendor.css $P/src/app/global.css web/src/app/
cp -R $P/src/shared/constants/theme web/src/shared/constants/
for d in section-card stat-strip copyable-field shimmer-skeleton info-field; do cp -R $P/src/shared/ui/$d web/src/shared/ui/; done
cp $P/src/components/format.ts web/src/shared/format.ts
```

Then add the source comment to the top of every copied `.ts/.tsx/.css` file. In `web/src/shared/format.ts` keep only `fmtMoney`, `fmtDate`, `fmtBytes`, `daysLeft`, `isUnlimited`, `UNLIMITED_YEAR`, `setFormat` and their private helpers; delete the rest (traffic strategies etc.). Fix its `i18n` import to `import i18n from '../i18n'`. Remove any import in the copied files that points outside the copied set (run `bunx tsc --noEmit -p tsconfig.app.json` in Step 9 to find them).

- [ ] **Step 2: `web/package.json`** — dependency versions exactly as in `../vpn-control/web/package.json`

```json
{
  "name": "vpn-control-bot-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "bun scripts/dev.ts",
    "build": "tsc -b && bun scripts/build.ts",
    "typecheck": "tsc --noEmit -p tsconfig.app.json"
  },
  "dependencies": {
    "@fontsource-variable/geist": "^5.3.0",
    "@fontsource-variable/geist-mono": "^5.3.0",
    "@mantine/core": "^9.6.3",
    "@mantine/hooks": "^9.6.3",
    "@tabler/icons-react": "^3.48.0",
    "@tanstack/react-query": "^5.104.0",
    "clsx": "^2.1.1",
    "dayjs": "^1.11.23",
    "i18next": "^26",
    "i18next-http-backend": "^4",
    "react": "^19.2.8",
    "react-dom": "^19.2.8",
    "react-i18next": "^17",
    "react-icons": "^5.7.0",
    "react-router": "^7.18.2"
  },
  "devDependencies": {
    "@types/bun": "^1.4.2",
    "@types/node": "^24",
    "@types/react": "^19.2.17",
    "@types/react-dom": "^19.2.3",
    "lightningcss": "^1.33.0",
    "postcss": "^8.5.6",
    "postcss-preset-mantine": "^1.18.0",
    "postcss-simple-vars": "^7.0.1",
    "typescript": "~6.0.2"
  }
}
```

Run `cd web && bun install` and commit `bun.lock`.

- [ ] **Step 3: Build/dev scripts with relative URLs**

`web/scripts/build.ts` — start from `../vpn-control/web/scripts/build.ts` and change exactly these things: `publicPath: './'`; the vendor `<link>` href `./${vendorName}`; drop the web-manifest block and the precompression (`.br/.gz`) block; keep fonts copying and `.gitkeep`. Result header comment:

```ts
// Copied from vpn-control/web/scripts/build.ts, adapted: the WebApp lives
// under a configurable path (WEBAPP_PATH), so every asset URL is relative.
```

`web/scripts/dev.ts`:

```ts
// Dev server: bundles index.html on each load and proxies ./api/* to the Go
// server (HTTP_ADDR) so the page can run outside the container.
import index from '../index.html'

const API = process.env.API_URL ?? 'http://localhost:8090/bot'
const publicDir = new URL('../public/', import.meta.url)

const server = Bun.serve({
    port: Number(process.env.DEV_PORT ?? 5174),
    development: { hmr: false },
    routes: {
        '/': index,
        '/api/*': (req) => {
            const url = new URL(req.url)
            return fetch(new Request(API + url.pathname + url.search, req))
        }
    },
    async fetch(req) {
        const file = Bun.file(new URL(`.${new URL(req.url).pathname}`, publicDir))
        return (await file.exists()) ? new Response(file) : new Response('Not Found', { status: 404 })
    }
})
console.log(`WebApp dev server on ${server.url} (api → ${API})`)
```

`web/index.html`:

```html
<!doctype html>
<html lang="ru">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover" />
        <title>VPN</title>
        <script src="https://telegram.org/js/telegram-web-app.js"></script>
    </head>
    <body>
        <div id="root"></div>
        <script type="module" src="./src/main.tsx"></script>
    </body>
</html>
```

- [ ] **Step 4: Telegram + API layer**

`web/src/telegram.ts`:

```ts
// Thin wrapper over window.Telegram.WebApp; everything is optional so the
// page also renders in a normal browser (it then shows "open from the bot").
type ThemeParams = Partial<Record<'bg_color' | 'text_color' | 'button_color' | 'secondary_bg_color', string>>
interface WebApp {
    initData: string
    colorScheme: 'light' | 'dark'
    themeParams: ThemeParams
    ready(): void
    expand(): void
    onEvent(e: 'themeChanged', cb: () => void): void
    BackButton: { show(): void; hide(): void; onClick(cb: () => void): void; offClick(cb: () => void): void }
    HapticFeedback?: { notificationOccurred(t: 'success' | 'error'): void }
    openTelegramLink(url: string): void
}
declare global {
    interface Window {
        Telegram?: { WebApp: WebApp }
    }
}

export const tg: WebApp | undefined = window.Telegram?.WebApp?.initData ? window.Telegram.WebApp : undefined

export function initTelegram() {
    tg?.ready()
    tg?.expand()
}

export const haptic = () => tg?.HapticFeedback?.notificationOccurred('success')
```

`web/src/types.ts` (mirrors `internal/profile`):

```ts
export interface Addon {
    id: number
    name: string
    tariff: string
    price: number
    included: boolean
    status: string
    expire_at: string | null
    unlimited: boolean
    used_bytes: number
    limit_bytes: number
}
export interface Subscription {
    id: number
    title: string
    tariff: string
    price: number
    status: string
    expire_at: string | null
    unlimited: boolean
    used_bytes: number
    limit_bytes: number
    device_limit: number | null
    url: string
    addons: Addon[]
}
export interface LedgerEntry {
    type: string
    amount: number
    date: string
}
export interface Referral {
    name: string
    active: boolean
    monthly: number
    total_paid: number
    earned: number
}
export interface Profile {
    id: number
    name: string
    balance: number
    balance_own: number
    balance_referral: number
    monthly: number
    nearest_expire_at: string | null
    subscriptions: Subscription[]
    ledger: LedgerEntry[]
    referrals: Referral[]
    referrals_earned: number
    ref_link: string
}
export interface Period {
    months: number
    days: number
    price: number
}
export interface Tariff {
    id: number
    kind: 'base' | 'addon'
    name: string
    description: string
    addon_name: string
    monthly_price: number
    traffic_limit_bytes: number
    hwid_limit: number | null
    periods: Period[] | null
}
```

`web/src/api.ts`:

```ts
import { useQuery } from '@tanstack/react-query'

import { tg } from './telegram'
import type { Profile, Tariff } from './types'

export class ApiError extends Error {
    constructor(public status: number, public code: string) {
        super(code)
    }
}

// Relative to the page, so it works under any WEBAPP_PATH.
async function get<T>(path: string): Promise<T> {
    const res = await fetch(`./api/${path}`, { headers: { Authorization: `tma ${tg?.initData ?? ''}` } })
    if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new ApiError(res.status, body.code ?? 'error')
    }
    return res.json()
}

const retry = (n: number, e: unknown) => !(e instanceof ApiError && e.status < 500) && n < 2

export const useProfile = () => useQuery({ queryKey: ['me'], queryFn: () => get<Profile>('me'), retry })
export const useTariffs = () => useQuery({ queryKey: ['tariffs'], queryFn: () => get<Tariff[]>('tariffs'), retry })
```

- [ ] **Step 5: i18n** — `web/src/i18n.ts` loads `./locales/ru/bot.json` via `i18next-http-backend` with `lng: 'ru'`, `ns: ['bot']`, `defaultNS: 'bot'`, `loadPath: './locales/{{lng}}/{{ns}}.json'`, `interpolation: { escapeValue: false }`, `react: { useSuspense: true }`, plus a `format.date` key used by `shared/format.ts`. `web/public/locales/ru/bot.json`:

```json
{
    "format": { "date": "DD.MM.YYYY", "datetime": "DD.MM.YYYY HH:mm" },
    "nav": { "overview": "Главная", "subscriptions": "Подписки", "balance": "Баланс", "referrals": "Рефералы" },
    "overview": {
        "hello": "Привет, {{name}}!",
        "balance": "Баланс", "monthly": "В месяц", "nearest": "Ближайшее окончание",
        "subs": "Мои подписки", "all": "Все", "tariffs": "Тарифы"
    },
    "sub": {
        "until": "до {{date}}", "forever": "бессрочно", "traffic": "Трафик", "devices": "Устройств: до {{n}}",
        "link": "Ссылка подписки", "addons": "Аддоны", "included": "входит в тариф", "per_month": "{{price}} / мес",
        "status": { "ACTIVE": "Активна", "EXPIRED": "Истекла", "DISABLED": "Отключена", "LIMITED": "Лимит трафика", "PENDING": "Подключается" }
    },
    "balance": {
        "title": "Баланс", "own": "Свои средства", "referral": "Реферальные", "history": "История операций",
        "types": { "payment": "Пополнение", "charge": "Списание", "adjustment": "Корректировка", "refund": "Возврат", "referral": "Реферальное начисление" }
    },
    "refs": {
        "title": "Рефералы", "link": "Ваша реферальная ссылка", "share": "Поделиться", "invited": "Приглашено",
        "earned": "Заработано", "paid": "оплатил {{sum}}", "per_month": "{{sum}} / мес", "inactive": "неактивен",
        "share_text": "Подключайся к VPN по моей ссылке"
    },
    "tariffs": { "title": "Тарифы", "per_month": "/ мес", "addon": "Аддон", "periods": "Можно оплатить сразу на:", "pay_soon": "Оплата скоро появится" },
    "empty": {
        "subs": "Подписок пока нет", "subs_hint": "Выберите тариф — подписка появится здесь.",
        "ledger": "Операций пока не было", "refs": "Вы ещё никого не пригласили",
        "refs_hint": "Поделитесь ссылкой — за оплаты друзей вы получаете процент на баланс."
    },
    "errors": {
        "no_tg": "Откройте приложение из Telegram-бота.",
        "no_access": "Доступ есть только у клиентов. Откройте бота и отправьте ссылку на подписку.",
        "unavailable": "Сервис временно недоступен. Попробуйте позже.",
        "retry": "Повторить"
    },
    "copied": "Скопировано"
}
```

- [ ] **Step 6: App shell, theme sync, routing**

`web/src/main.tsx`:

```tsx
import '@fontsource-variable/geist'
import '@fontsource-variable/geist-mono'
import './app/vendor.css'
import './app/global.css'
import './i18n'

import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode, Suspense, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter } from 'react-router'

import { App } from './App'
import { initTelegram, tg } from './telegram'
import { PageSkeleton } from './ui/PageSkeleton'
import { cssVariablesResolver, theme } from '@shared/constants/theme'

initTelegram()

const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } })

// Follows the Telegram client's light/dark switch.
function Root() {
    const [scheme, setScheme] = useState<'light' | 'dark'>(tg?.colorScheme ?? 'dark')
    useEffect(() => tg?.onEvent('themeChanged', () => setScheme(tg!.colorScheme)), [])
    return (
        <MantineProvider cssVariablesResolver={cssVariablesResolver} forceColorScheme={scheme} theme={theme}>
            <Suspense fallback={<PageSkeleton />}>
                <QueryClientProvider client={queryClient}>
                    <HashRouter>
                        <App />
                    </HashRouter>
                </QueryClientProvider>
            </Suspense>
        </MantineProvider>
    )
}

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <Root />
    </StrictMode>
)
```

`web/src/App.tsx`:

```tsx
import { Route, Routes } from 'react-router'

import { ApiError, useProfile } from './api'
import { AppShell } from './layout/AppShell'
import { Balance } from './pages/Balance'
import { Overview } from './pages/Overview'
import { Referrals } from './pages/Referrals'
import { SubscriptionDetails } from './pages/SubscriptionDetails'
import { Subscriptions } from './pages/Subscriptions'
import { Tariffs } from './pages/Tariffs'
import { tg } from './telegram'
import { ErrorScreen } from './ui/ErrorScreen'
import { PageSkeleton } from './ui/PageSkeleton'

export function App() {
    const profile = useProfile()
    if (!tg) return <ErrorScreen code="no_tg" />
    if (profile.isPending) return <PageSkeleton />
    if (profile.isError) {
        const e = profile.error
        const code = e instanceof ApiError && e.code === 'no_access' ? 'no_access' : e instanceof ApiError && e.status === 401 ? 'no_tg' : 'unavailable'
        return <ErrorScreen code={code} onRetry={code === 'unavailable' ? () => profile.refetch() : undefined} />
    }
    const p = profile.data
    return (
        <AppShell>
            <Routes>
                <Route element={<Overview p={p} />} path="/" />
                <Route element={<Subscriptions p={p} />} path="/subscriptions" />
                <Route element={<SubscriptionDetails p={p} />} path="/subscriptions/:id" />
                <Route element={<Balance p={p} />} path="/balance" />
                <Route element={<Referrals p={p} />} path="/referrals" />
                <Route element={<Tariffs />} path="/tariffs" />
                <Route element={<Overview p={p} />} path="*" />
            </Routes>
        </AppShell>
    )
}
```

`web/src/layout/AppShell.tsx` — content column (`maxWidth: 560`, `padding: 16px 16px calc(80px + env(safe-area-inset-bottom))`) and a fixed bottom nav with four tabs (Overview `IconHome`, Subscriptions `IconPackage`, Balance `IconWallet`, Referrals `IconUsers` from `@tabler/icons-react`), active tab = `useLocation().pathname` prefix; on nested pages (`/subscriptions/:id`, `/tariffs`) shows Telegram's BackButton:

```tsx
import { Box, Group, Text, UnstyledButton } from '@mantine/core'
import { IconHome, IconPackage, IconUsers, IconWallet } from '@tabler/icons-react'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'

import { tg } from '../telegram'
import classes from './app-shell.module.css'

const tabs = [
    { to: '/', icon: IconHome, key: 'overview' },
    { to: '/subscriptions', icon: IconPackage, key: 'subscriptions' },
    { to: '/balance', icon: IconWallet, key: 'balance' },
    { to: '/referrals', icon: IconUsers, key: 'referrals' }
] as const

export function AppShell({ children }: { children: ReactNode }) {
    const { t } = useTranslation()
    const { pathname } = useLocation()
    const navigate = useNavigate()
    const nested = pathname.split('/').filter(Boolean).length > 1 || pathname === '/tariffs'

    useEffect(() => {
        if (!tg || !nested) return tg?.BackButton.hide()
        const back = () => navigate(-1)
        tg.BackButton.show()
        tg.BackButton.onClick(back)
        return () => tg?.BackButton.offClick(back)
    }, [nested, navigate])

    const active = (to: string) => (to === '/' ? pathname === '/' : pathname.startsWith(to))
    return (
        <>
            <Box className={classes.content}>{children}</Box>
            <Group className={classes.nav} gap={0} grow wrap="nowrap">
                {tabs.map(({ to, icon: Icon, key }) => (
                    <UnstyledButton className={classes.tab} data-active={active(to) || undefined} key={to} onClick={() => navigate(to)}>
                        <Icon size={22} stroke={1.75} />
                        <Text size="xs">{t(`nav.${key}`)}</Text>
                    </UnstyledButton>
                ))}
            </Group>
        </>
    )
}
```

`web/src/layout/app-shell.module.css`:

```css
.content {
    max-width: 560px;
    margin: 0 auto;
    padding: 16px 16px calc(84px + env(safe-area-inset-bottom));
}
.nav {
    position: fixed;
    inset: auto 0 0 0;
    padding: 6px 8px calc(6px + env(safe-area-inset-bottom));
    background: var(--mantine-color-body);
    border-top: 1px solid var(--mantine-color-default-border);
    z-index: 10;
}
.tab {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    padding: 6px 0;
    min-height: 52px;
    border-radius: var(--mantine-radius-md);
    color: var(--mantine-color-dimmed);
}
.tab[data-active] {
    color: var(--mantine-primary-color-filled);
}
```

- [ ] **Step 7: Shared UI pieces** (`web/src/ui/`)

```tsx
// StateBadge.tsx — status pill with the same colors as the bot's icons.
import { Badge } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { daysLeft } from '@shared/format'

export function stateColor(expireAt: string | null, unlimited: boolean) {
    if (unlimited) return 'teal'
    const d = daysLeft(expireAt)
    if (d === null) return 'gray'
    return d < 0 ? 'red' : d < 3 ? 'yellow' : 'teal'
}

export function StateBadge({ status, expireAt, unlimited }: { status: string; expireAt: string | null; unlimited: boolean }) {
    const { t } = useTranslation()
    return (
        <Badge color={stateColor(expireAt, unlimited)} radius="sm" variant="light">
            {t(`sub.status.${status}`, { defaultValue: status })}
        </Badge>
    )
}
```

```tsx
// Empty.tsx — empty state with a hint and an optional action.
import { Button, Stack, Text, ThemeIcon } from '@mantine/core'
import type { ReactNode } from 'react'

export function Empty({ icon, title, hint, action }: { icon: ReactNode; title: string; hint?: string; action?: { label: string; onClick: () => void } }) {
    return (
        <Stack align="center" gap="xs" py="xl" ta="center">
            <ThemeIcon color="gray" radius="xl" size={56} variant="light">
                {icon}
            </ThemeIcon>
            <Text fw={600}>{title}</Text>
            {hint && <Text c="dimmed" size="sm" maw={320}>{hint}</Text>}
            {action && <Button mt="sm" onClick={action.onClick} radius="md">{action.label}</Button>}
        </Stack>
    )
}
```

```tsx
// PageSkeleton.tsx
import { Stack } from '@mantine/core'

import { ShimmerSkeleton } from '@shared/ui/shimmer-skeleton'

export function PageSkeleton() {
    return (
        <Stack gap="md" p="md" maw={560} mx="auto">
            <ShimmerSkeleton height={28} radius="md" width="60%" />
            <ShimmerSkeleton height={96} radius="lg" width="100%" />
            <ShimmerSkeleton height={140} radius="lg" width="100%" />
            <ShimmerSkeleton height={140} radius="lg" width="100%" />
        </Stack>
    )
}
```

```tsx
// ErrorScreen.tsx
import { Button, Center, Stack, Text, ThemeIcon } from '@mantine/core'
import { IconLock, IconPlugConnectedX, IconBrandTelegram } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

const icons = { no_tg: IconBrandTelegram, no_access: IconLock, unavailable: IconPlugConnectedX }

export function ErrorScreen({ code, onRetry }: { code: keyof typeof icons; onRetry?: () => void }) {
    const { t } = useTranslation()
    const Icon = icons[code]
    return (
        <Center h="100dvh" p="lg">
            <Stack align="center" gap="sm" ta="center" maw={340}>
                <ThemeIcon radius="xl" size={64} variant="light">
                    <Icon size={32} />
                </ThemeIcon>
                <Text fw={500}>{t(`errors.${code}`)}</Text>
                {onRetry && <Button onClick={onRetry} variant="light">{t('errors.retry')}</Button>}
            </Stack>
        </Center>
    )
}
```

- [ ] **Step 8: Pages** (`web/src/pages/`) — Mantine primitives + copied `StatStrip`, `SectionCard`, `CopyableFieldShared`; money via `fmtMoney(v, 2)` (call `setFormat({ base: 'RUB', locale: 'ru-RU' })` once in `main.tsx`), dates via `fmtDate`, bytes via `fmtBytes`.

```tsx
// Overview.tsx — greeting, figures, nearest subscriptions, shortcuts.
import { Button, Card, Group, Stack, Text, Title, UnstyledButton } from '@mantine/core'
import { IconChevronRight, IconPackage, IconTag } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtDate, fmtMoney } from '@shared/format'
import { StatStrip } from '@shared/ui/stat-strip'
import type { Profile } from '../types'
import { Empty } from '../ui/Empty'
import { StateBadge } from '../ui/StateBadge'

export function Overview({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    return (
        <Stack gap="lg">
            <Title order={3}>{t('overview.hello', { name: p.name })}</Title>
            <StatStrip
                items={[
                    { label: t('overview.balance'), value: fmtMoney(p.balance, 2), onClick: () => navigate('/balance') },
                    { label: t('overview.monthly'), value: fmtMoney(p.monthly) },
                    { label: t('overview.nearest'), value: fmtDate(p.nearest_expire_at) }
                ]}
            />
            <Group justify="space-between">
                <Text fw={600}>{t('overview.subs')}</Text>
                {p.subscriptions.length > 0 && (
                    <Button onClick={() => navigate('/subscriptions')} rightSection={<IconChevronRight size={16} />} size="compact-sm" variant="subtle">
                        {t('overview.all')}
                    </Button>
                )}
            </Group>
            {p.subscriptions.length === 0 ? (
                <Empty
                    action={{ label: t('overview.tariffs'), onClick: () => navigate('/tariffs') }}
                    hint={t('empty.subs_hint')}
                    icon={<IconPackage />}
                    title={t('empty.subs')}
                />
            ) : (
                p.subscriptions.slice(0, 3).map((s) => (
                    <UnstyledButton key={s.id} onClick={() => navigate(`/subscriptions/${s.id}`)}>
                        <Card padding="md" radius="lg" withBorder>
                            <Group justify="space-between" wrap="nowrap">
                                <Stack gap={2}>
                                    <Text fw={600}>{s.title}</Text>
                                    <Text c="dimmed" size="sm">
                                        {s.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(s.expire_at) })}
                                    </Text>
                                </Stack>
                                <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
                            </Group>
                        </Card>
                    </UnstyledButton>
                ))
            )}
            <Button leftSection={<IconTag size={18} />} onClick={() => navigate('/tariffs')} radius="md" size="md" variant="light">
                {t('overview.tariffs')}
            </Button>
        </Stack>
    )
}
```

`Subscriptions.tsx` — title + the same subscription cards as Overview for every subscription (extract the card into `web/src/ui/SubscriptionRow.tsx` and use it in both pages), empty state identical to Overview.

```tsx
// SubscriptionDetails.tsx — everything about one subscription.
import { Group, Progress, Stack, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Navigate, useParams } from 'react-router'

import { fmtBytes, fmtDate, fmtMoney } from '@shared/format'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SectionCard } from '@shared/ui/section-card'
import { haptic } from '../telegram'
import type { Profile } from '../types'
import { StateBadge } from '../ui/StateBadge'

const pct = (used: number, limit: number) => (limit > 0 ? Math.min(100, (used / limit) * 100) : 0)

export function SubscriptionDetails({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const { id } = useParams()
    const s = p.subscriptions.find((x) => x.id === Number(id))
    if (!s) return <Navigate replace to="/subscriptions" />
    const until = s.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(s.expire_at) })
    return (
        <Stack gap="lg">
            <Group justify="space-between" wrap="nowrap">
                <Title order={3}>{s.title}</Title>
                <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
            </Group>
            <SectionCard.Root>
                <Group justify="space-between">
                    <Text c="dimmed">{s.tariff}</Text>
                    <Text fw={600}>{t('sub.per_month', { price: fmtMoney(s.price) })}</Text>
                </Group>
                <Text fw={500}>{until}</Text>
                <Stack gap={6}>
                    <Group justify="space-between">
                        <Text size="sm">{t('sub.traffic')}</Text>
                        <Text c="dimmed" size="sm">
                            {fmtBytes(s.used_bytes)} / {s.limit_bytes > 0 ? fmtBytes(s.limit_bytes) : '∞'}
                        </Text>
                    </Group>
                    {s.limit_bytes > 0 && <Progress radius="xl" value={pct(s.used_bytes, s.limit_bytes)} />}
                </Stack>
                {s.device_limit !== null && <Text size="sm">{t('sub.devices', { n: s.device_limit })}</Text>}
            </SectionCard.Root>
            {s.url && (
                <div onClick={haptic}>
                    <CopyableFieldShared label={t('sub.link')} size="md" value={s.url} />
                </div>
            )}
            {s.addons.length > 0 && (
                <Stack gap="sm">
                    <Text fw={600}>{t('sub.addons')}</Text>
                    <SectionCard.Root>
                        {s.addons.map((a) => (
                            <Group justify="space-between" key={a.id} wrap="nowrap">
                                <Stack gap={0}>
                                    <Text fw={500}>{a.name}</Text>
                                    <Text c="dimmed" size="xs">
                                        {a.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(a.expire_at) })}
                                        {a.included ? ` · ${t('sub.included')}` : ` · ${t('sub.per_month', { price: fmtMoney(a.price) })}`}
                                    </Text>
                                </Stack>
                                <StateBadge expireAt={a.expire_at} status={a.status} unlimited={a.unlimited} />
                            </Group>
                        ))}
                    </SectionCard.Root>
                </Stack>
            )}
        </Stack>
    )
}
```

```tsx
// Balance.tsx — totals and full history grouped by month.
import { Group, Stack, Text, Title } from '@mantine/core'
import { IconReceipt } from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'

import { fmtDate, fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { StatStrip } from '@shared/ui/stat-strip'
import type { LedgerEntry, Profile } from '../types'
import { Empty } from '../ui/Empty'

export function Balance({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const months = new Map<string, LedgerEntry[]>()
    for (const e of p.ledger) {
        const key = dayjs(e.date).format('MMMM YYYY')
        months.set(key, [...(months.get(key) ?? []), e])
    }
    return (
        <Stack gap="lg">
            <Title order={3}>{t('balance.title')}</Title>
            <StatStrip
                items={[
                    { label: t('balance.title'), value: fmtMoney(p.balance, 2) },
                    { label: t('balance.own'), value: fmtMoney(p.balance_own, 2) },
                    { label: t('balance.referral'), value: fmtMoney(p.balance_referral, 2) }
                ]}
            />
            <Text fw={600}>{t('balance.history')}</Text>
            {p.ledger.length === 0 ? (
                <Empty icon={<IconReceipt />} title={t('empty.ledger')} />
            ) : (
                [...months].map(([month, items]) => (
                    <Stack gap="xs" key={month}>
                        <Text c="dimmed" size="sm" tt="capitalize">{month}</Text>
                        <SectionCard.Root gap="sm">
                            {items.map((e, i) => (
                                <Group justify="space-between" key={i} wrap="nowrap">
                                    <Stack gap={0}>
                                        <Text size="sm">{t(`balance.types.${e.type}`, { defaultValue: e.type })}</Text>
                                        <Text c="dimmed" size="xs">{fmtDate(e.date)}</Text>
                                    </Stack>
                                    <Text c={e.amount >= 0 ? 'teal' : undefined} fw={600}>
                                        {e.amount > 0 ? '+' : ''}
                                        {fmtMoney(e.amount, 2)}
                                    </Text>
                                </Group>
                            ))}
                        </SectionCard.Root>
                    </Stack>
                ))
            )}
        </Stack>
    )
}
```

Set `dayjs.locale('ru')` (import `dayjs/locale/ru`) in `main.tsx` so month names are Russian.

```tsx
// Referrals.tsx — link with copy/share, totals, every invitee with income.
import { Button, Group, Stack, Text, Title } from '@mantine/core'
import { IconSend, IconUsers } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { fmtMoney } from '@shared/format'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SectionCard } from '@shared/ui/section-card'
import { StatStrip } from '@shared/ui/stat-strip'
import { haptic, tg } from '../telegram'
import type { Profile } from '../types'
import { Empty } from '../ui/Empty'

export function Referrals({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const share = () =>
        tg?.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(p.ref_link)}&text=${encodeURIComponent(t('refs.share_text'))}`)
    return (
        <Stack gap="lg">
            <Title order={3}>{t('refs.title')}</Title>
            <div onClick={haptic}>
                <CopyableFieldShared label={t('refs.link')} size="md" value={p.ref_link} />
            </div>
            <Button leftSection={<IconSend size={18} />} onClick={share} radius="md" size="md">
                {t('refs.share')}
            </Button>
            <StatStrip
                items={[
                    { label: t('refs.invited'), value: p.referrals.length },
                    { label: t('refs.earned'), value: fmtMoney(p.referrals_earned, 2) }
                ]}
            />
            {p.referrals.length === 0 ? (
                <Empty hint={t('empty.refs_hint')} icon={<IconUsers />} title={t('empty.refs')} />
            ) : (
                <SectionCard.Root gap="sm">
                    {p.referrals.map((r, i) => (
                        <Group justify="space-between" key={i} wrap="nowrap">
                            <Stack gap={0}>
                                <Text c={r.active ? undefined : 'dimmed'} fw={500}>{r.name}</Text>
                                <Text c="dimmed" size="xs">
                                    {r.active ? t('refs.per_month', { sum: fmtMoney(r.monthly) }) : t('refs.inactive')} ·{' '}
                                    {t('refs.paid', { sum: fmtMoney(r.total_paid) })}
                                </Text>
                            </Stack>
                            <Text c="teal" fw={600}>+{fmtMoney(r.earned, 2)}</Text>
                        </Group>
                    ))}
                </SectionCard.Root>
            )}
        </Stack>
    )
}
```

```tsx
// Tariffs.tsx — catalog; paying is not available yet.
import { Badge, Button, Card, Group, Stack, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { useTariffs } from '../api'
import { fmtBytes, fmtMoney } from '@shared/format'
import { PageSkeleton } from '../ui/PageSkeleton'

const term = (m: number, d: number) => [m && `${m} мес`, d && `${d} дн`].filter(Boolean).join(' ')

export function Tariffs() {
    const { t } = useTranslation()
    const q = useTariffs()
    if (q.isPending) return <PageSkeleton />
    return (
        <Stack gap="lg">
            <Title order={3}>{t('tariffs.title')}</Title>
            {(q.data ?? []).map((tf) => (
                <Card key={tf.id} padding="lg" radius="lg" withBorder>
                    <Stack gap="xs">
                        <Group justify="space-between" wrap="nowrap">
                            <Text fw={700} size="lg">{tf.kind === 'addon' && tf.addon_name ? `${tf.addon_name} · ${tf.name}` : tf.name}</Text>
                            {tf.kind === 'addon' && <Badge variant="light">{t('tariffs.addon')}</Badge>}
                        </Group>
                        <Text>
                            <Text fw={700} size="xl" span>{fmtMoney(tf.monthly_price)}</Text>{' '}
                            <Text c="dimmed" span>{t('tariffs.per_month')}</Text>
                        </Text>
                        {tf.description && <Text c="dimmed" size="sm">{tf.description}</Text>}
                        <Group gap="xs">
                            {tf.traffic_limit_bytes > 0 && <Badge color="gray" variant="light">{fmtBytes(tf.traffic_limit_bytes)}</Badge>}
                            {tf.hwid_limit !== null && <Badge color="gray" variant="light">≤ {tf.hwid_limit} устр.</Badge>}
                        </Group>
                        {tf.periods && tf.periods.length > 0 && (
                            <Text c="dimmed" size="xs">
                                {t('tariffs.periods')} {tf.periods.map((p) => `${term(p.months, p.days)} — ${fmtMoney(p.price)}`).join(', ')}
                            </Text>
                        )}
                        <Button disabled mt="xs" radius="md" variant="light">{t('tariffs.pay_soon')}</Button>
                    </Stack>
                </Card>
            ))}
        </Stack>
    )
}
```

- [ ] **Step 9: Verify the build**

Run: `cd web && bun run typecheck && bun run build && ls dist && grep -c '"/assets' dist/index.html`
Expected: typecheck passes; `dist/index.html` exists; the grep prints `0` (no root-absolute asset URLs).

- [ ] **Step 10: Check it renders** — run `go run ./cmd/bot` with a real `.env` (or `cd web && bun run dev` against a running bot) and open the WebApp from Telegram Desktop; walk through all five sections in light and dark Telegram themes; check empty states with a fresh customer. Fix layout issues before committing.

- [ ] **Step 11: Commit**

```bash
git add web
git commit -m "feat: telegram webapp frontend"
```

---

### Task 15: "bot" token preset in vpn-control (other repository)

**Files:**
- Modify: `../vpn-control/web/src/components/api-tokens/modals/create-api-token-modal.tsx` (next to `SUBPAGE_PRESET_KEYS`, line ~26, and the preset buttons that use it)
- Modify: `../vpn-control/web/public/locales/{en,ru}/vpn-control.json` (preset label next to the existing subpage preset label)

- [ ] **Step 1: Read** how `SUBPAGE_PRESET_KEYS` is used in the modal (search the file for `SUBPAGE_PRESET_KEYS` and for the subpage preset label key in both locale files).

- [ ] **Step 2: Add the preset**

```ts
// What vpn-control-bot needs: find/create customers, link a Telegram id,
// read their card (subscriptions, balance, referrals) and the tariffs.
const BOT_PRESET_KEYS = ['customers:lookup', 'customers:get', 'customers:create', 'customers:update', 'tariffs:list']
```

Add a "Bot" preset button exactly like the subpage one (same component, same `setKeys`/selection call, label via a new i18n key next to the subpage one: en "Telegram bot", ru "Telegram-бот").

- [ ] **Step 3: Verify** `cd ../vpn-control/web && bun run typecheck` — Expected: PASS (includes the i18n key check).

- [ ] **Step 4: Commit in that repo**

```bash
cd ../vpn-control
git add web/src/components/api-tokens/modals/create-api-token-modal.tsx web/public/locales
git commit -m "feat(api-tokens): add Telegram bot scope preset"
```

---

### Task 16: Docker image, compose files, local domain via traefik + mkcert

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `compose.yml`, `compose.dev.yml`, `dev/traefik/traefik.yml`, `dev/traefik/dynamic.yml`, `README.md`
- Modify: `Makefile`

- [ ] **Step 1: `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1
FROM oven/bun:1 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
ENV DB_PATH=/data/bot.db HTTP_ADDR=:8090
VOLUME /data
EXPOSE 8090
ENTRYPOINT ["/bot"]
```

`.dockerignore`:
```
.git
.env
data
dev/certs
web/node_modules
web/dist
bin
docs
```

- [ ] **Step 2: `compose.yml`** (production example, next to the panel)

```yaml
# Example deployment next to vpn-control: same docker network, the WebApp is
# routed by path ($WEBAPP_PATH) on the panel's domain by your balancer/Traefik.
services:
    vpn-control-bot:
        build: .
        container_name: vpn-control-bot
        restart: unless-stopped
        env_file: .env
        environment:
            DB_PATH: /data/bot.db
            HTTP_ADDR: ':8090'
        volumes:
            - bot-data:/data
        networks:
            - remnawave-network
        labels:
            - 'traefik.enable=true'
            - 'traefik.http.routers.vpn-control-bot.rule=Host(`${APP_DOMAIN}`) && PathPrefix(`${WEBAPP_PATH:-/bot}`)'
            - 'traefik.http.routers.vpn-control-bot.priority=100'
            - 'traefik.http.routers.vpn-control-bot.tls=true'
            - 'traefik.http.routers.vpn-control-bot.entrypoints=${TRAEFIK_ENTRYPOINTS:-web,websecure}'
            - 'traefik.http.routers.vpn-control-bot.tls.certresolver=${TRAEFIK_CERTRESOLVER:-letsencrypt}'
            - 'traefik.http.services.vpn-control-bot.loadbalancer.server.port=8090'

volumes:
    bot-data:

networks:
    remnawave-network:
        name: ${REMNAWAVE_NETWORK:-remnawave-network}
        external: true
```

The router `priority=100` makes `/bot` win over the panel's `Host()`-only router.

- [ ] **Step 3: Dev traefik config**

`dev/traefik/traefik.yml`:
```yaml
entryPoints:
    web:
        address: ':80'
        http:
            redirections:
                entryPoint: { to: websecure, scheme: https }
    websecure:
        address: ':443'
providers:
    docker:
        exposedByDefault: false
    file:
        filename: /etc/traefik/dynamic.yml
api:
    dashboard: true
    insecure: true
log:
    level: INFO
```

`dev/traefik/dynamic.yml`:
```yaml
tls:
    stores:
        default:
            defaultCertificate:
                certFile: /certs/local.pem
                keyFile: /certs/local-key.pem
```

- [ ] **Step 4: `compose.dev.yml`**

```yaml
# Local stack: https://$LOCAL_DOMAIN → vpn-control, https://$LOCAL_DOMAIN$WEBAPP_PATH → bot.
# Needs: `make certs` (mkcert) and `127.0.0.1 $LOCAL_DOMAIN` in /etc/hosts (`make hosts`).
name: vpn-control-dev
services:
    traefik:
        image: traefik:v3
        ports: ['80:80', '443:443', '8088:8080']
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock:ro
            - ./dev/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
            - ./dev/traefik/dynamic.yml:/etc/traefik/dynamic.yml:ro
            - ./dev/certs:/certs:ro

    vpn-control:
        image: ghcr.io/lentryd/vpn-control:latest
        env_file: ${PANEL_ENV:-../vpn-control/.env}
        environment:
            PORT: 8080
            DB_PATH: /data/vpn-control.db
            APP_DOMAIN: ${LOCAL_DOMAIN:?set LOCAL_DOMAIN in .env}
        volumes:
            - panel-data:/data
        labels:
            - 'traefik.enable=true'
            - 'traefik.http.routers.panel.rule=Host(`${LOCAL_DOMAIN}`)'
            - 'traefik.http.routers.panel.entrypoints=websecure'
            - 'traefik.http.routers.panel.tls=true'
            - 'traefik.http.services.panel.loadbalancer.server.port=8080'

    bot:
        build: .
        env_file: .env
        environment:
            VPNCTL_URL: http://vpn-control:8080
            PUBLIC_URL: https://${LOCAL_DOMAIN}
            DB_PATH: /data/bot.db
            HTTP_ADDR: ':8090'
        volumes:
            - bot-data:/data
        depends_on: [vpn-control]
        labels:
            - 'traefik.enable=true'
            - 'traefik.http.routers.bot.rule=Host(`${LOCAL_DOMAIN}`) && PathPrefix(`${WEBAPP_PATH:-/bot}`)'
            - 'traefik.http.routers.bot.priority=100'
            - 'traefik.http.routers.bot.entrypoints=websecure'
            - 'traefik.http.routers.bot.tls=true'
            - 'traefik.http.services.bot.loadbalancer.server.port=8090'

volumes:
    panel-data:
    bot-data:
```

- [ ] **Step 5: Makefile dev targets** (append; recipes use tabs)

```makefile
-include .env
export

DEV = docker compose -f compose.dev.yml

.PHONY: certs hosts up down logs

certs:
	@test -n "$(LOCAL_DOMAIN)" || (echo "set LOCAL_DOMAIN in .env" && exit 1)
	mkdir -p dev/certs
	mkcert -install
	mkcert -cert-file dev/certs/local.pem -key-file dev/certs/local-key.pem "$(LOCAL_DOMAIN)"

hosts:
	@grep -qE "[[:space:]]$(LOCAL_DOMAIN)$$" /etc/hosts \
		&& echo "/etc/hosts already has $(LOCAL_DOMAIN)" \
		|| echo "add this line to /etc/hosts:  127.0.0.1 $(LOCAL_DOMAIN)"

up: certs hosts
	$(DEV) up -d --build

down:
	$(DEV) down

logs:
	$(DEV) logs -f bot
```

`-include` keeps `make test` working without a `.env`.

- [ ] **Step 6: `README.md`** — sections: What it is (two modes, screens); Configuration (the ENV table from Global Constraints); Panel token ("Settings → API tokens → preset «Telegram bot»"); Local development (fill `.env` incl. `LOCAL_DOMAIN=local.<your-domain>`, `make up`, set the bot's domain in @BotFather → Bot Settings → Configure Mini App / Domain to `LOCAL_DOMAIN`, open Telegram Desktop on the same machine — it trusts the mkcert CA; traefik dashboard on http://localhost:8088); Production (`compose.yml` next to the panel, `docker compose up -d --build`); Tests (`make test`).

- [ ] **Step 7: Verify**

Run: `docker build -t vpn-control-bot:dev . && docker compose -f compose.dev.yml config >/dev/null && echo ok`
Expected: image builds; `ok`.

Then with a filled `.env`: `make up`, open `https://$LOCAL_DOMAIN/bot/` in a browser — expected: the "open from the bot" screen (no initData), valid certificate; `https://$LOCAL_DOMAIN/` — the panel. In Telegram Desktop: `/start` → onboarding per `BOT_MODE`; the "Открыть" menu button opens the WebApp with data.

- [ ] **Step 8: Commit**

```bash
git add Dockerfile .dockerignore compose.yml compose.dev.yml dev/traefik Makefile README.md
git commit -m "feat: docker image, compose files and local traefik domain"
```

---

### Task 17: End-to-end check against the spec

- [ ] **Step 1:** `make test` — all Go tests pass; `cd web && bun run typecheck` passes.
- [ ] **Step 2:** In the local stack, walk the scenarios and tick each:
  - closed mode, unknown user, `/start` → asks for the subscription link; garbage text → hint; unknown link → "not found"; a link of a customer with another Telegram id → refused; a free link → "✅ привязана" + home; the panel shows the `telegram_id` and the customer's other fields unchanged.
  - `/start ref_<code>` from another account (code from 👥 Рефералы) → new customer in the panel with that referrer; forged code → "недействительна" (closed) / created without referrer (`BOT_MODE=open`).
  - open mode, unknown user → created, welcome + home.
  - every inline button edits the same message; "Подробнее в приложении" opens the right WebApp section; tariff "Выбрать" shows the "скоро" alert.
  - stop the panel container → bot answers "Сервис временно недоступен", WebApp shows the retry screen.
- [ ] **Step 3:** Fix anything found (each fix with a test where the logic lives in a pure package), commit.
