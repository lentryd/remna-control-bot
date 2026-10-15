# Payment providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A customer pays any order through Telegram Stars, YooKassa, CryptoBot or Heleket; the payment is confirmed automatically (webhook, Telegram payment events, or a periodic status poll) and applied through the existing order core exactly once.

**Architecture:** A new `internal/payments` package holds a small `Provider` interface and one adapter file per provider (invoice creation, webhook verification, optional status polling). `internal/orders` gains the `awaiting_payment` status and three methods — `CreateWith`/`Pay` (issue an invoice), `Paid` (apply a confirmed payment), `SweepProviders` (lost-webhook recovery and expiry). The WebApp serves `POST /pay/:provider` webhooks and returns `pay_url`; the bot handles Stars' `pre_checkout_query` / `successful_payment` and offers provider buttons in its renew flow.

**Tech Stack:** Go 1.26 (stdlib `net/http`, `crypto/*`), go-telegram/bot v1.23, Fiber v2, modernc SQLite; React 19 + Mantine 9 + TanStack Query (Bun).

**Spec:** `docs/superpowers/specs/2026-10-07-payment-providers-design.md`. Where this plan differs from the spec (simplifications found while reading the code), **the plan wins**:
- the order's `Method` *is* the provider id — there is no separate `provider` column;
- the invoiced amount is stored as `pay_amount REAL` + `pay_currency TEXT` (not one string);
- `Pay` takes `customerID int` (ownership check), not a `*remnactl.Customer`;
- the provider sweep runs in its own 1-minute loop in `cmd/bot/main.go`, not inside `notify.Runner` (whose interval is 1 h), and `SweepProviders` itself expires stale `awaiting_payment` orders (`ExpireStale` is unchanged);
- `Quote` gets `Titles map[string]string`; the panel stores `method` as a free string, so provider ids (`yookassa`, …) are valid payment methods there (checked in remna-control: no allow-list).

## Global Constraints

- Money is `float64` in major units (rubles), rounded with `math.Round(v*100)/100`; invoices are issued in RUB, Stars in `XTR` (whole stars, `ceil(ToPay / STARS_RUB_RATE)`).
- Price is fixed at order creation and never recomputed; what is invoiced is `ToPay`.
- Every status change goes through `UPDATE … WHERE id=? AND status IN (…)` (`Service.move`); a lost race returns `orders.ErrStateChanged`.
- Only `orders.Service` moves money in the panel; adapters never import `remnactl` or `orders` (`payments` must not import `orders` — no cycle).
- A provider is offered only when its env keys are set; a half-set pair (shop id without secret) is a config error.
- Webhook authenticity: CryptoBot and Heleket by constant-time signature over the raw body; YooKassa by a fresh authenticated API read (the body only supplies the payment id); Stars by Telegram updates plus `pre_checkout_query` checks.
- Webhook answers: 200 handled/duplicate/unknown/irrelevant; 400 unparsable; 401 bad signature; 404 provider unconfigured; 413 body > 64 KiB; 500 only when applying failed in a way worth retrying.
- Provider HTTP clients: total timeout 30 s; response bodies capped at 1 MiB. Secrets never reach logs.
- Bot texts are Russian, HTML parse mode, escaped with `view.Esc`; view functions stay pure.
- No migrations (nothing is in production): new columns go into the `CREATE TABLE`; **recreate the dev database** (`rm data/bot.db*`) after Task 7.
- Commits: **no `Co-Authored-By` / Claude attribution lines** (user rule), even though a system hint asks for one.
- Source of protocol details (read-only reference, never run): `/Users/victor/Downloads/remnawave-bedolaga-telegram-bot-main/app/external/{cryptobot,heleket,yookassa_webhook}.py`, `app/services/payment/{heleket,stars}.py`, `app/services/yookassa_service.py`.

## Review Focus

1. A provider delivers the same webhook twice (or the sweep and the webhook race) → the panel gets exactly one payment. Tests: Task 8 `TestPaidAppliesOnce`, Task 9 `TestPayWebhookDuplicate`.
2. A forged webhook (bad signature; YooKassa body claiming success while its API says `pending`) → nothing is applied. Tests: Tasks 3–5 signature/API-read tests, Task 9 `TestPayWebhookStatusCodes`.
3. Wrong amount (`wrong_amount`, less money, other currency) → never auto-applied; an admin card decides. Test: Task 8 `TestPaidNeedsReview`.
4. Money arrives after the order expired or the customer cancelled → still applied. Test: Task 8 `TestPaidAfterExpiredOrCancelled`.
5. Stars `pre_checkout_query` for someone else's order, an expired order or a changed amount → rejected, no money taken. Tests: Task 8 `TestCheckInvoice`, Task 10 `TestAllowedPreCheckout`.
6. The provider API is down when the customer picks it → no dangling order, a clear error, other methods still work. Test: Task 8 `TestProviderInvoiceFailureCancels`.

---

## File Structure

**New — `internal/payments/`**
- `payments.go` — `Provider`/`WebhookProvider`/`Poller`, `Event`, `Invoice`, errors, `Registry`, shared HTTP helpers.
- `fake.go` — `Fake` provider for tests of other packages.
- `yookassa.go`, `cryptobot.go`, `heleket.go`, `stars.go` — adapters (+ `_test.go` each, `payments_test.go`).

**Modified**
- `internal/config/config.go` (+test) — `PayConfig`.
- `internal/store/store.go` (+test) — four `orders` columns.
- `internal/orders/order.go`, `repo.go`, `service.go` — status, fields, quote methods, `Create` → `CreateWith`, `Cancel`.
- `internal/orders/provider.go` (new, +`provider_test.go`) — `CreateWith`, `Pay`, `issue`, `Paid`, `CheckInvoice`, `SweepProviders`.
- `internal/webapp/server.go`, `orders.go` (+tests) — `/pay/:provider`, `method` + `pay_url`.
- `internal/telegram/private.go`, `bot.go`, `orders.go`, `route.go`; `view/orders.go`, `view/screen.go`, `view/texts.go` (+tests) — Stars events, provider buttons.
- `cmd/bot/main.go`, `.env.example`, `README.md`.
- Web: `src/types.ts`, `src/telegram.ts`, `src/api.ts`, `src/pages/Checkout.tsx`, `src/ui/OrdersList.tsx`, `public/locales/ru/bot.json`.

---

### Task 1: Config — provider credentials

**Files:** Modify `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces — Produces:** `Config.Pay PayConfig`; `PayConfig{YooKassaShopID, YooKassaSecret, YooKassaReturnURL, YooKassaReceiptEmail string; YooKassaVAT int; CryptoBotToken string; CryptoBotTestnet bool; CryptoBotAssets string; HeleketMerchantID, HeleketAPIKey, HeleketToCurrency, HeleketNetwork string; StarsRubRate float64}`; `func (PayConfig) YooKassaOn() bool`, `CryptoBotOn()`, `HeleketOn()`, `StarsOn()`.

- [ ] **Step 1: Failing test** — append to `config_test.go` (it already has `env` and `base`):

```go
func TestPayProviders(t *testing.T) {
	c, err := load(env(base()))
	if err != nil || c.Pay.YooKassaOn() || c.Pay.CryptoBotOn() || c.Pay.HeleketOn() || c.Pay.StarsOn() || c.Pay.YooKassaVAT != 1 {
		t.Fatalf("defaults: %+v %v", c.Pay, err)
	}
	m := base()
	for k, v := range map[string]string{
		"YOOKASSA_SHOP_ID": "123", "YOOKASSA_SECRET_KEY": "sk", "YOOKASSA_VAT_CODE": "4", "YOOKASSA_RECEIPT_EMAIL": "a@b.c",
		"CRYPTOBOT_TOKEN": "tok", "CRYPTOBOT_TESTNET": "true", "CRYPTOBOT_ASSETS": "USDT,TON",
		"HELEKET_MERCHANT_ID": "m", "HELEKET_API_KEY": "k", "HELEKET_TO_CURRENCY": "USDT", "STARS_RUB_RATE": "1.5",
	} {
		m[k] = v
	}
	c, err = load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Pay
	if !p.YooKassaOn() || !p.CryptoBotOn() || !p.HeleketOn() || !p.StarsOn() || p.YooKassaVAT != 4 || !p.CryptoBotTestnet ||
		p.CryptoBotAssets != "USDT,TON" || p.StarsRubRate != 1.5 || p.HeleketToCurrency != "USDT" || p.YooKassaReceiptEmail != "a@b.c" {
		t.Fatalf("configured: %+v", p)
	}
	if p.YooKassaReturnURL != "https://vpn.example.com/bot/" {
		t.Errorf("default return url %q", p.YooKassaReturnURL)
	}
	for _, bad := range []map[string]string{
		{"YOOKASSA_SHOP_ID": "1"}, {"YOOKASSA_SECRET_KEY": "s"}, {"HELEKET_MERCHANT_ID": "m"}, {"HELEKET_API_KEY": "k"},
		{"YOOKASSA_VAT_CODE": "9"}, {"CRYPTOBOT_TESTNET": "maybe"}, {"STARS_RUB_RATE": "-1"}, {"STARS_RUB_RATE": "abc"},
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
```

- [ ] **Step 2: Run** `go test ./internal/config/ -run TestPayProviders` → FAIL (`c.Pay` undefined).
- [ ] **Step 3: Implement.** In `config.go` add the type and methods after `Config`:

```go
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

	StarsRubRate float64 // roubles per star; > 0 enables Stars
}

func (p PayConfig) YooKassaOn() bool  { return p.YooKassaShopID != "" && p.YooKassaSecret != "" }
func (p PayConfig) CryptoBotOn() bool { return p.CryptoBotToken != "" }
func (p PayConfig) HeleketOn() bool   { return p.HeleketMerchantID != "" && p.HeleketAPIKey != "" }
func (p PayConfig) StarsOn() bool     { return p.StarsRubRate > 0 }
```

Add field `Pay PayConfig` to `Config` (after `OrderTTL`). In `load`, right before the `BOT_MODE` check:

```go
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
	if c.Pay.StarsRubRate, err = strconv.ParseFloat(or("STARS_RUB_RATE", "0"), 64); err != nil || c.Pay.StarsRubRate < 0 || math.IsNaN(c.Pay.StarsRubRate) || math.IsInf(c.Pay.StarsRubRate, 0) {
		return Config{}, fmt.Errorf("config: STARS_RUB_RATE must be a number ≥ 0")
	}
```

Add `"math"` to the imports.
- [ ] **Step 4: Run** `go test ./internal/config/` → PASS.
- [ ] **Step 5: Commit** `git add internal/config && git commit -m "feat(config): payment provider credentials"`

---

### Task 2: `internal/payments` core — interfaces, Registry, Fake

**Files:** Create `internal/payments/payments.go`, `internal/payments/fake.go`, `internal/payments/payments_test.go`

**Interfaces — Produces** (package `payments`):

```go
type InvoiceRequest struct{ OrderID, Title string; Amount float64 /* RUB */; ExpiresIn time.Duration }
type Invoice struct{ ExternalID, URL string; Amount float64; Currency string }
type Event struct {
	Provider, OrderID, ExternalID string
	Paid, Review                  bool
	Amount                        float64
	Currency                      string
}
type Provider interface {
	ID() string
	Title() string
	Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error)
}
type WebhookProvider interface {
	Provider
	Webhook(ctx context.Context, h http.Header, body []byte) (ev Event, relevant bool, err error)
}
type Poller interface {
	Provider
	Status(ctx context.Context, externalID string) (Event, error)
}
var ErrBadSignature, ErrBadBody error
type Registry struct{ … }
func NewRegistry(ps ...Provider) *Registry
func (r *Registry) Get(id string) (Provider, bool)
func (r *Registry) Enabled() []Provider
func (r *Registry) IDs() []string
func (r *Registry) Title(id string) string
type Fake struct{ Name, Currency string; Invoices []InvoiceRequest; Err, StatusErr error; Statuses map[string]Event; Hook func(http.Header, []byte) (Event, bool, error) }
```

All `*Registry` methods are nil-safe (a nil registry has no providers).

- [ ] **Step 1: Failing test** `payments_test.go`

```go
package payments

import (
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	a, b := &Fake{Name: "a"}, &Fake{Name: "b"}
	r := NewRegistry(a, b)
	if got := r.IDs(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("ids %v", got)
	}
	if p, ok := r.Get("b"); !ok || p != Provider(b) {
		t.Fatalf("get b: %v %v", p, ok)
	}
	if _, ok := r.Get("zzz"); ok {
		t.Fatal("unknown provider found")
	}
	if len(r.Enabled()) != 2 || r.Title("a") != "Fake pay" || r.Title("zzz") != "" {
		t.Fatalf("enabled %d title %q", len(r.Enabled()), r.Title("a"))
	}
	var none *Registry
	if len(none.Enabled()) != 0 || len(none.IDs()) != 0 || none.Title("a") != "" {
		t.Fatal("nil registry must be empty")
	}
	if _, ok := none.Get("a"); ok {
		t.Fatal("nil registry found a provider")
	}
}

func TestClipAndMoney(t *testing.T) {
	if clip("абвгд", 3) != "абв" || clip("ab", 3) != "ab" {
		t.Error("clip is rune-based")
	}
	if money(1500) != "1500.00" || money(0.1+0.2) != "0.30" {
		t.Errorf("money %q %q", money(1500), money(0.1+0.2))
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/payments/` → FAIL (no package).
- [ ] **Step 3: `payments.go`**

```go
// Package payments talks to online payment providers: it issues invoices,
// verifies the providers' webhooks and polls payment status. It never moves
// money in the panel — internal/orders does that after an Event says money
// arrived — so this package does not import orders.
package payments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrBadSignature = errors.New("payments: bad signature")
	ErrBadBody      = errors.New("payments: bad webhook body")
)

// InvoiceRequest is what a provider needs to bill an order.
type InvoiceRequest struct {
	OrderID   string // our order UUID; comes back in the webhook
	Title     string
	Amount    float64 // roubles
	ExpiresIn time.Duration
}

// Invoice is the provider's answer: where to send the customer and what it asks.
type Invoice struct {
	ExternalID string
	URL        string
	Amount     float64 // in Currency (roubles for fiat providers, stars for Stars)
	Currency   string  // "RUB", "XTR"
}

// Event is a provider's statement about one invoice.
type Event struct {
	Provider   string
	OrderID    string
	ExternalID string
	Paid       bool    // money arrived
	Review     bool    // the provider flags a wrong amount: a human decides
	Amount     float64 // in the invoice's own unit
	Currency   string
}

type Provider interface {
	ID() string // "yookassa"
	Title() string
	Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error)
}

// WebhookProvider receives the provider's callbacks. relevant=false means a
// valid message that needs no action (a status other than paid).
type WebhookProvider interface {
	Provider
	Webhook(ctx context.Context, h http.Header, body []byte) (ev Event, relevant bool, err error)
}

// Poller can be asked about an invoice (lost-webhook recovery).
type Poller interface {
	Provider
	Status(ctx context.Context, externalID string) (Event, error)
}

// Registry is the set of enabled providers, in registration order. All
// methods are safe on a nil Registry.
type Registry struct{ list []Provider }

func NewRegistry(ps ...Provider) *Registry { return &Registry{list: ps} }

func (r *Registry) Get(id string) (Provider, bool) {
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

func (r *Registry) Enabled() []Provider {
	if r == nil {
		return nil
	}
	return r.list
}

func (r *Registry) IDs() []string {
	var out []string
	for _, p := range r.Enabled() {
		out = append(out, p.ID())
	}
	return out
}

// Title is the customer-facing name of a provider ("" when unknown).
func (r *Registry) Title(id string) string {
	if p, ok := r.Get(id); ok {
		return p.Title()
	}
	return ""
}

// defaultClient bounds every provider call: a slow provider must not hold an order.
var defaultClient = &http.Client{Timeout: 30 * time.Second}

func pick(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return defaultClient
}

// apiError is a non-2xx answer from a provider.
type apiError struct {
	Provider string
	Status   int
	Body     string
}

func (e *apiError) Error() string { return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Body) }

func readBody(resp *http.Response) ([]byte, error) { return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func money(v float64) string { return fmt.Sprintf("%.2f", v) }
```

- [ ] **Step 4: `fake.go`**

```go
package payments

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Fake is an in-memory provider for the tests of packages that use providers.
type Fake struct {
	Name      string // default "fake"
	Currency  string // invoice currency, default "RUB"
	Err       error  // Invoice fails with it
	StatusErr error  // Status fails with it
	Statuses  map[string]Event
	Hook      func(h http.Header, body []byte) (Event, bool, error)

	mu       sync.Mutex
	Invoices []InvoiceRequest
}

var (
	_ WebhookProvider = (*Fake)(nil)
	_ Poller          = (*Fake)(nil)
)

func (f *Fake) ID() string {
	if f.Name == "" {
		return "fake"
	}
	return f.Name
}

func (f *Fake) Title() string { return "Fake pay" }

func (f *Fake) Invoice(_ context.Context, in InvoiceRequest) (Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return Invoice{}, f.Err
	}
	f.Invoices = append(f.Invoices, in)
	cur := f.Currency
	if cur == "" {
		cur = "RUB"
	}
	return Invoice{ExternalID: fmt.Sprintf("ext-%d", len(f.Invoices)), URL: "https://pay.test/" + in.OrderID, Amount: in.Amount, Currency: cur}, nil
}

func (f *Fake) Webhook(_ context.Context, h http.Header, body []byte) (Event, bool, error) {
	if f.Hook == nil {
		return Event{}, false, nil
	}
	return f.Hook(h, body)
}

// Status returns the configured result; an unknown invoice is still unpaid.
func (f *Fake) Status(_ context.Context, externalID string) (Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatusErr != nil {
		return Event{}, f.StatusErr
	}
	return f.Statuses[externalID], nil
}
```

- [ ] **Step 5: Run** `go vet ./internal/payments/ && go test ./internal/payments/` → PASS.
- [ ] **Step 6: Commit** `git add internal/payments && git commit -m "feat(payments): provider interfaces, registry and fake"`

---

### Task 3: YooKassa adapter

**Files:** Create `internal/payments/yookassa.go`, `internal/payments/yookassa_test.go`

**Interfaces — Consumes:** Task 2 types. **Produces:** `type YooKassa struct{ ShopID, Secret, ReturnURL, ReceiptEmail string; VAT int; BaseURL string; Client *http.Client }` implementing `WebhookProvider` + `Poller` (`ID() == "yookassa"`, `Title() == "Карта / СБП"`).

- [ ] **Step 1: Failing tests** `yookassa_test.go`

```go
package payments

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ykServer(t *testing.T, h http.HandlerFunc) *YooKassa {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &YooKassa{ShopID: "shop", Secret: "key", ReturnURL: "https://app/bot/", VAT: 1, BaseURL: srv.URL}
}

func TestYooKassaInvoice(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != "POST" || r.URL.Path != "/payments" || !ok || user != "shop" || pass != "key" {
			t.Errorf("request %s %s basic-auth=%v", r.Method, r.URL.Path, ok)
		}
		if r.Header.Get("Idempotence-Key") != "ord-1" {
			t.Errorf("idempotence key %q", r.Header.Get("Idempotence-Key"))
		}
		var b struct {
			Amount       struct{ Value, Currency string }
			Capture      bool
			Confirmation struct {
				Type      string
				ReturnURL string `json:"return_url"`
			}
			Description string
			Metadata    map[string]string
			Receipt     *struct {
				Customer struct{ Email string }
				Items    []struct {
					VATCode int `json:"vat_code"`
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		if b.Amount.Value != "800.00" || b.Amount.Currency != "RUB" || !b.Capture || b.Confirmation.Type != "redirect" ||
			b.Confirmation.ReturnURL != "https://app/bot/" || b.Metadata["order_id"] != "ord-1" || b.Description != "Продление" {
			t.Errorf("body %+v", b)
		}
		if b.Receipt != nil {
			t.Errorf("receipt without an e-mail: %+v", b.Receipt)
		}
		w.Write([]byte(`{"id":"p1","status":"pending","confirmation":{"type":"redirect","confirmation_url":"https://yoomoney.ru/pay/p1"}}`))
	})
	inv, err := y.Invoice(t.Context(), InvoiceRequest{OrderID: "ord-1", Title: "Продление", Amount: 800})
	if err != nil || inv.ExternalID != "p1" || inv.URL != "https://yoomoney.ru/pay/p1" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestYooKassaInvoiceReceipt(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Receipt struct {
				Customer struct{ Email string }
				Items    []struct {
					VATCode        int    `json:"vat_code"`
					PaymentMode    string `json:"payment_mode"`
					PaymentSubject string `json:"payment_subject"`
					Quantity       string
				}
			}
		}
		json.NewDecoder(r.Body).Decode(&b)
		if b.Receipt.Customer.Email != "a@b.c" || len(b.Receipt.Items) != 1 || b.Receipt.Items[0].VATCode != 4 ||
			b.Receipt.Items[0].PaymentMode != "full_payment" || b.Receipt.Items[0].PaymentSubject != "service" || b.Receipt.Items[0].Quantity != "1.00" {
			t.Errorf("receipt %+v", b.Receipt)
		}
		w.Write([]byte(`{"id":"p1","confirmation":{"confirmation_url":"https://x"}}`))
	})
	y.ReceiptEmail, y.VAT = "a@b.c", 4
	if _, err := y.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestYooKassaInvoiceErrors(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"type":"error","code":"invalid_request"}`))
	})
	if _, err := y.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error for HTTP 400")
	}
	y2 := ykServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":"p1"}`)) })
	if _, err := y2.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error for a response without a confirmation url")
	}
}

const ykPaid = `{"id":"p1","status":"succeeded","paid":true,"amount":{"value":"800.00","currency":"RUB"},"metadata":{"order_id":"ord-1"}}`

func TestYooKassaWebhook(t *testing.T) {
	paid := true
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/payments/p1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if paid {
			w.Write([]byte(ykPaid))
			return
		}
		w.Write([]byte(`{"id":"p1","status":"pending","paid":false,"amount":{"value":"800.00","currency":"RUB"},"metadata":{"order_id":"ord-1"}}`))
	})
	ctx := t.Context()
	body := []byte(`{"type":"notification","event":"payment.succeeded","object":{"id":"p1","status":"succeeded"}}`)

	ev, relevant, err := y.Webhook(ctx, nil, body)
	if err != nil || !relevant || !ev.Paid || ev.OrderID != "ord-1" || ev.ExternalID != "p1" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "yookassa" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}

	// Forged body: it claims success, the API says pending → nothing to do.
	paid = false
	if _, relevant, err := y.Webhook(ctx, nil, body); err != nil || relevant {
		t.Errorf("forged: relevant=%v err=%v", relevant, err)
	}
	// Other events and a missing id are not relevant / not valid.
	if _, relevant, err := y.Webhook(ctx, nil, []byte(`{"event":"payment.canceled","object":{"id":"p1"}}`)); err != nil || relevant {
		t.Errorf("canceled: relevant=%v err=%v", relevant, err)
	}
	if _, _, err := y.Webhook(ctx, nil, []byte(`{"event":"payment.succeeded","object":{}}`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("no id: %v", err)
	}
	if _, _, err := y.Webhook(ctx, nil, []byte(`not json`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("garbage: %v", err)
	}
}

func TestYooKassaWebhookAPIDown(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, relevant, err := y.Webhook(t.Context(), nil, []byte(`{"event":"payment.succeeded","object":{"id":"p1"}}`))
	if err == nil || relevant {
		t.Fatalf("an unverifiable payment must fail (so the provider retries): relevant=%v err=%v", relevant, err)
	}
}

func TestYooKassaStatus(t *testing.T) {
	y := ykServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(ykPaid)) })
	ev, err := y.Status(t.Context(), "p1")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" {
		t.Fatalf("%+v %v", ev, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/payments/ -run YooKassa` → FAIL (undefined).
- [ ] **Step 3: `yookassa.go`**

```go
package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const yooKassaAPI = "https://api.yookassa.ru/v3"

// YooKassa: cards and SBP in roubles. The webhook body is never trusted — the
// payment is re-read from the API (Bedolaga does the same: its IP check is only
// a first barrier, the API read is the authority).
type YooKassa struct {
	ShopID, Secret string
	ReturnURL      string
	ReceiptEmail   string // fiscal receipt customer; no receipt when empty
	VAT            int
	BaseURL        string // tests; default https://api.yookassa.ru/v3
	Client         *http.Client
}

var (
	_ WebhookProvider = (*YooKassa)(nil)
	_ Poller          = (*YooKassa)(nil)
)

func (y *YooKassa) ID() string    { return "yookassa" }
func (y *YooKassa) Title() string { return "Карта / СБП" }

type ykPayment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Paid   bool   `json:"paid"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Metadata     map[string]string `json:"metadata"`
	Confirmation struct {
		URL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

func (p ykPayment) event() Event {
	v, _ := strconv.ParseFloat(p.Amount.Value, 64)
	return Event{Provider: "yookassa", OrderID: p.Metadata["order_id"], ExternalID: p.ID,
		Paid: p.Status == "succeeded" && p.Paid, Amount: v, Currency: p.Amount.Currency}
}

func (y *YooKassa) do(ctx context.Context, method, path, idempotence string, in, out any) error {
	base := y.BaseURL
	if base == "" {
		base = yooKassaAPI
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(y.ShopID, y.Secret)
	req.Header.Set("Content-Type", "application/json")
	if idempotence != "" {
		req.Header.Set("Idempotence-Key", idempotence)
	}
	resp, err := pick(y.Client).Do(req)
	if err != nil {
		return fmt.Errorf("yookassa: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("yookassa: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"yookassa", resp.StatusCode, clip(string(data), 300)}
	}
	return json.Unmarshal(data, out)
}

func (y *YooKassa) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	amount := map[string]string{"value": money(in.Amount), "currency": "RUB"}
	req := map[string]any{
		"amount":       amount,
		"capture":      true,
		"confirmation": map[string]string{"type": "redirect", "return_url": y.ReturnURL},
		"description":  clip(in.Title, 128),
		"metadata":     map[string]string{"order_id": in.OrderID},
	}
	if y.ReceiptEmail != "" {
		req["receipt"] = map[string]any{
			"customer": map[string]string{"email": y.ReceiptEmail},
			"items": []map[string]any{{
				"description": clip(in.Title, 128), "quantity": "1.00", "amount": amount,
				"vat_code": y.VAT, "payment_mode": "full_payment", "payment_subject": "service",
			}},
		}
	}
	var p ykPayment
	// The order id as the idempotence key: re-issuing the same order returns the same payment.
	if err := y.do(ctx, http.MethodPost, "/payments", in.OrderID, req, &p); err != nil {
		return Invoice{}, err
	}
	if p.ID == "" || p.Confirmation.URL == "" {
		return Invoice{}, fmt.Errorf("yookassa: no confirmation url in the response")
	}
	return Invoice{ExternalID: p.ID, URL: p.Confirmation.URL, Amount: in.Amount, Currency: "RUB"}, nil
}

func (y *YooKassa) get(ctx context.Context, id string) (ykPayment, error) {
	var p ykPayment
	err := y.do(ctx, http.MethodGet, "/payments/"+id, "", nil, &p)
	return p, err
}

func (y *YooKassa) Status(ctx context.Context, externalID string) (Event, error) {
	p, err := y.get(ctx, externalID)
	return p.event(), err
}

func (y *YooKassa) Webhook(ctx context.Context, _ http.Header, body []byte) (Event, bool, error) {
	var n struct {
		Event  string `json:"event"`
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	if n.Event != "payment.succeeded" {
		return Event{}, false, nil
	}
	if n.Object.ID == "" {
		return Event{}, false, fmt.Errorf("%w: no payment id", ErrBadBody)
	}
	p, err := y.get(ctx, n.Object.ID) // the authority: ignore everything else in the body
	if err != nil {
		return Event{}, false, err
	}
	ev := p.event()
	return ev, ev.Paid, nil
}
```

- [ ] **Step 4: Run** `go vet ./internal/payments/ && go test ./internal/payments/` → PASS.
- [ ] **Step 5: Commit** `git add internal/payments && git commit -m "feat(payments): YooKassa adapter"`

---

### Task 4: CryptoBot adapter

**Files:** Create `internal/payments/cryptobot.go`, `internal/payments/cryptobot_test.go`

**Interfaces — Produces:** `type CryptoBot struct{ Token string; Testnet bool; Assets string; BaseURL string; Client *http.Client }` implementing `WebhookProvider` + `Poller` (`ID() == "cryptobot"`, `Title() == "Криптовалюта (CryptoBot)"`).

- [ ] **Step 1: Failing tests** `cryptobot_test.go`

```go
package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func cbServer(t *testing.T, h http.HandlerFunc) *CryptoBot {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &CryptoBot{Token: "123:AAtoken", BaseURL: srv.URL}
}

// cbSig is the documented signature: HMAC-SHA256 of the raw body, keyed with SHA256(token).
func cbSig(token string, body []byte) string {
	key := sha256.Sum256([]byte(token))
	m := hmac.New(sha256.New, key[:])
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func TestCryptoBotInvoice(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/createInvoice" || r.Header.Get("Crypto-Pay-API-Token") != "123:AAtoken" {
			t.Errorf("request %s %s token=%q", r.Method, r.URL.Path, r.Header.Get("Crypto-Pay-API-Token"))
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["currency_type"] != "fiat" || b["fiat"] != "RUB" || b["amount"] != "800.00" || b["payload"] != "ord-1" ||
			b["expires_in"] != float64(3600) || b["accepted_assets"] != "USDT,TON" || b["description"] != "Продление" {
			t.Errorf("body %v", b)
		}
		w.Write([]byte(`{"ok":true,"result":{"invoice_id":77,"status":"active","bot_invoice_url":"https://t.me/CryptoBot?start=IVabc"}}`))
	})
	c.Assets = "USDT,TON"
	inv, err := c.Invoice(t.Context(), InvoiceRequest{OrderID: "ord-1", Title: "Продление", Amount: 800, ExpiresIn: time.Hour})
	if err != nil || inv.ExternalID != "77" || inv.URL != "https://t.me/CryptoBot?start=IVabc" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestCryptoBotInvoiceErrors(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error":{"code":400,"name":"AMOUNT_TOO_SMALL"}}`))
	})
	if _, err := c.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err == nil {
		t.Fatal("want an error when ok=false")
	}
}

func TestCryptoBotTestnetBase(t *testing.T) {
	if (&CryptoBot{Testnet: true}).base() != "https://testnet-pay.crypt.bot/api" || (&CryptoBot{}).base() != "https://pay.crypt.bot/api" {
		t.Error("base urls")
	}
}

const cbPaid = `{"update_id":1,"update_type":"invoice_paid","request_date":"2026-10-07T12:00:00Z","payload":{"invoice_id":77,"status":"paid","currency_type":"fiat","fiat":"RUB","amount":"800.00","payload":"ord-1"}}`

func TestCryptoBotWebhook(t *testing.T) {
	c := &CryptoBot{Token: "123:AAtoken"}
	hdr := func(body []byte) http.Header {
		h := http.Header{}
		h.Set("crypto-pay-api-signature", cbSig("123:AAtoken", body))
		return h
	}
	body := []byte(cbPaid)
	ev, relevant, err := c.Webhook(t.Context(), hdr(body), body)
	if err != nil || !relevant || !ev.Paid || ev.OrderID != "ord-1" || ev.ExternalID != "77" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "cryptobot" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}
	// Forged: wrong signature, missing signature, altered body under the old signature.
	bad := http.Header{}
	bad.Set("crypto-pay-api-signature", cbSig("other-token", body))
	for name, h := range map[string]http.Header{"wrong key": bad, "missing": {}} {
		if _, _, err := c.Webhook(t.Context(), h, body); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	tampered := []byte(`{"update_type":"invoice_paid","payload":{"invoice_id":77,"status":"paid","fiat":"RUB","amount":"1.00","payload":"ord-1"}}`)
	if _, _, err := c.Webhook(t.Context(), hdr(body), tampered); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered: %v", err)
	}
	// Signed but not a payment, or not JSON.
	other := []byte(`{"update_type":"something_else","payload":{}}`)
	if _, relevant, err := c.Webhook(t.Context(), hdr(other), other); err != nil || relevant {
		t.Errorf("other update: relevant=%v err=%v", relevant, err)
	}
	junk := []byte(`nope`)
	if _, _, err := c.Webhook(t.Context(), hdr(junk), junk); !errors.Is(err, ErrBadBody) {
		t.Errorf("junk: %v", err)
	}
}

func TestCryptoBotStatus(t *testing.T) {
	c := cbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/getInvoices" || r.URL.Query().Get("invoice_ids") != "77" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		w.Write([]byte(`{"ok":true,"result":{"items":[{"invoice_id":77,"status":"paid","fiat":"RUB","amount":"800.00","payload":"ord-1"}]}}`))
	})
	ev, err := c.Status(t.Context(), "77")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" || ev.Amount != 800 {
		t.Fatalf("%+v %v", ev, err)
	}
	empty := cbServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true,"result":{"items":[]}}`)) })
	if _, err := empty.Status(t.Context(), "77"); err == nil {
		t.Fatal("want an error for an unknown invoice")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/payments/ -run CryptoBot` → FAIL.
- [ ] **Step 3: `cryptobot.go`**

```go
package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CryptoBot (Crypto Pay API): invoices are issued in fiat (RUB); the customer
// pays in USDT/TON/… and CryptoBot converts.
type CryptoBot struct {
	Token   string
	Testnet bool
	Assets  string // accepted assets, e.g. "USDT,TON"; empty: all
	BaseURL string // tests
	Client  *http.Client
}

var (
	_ WebhookProvider = (*CryptoBot)(nil)
	_ Poller          = (*CryptoBot)(nil)
)

func (c *CryptoBot) ID() string    { return "cryptobot" }
func (c *CryptoBot) Title() string { return "Криптовалюта (CryptoBot)" }

func (c *CryptoBot) base() string {
	switch {
	case c.BaseURL != "":
		return c.BaseURL
	case c.Testnet:
		return "https://testnet-pay.crypt.bot/api"
	}
	return "https://pay.crypt.bot/api"
}

type cbInvoice struct {
	InvoiceID     int64  `json:"invoice_id"`
	Status        string `json:"status"`
	Fiat          string `json:"fiat"`
	Asset         string `json:"asset"`
	Amount        string `json:"amount"`
	Payload       string `json:"payload"`
	BotInvoiceURL string `json:"bot_invoice_url"`
}

func (i cbInvoice) event() Event {
	amount, _ := strconv.ParseFloat(i.Amount, 64)
	cur := i.Fiat
	if cur == "" {
		cur = i.Asset
	}
	return Event{Provider: "cryptobot", OrderID: i.Payload, ExternalID: strconv.FormatInt(i.InvoiceID, 10),
		Paid: i.Status == "paid", Amount: amount, Currency: cur}
}

type cbReply[T any] struct {
	OK     bool `json:"ok"`
	Result T    `json:"result"`
	Error  any  `json:"error"`
}

func (c *CryptoBot) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Crypto-Pay-API-Token", c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pick(c.Client).Do(req)
	if err != nil {
		return fmt.Errorf("cryptobot: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("cryptobot: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &apiError{"cryptobot", resp.StatusCode, clip(string(data), 300)}
	}
	return json.Unmarshal(data, out)
}

func (c *CryptoBot) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	req := map[string]any{
		"currency_type": "fiat", "fiat": "RUB", "amount": money(in.Amount),
		"description": clip(in.Title, 1024), "payload": in.OrderID,
	}
	if secs := int(in.ExpiresIn.Seconds()); secs > 0 {
		req["expires_in"] = min(secs, 2678400) // the API's maximum is 28 days
	}
	if c.Assets != "" {
		req["accepted_assets"] = c.Assets
	}
	var r cbReply[cbInvoice]
	if err := c.call(ctx, http.MethodPost, "/createInvoice", req, &r); err != nil {
		return Invoice{}, err
	}
	if !r.OK || r.Result.InvoiceID == 0 || r.Result.BotInvoiceURL == "" {
		return Invoice{}, fmt.Errorf("cryptobot: createInvoice failed: %v", r.Error)
	}
	return Invoice{ExternalID: strconv.FormatInt(r.Result.InvoiceID, 10), URL: r.Result.BotInvoiceURL, Amount: in.Amount, Currency: "RUB"}, nil
}

func (c *CryptoBot) Status(ctx context.Context, externalID string) (Event, error) {
	var r cbReply[struct {
		Items []cbInvoice `json:"items"`
	}]
	if err := c.call(ctx, http.MethodGet, "/getInvoices?"+url.Values{"invoice_ids": {externalID}}.Encode(), nil, &r); err != nil {
		return Event{}, err
	}
	if !r.OK || len(r.Result.Items) == 0 {
		return Event{}, fmt.Errorf("cryptobot: invoice %s not found", externalID)
	}
	return r.Result.Items[0].event(), nil
}

// validSignature: HMAC-SHA256 of the raw body, key = SHA256(api token) (Crypto Pay docs).
func (c *CryptoBot) validSignature(sig string, body []byte) bool {
	if sig == "" || c.Token == "" {
		return false
	}
	key := sha256.Sum256([]byte(c.Token))
	m := hmac.New(sha256.New, key[:])
	m.Write(body)
	return hmac.Equal([]byte(strings.ToLower(sig)), []byte(hex.EncodeToString(m.Sum(nil))))
}

func (c *CryptoBot) Webhook(_ context.Context, h http.Header, body []byte) (Event, bool, error) {
	if !c.validSignature(h.Get("crypto-pay-api-signature"), body) {
		return Event{}, false, ErrBadSignature
	}
	var u struct {
		UpdateType string    `json:"update_type"`
		Payload    cbInvoice `json:"payload"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	if u.UpdateType != "invoice_paid" {
		return Event{}, false, nil
	}
	return u.Payload.event(), true, nil
}
```

- [ ] **Step 4: Run** `go vet ./internal/payments/ && go test ./internal/payments/` → PASS.
- [ ] **Step 5: Commit** `git add internal/payments && git commit -m "feat(payments): CryptoBot adapter"`

---

### Task 5: Heleket adapter

**Files:** Create `internal/payments/heleket.go`, `internal/payments/heleket_test.go`

**Interfaces — Produces:** `type Heleket struct{ MerchantID, APIKey, ToCurrency, Network, CallbackURL, BaseURL string; Client *http.Client }` implementing `WebhookProvider` + `Poller` (`ID() == "heleket"`, `Title() == "Криптовалюта (Heleket)"`).

Signature (from Bedolaga `app/external/heleket.py`): `sign = hex(md5(base64(body) + api_key))`. Requests: sign the exact bytes sent. Webhooks: the `sign` field is inside the JSON; the signed string is the body **without** `sign`, compact, with `/` escaped as `\/` (PHP's default). Because we cannot test against the live service, verification accepts either slash form and keeps each value's original text (so `\uXXXX` escapes and number formats survive).

- [ ] **Step 1: Failing tests** `heleket_test.go`

```go
package payments

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func hkServer(t *testing.T, h http.HandlerFunc) *Heleket {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Heleket{MerchantID: "merch-uuid", APIKey: "secret", BaseURL: srv.URL, CallbackURL: "https://bot.example.com/pay/heleket"}
}

func hkMD5(signed, key string) string {
	sum := md5.Sum([]byte(base64.StdEncoding.EncodeToString([]byte(signed)) + key))
	return hex.EncodeToString(sum[:])
}

func TestHeleketInvoice(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/payment" || r.Header.Get("merchant") != "merch-uuid" {
			t.Errorf("request %s %s merchant=%q", r.Method, r.URL.Path, r.Header.Get("merchant"))
		}
		if r.Header.Get("sign") != hkMD5(string(raw), "secret") {
			t.Errorf("sign %q does not match the body", r.Header.Get("sign"))
		}
		var b map[string]any
		json.Unmarshal(raw, &b)
		if b["amount"] != "800.00" || b["currency"] != "RUB" || b["order_id"] != "ord-1" || b["lifetime"] != float64(43200) ||
			b["url_callback"] != "https://bot.example.com/pay/heleket" || b["to_currency"] != "USDT" || b["network"] != "tron" {
			t.Errorf("body %v", b)
		}
		w.Write([]byte(`{"state":0,"result":{"uuid":"hk-1","order_id":"ord-1","url":"https://pay.heleket.com/pay/hk-1","payment_status":"check"}}`))
	})
	h.ToCurrency, h.Network = "USDT", "tron"
	inv, err := h.Invoice(t.Context(), InvoiceRequest{OrderID: "ord-1", Title: "t", Amount: 800, ExpiresIn: 48 * time.Hour}) // lifetime is clamped to 12 h
	if err != nil || inv.ExternalID != "hk-1" || inv.URL != "https://pay.heleket.com/pay/hk-1" || inv.Amount != 800 || inv.Currency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
}

func TestHeleketInvoiceErrors(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"state":1,"message":"bad merchant"}`)) })
	if _, err := h.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 1}); err == nil || !strings.Contains(err.Error(), "bad merchant") {
		t.Fatalf("want the provider's message, got %v", err)
	}
}

// hookBody is a webhook as PHP would send it: slashes escaped, sign in the middle.
func hookBody(status, key string) []byte {
	signed := `{"type":"payment","uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","url":"https:\/\/pay.heleket.com\/pay\/hk-1","payment_status":"` + status + `","note":"Привет"}`
	sign := hkMD5(signed, key)
	return []byte(`{ "type":"payment", "uuid":"hk-1", "order_id":"ord-1", "sign":"` + sign + `", "amount":"800.00", "currency":"RUB", ` +
		`"url":"https:\/\/pay.heleket.com\/pay\/hk-1", "payment_status":"` + status + `", "note":"Привет" }`)
}

func TestHeleketWebhook(t *testing.T) {
	h := &Heleket{MerchantID: "m", APIKey: "secret"}
	ctx := t.Context()

	ev, relevant, err := h.Webhook(ctx, nil, hookBody("paid", "secret"))
	if err != nil || !relevant || !ev.Paid || ev.Review || ev.OrderID != "ord-1" || ev.ExternalID != "hk-1" || ev.Amount != 800 || ev.Currency != "RUB" || ev.Provider != "heleket" {
		t.Fatalf("paid: %+v %v %v", ev, relevant, err)
	}
	if ev, relevant, _ := h.Webhook(ctx, nil, hookBody("paid_over", "secret")); !relevant || !ev.Paid {
		t.Errorf("paid_over is paid: %+v", ev)
	}
	if ev, relevant, _ := h.Webhook(ctx, nil, hookBody("wrong_amount", "secret")); !relevant || ev.Paid || !ev.Review {
		t.Errorf("wrong_amount needs a human: %+v", ev)
	}
	for _, st := range []string{"check", "cancel", "fail", "system_fail", "process"} {
		if _, relevant, err := h.Webhook(ctx, nil, hookBody(st, "secret")); err != nil || relevant {
			t.Errorf("%s: relevant=%v err=%v", st, relevant, err)
		}
	}
	// The unescaped-slash form of the signature is accepted too.
	plain := `{"type":"payment","uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","payment_status":"paid","url":"https://pay.heleket.com/x"}`
	body := []byte(`{"sign":"` + hkMD5(plain, "secret") + `",` + plain[1:])
	if _, relevant, err := h.Webhook(ctx, nil, body); err != nil || !relevant {
		t.Errorf("unescaped form: relevant=%v err=%v", relevant, err)
	}
	// Forged.
	if _, _, err := h.Webhook(ctx, nil, hookBody("paid", "other-key")); !errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong key: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(strings.Replace(string(hookBody("paid", "secret")), `"800.00"`, `"1.00"`, 1))); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered amount: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(`{"payment_status":"paid"}`)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("no sign: %v", err)
	}
	if _, _, err := h.Webhook(ctx, nil, []byte(`nope`)); !errors.Is(err, ErrBadBody) {
		t.Errorf("junk: %v", err)
	}
}

func TestHeleketWebhookIgnoresPayouts(t *testing.T) {
	h := &Heleket{MerchantID: "m", APIKey: "secret"}
	signed := `{"type":"payout","uuid":"x","payment_status":"paid"}`
	body := []byte(`{"sign":"` + hkMD5(signed, "secret") + `",` + signed[1:])
	if _, relevant, err := h.Webhook(t.Context(), nil, body); err != nil || relevant {
		t.Errorf("payout: relevant=%v err=%v", relevant, err)
	}
}

func TestHeleketStatus(t *testing.T) {
	h := hkServer(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/payment/info" || b["uuid"] != "hk-1" {
			t.Errorf("request %s %v", r.URL.Path, b)
		}
		w.Write([]byte(`{"state":0,"result":{"uuid":"hk-1","order_id":"ord-1","amount":"800.00","currency":"RUB","payment_status":"paid"}}`))
	})
	ev, err := h.Status(t.Context(), "hk-1")
	if err != nil || !ev.Paid || ev.OrderID != "ord-1" || ev.Amount != 800 {
		t.Fatalf("%+v %v", ev, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/payments/ -run Heleket` → FAIL.
- [ ] **Step 3: `heleket.go`**

```go
package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const heleketAPI = "https://api.heleket.com/v1"

// Heleket: multi-chain crypto. Invoices are issued in RUB; Heleket converts.
type Heleket struct {
	MerchantID, APIKey  string
	ToCurrency, Network string // optional fixed asset / network
	CallbackURL         string // where Heleket posts status changes
	BaseURL             string // tests; default https://api.heleket.com/v1
	Client              *http.Client
}

var (
	_ WebhookProvider = (*Heleket)(nil)
	_ Poller          = (*Heleket)(nil)
)

func (h *Heleket) ID() string    { return "heleket" }
func (h *Heleket) Title() string { return "Криптовалюта (Heleket)" }

// hkSign is hex(md5(base64(body) + api key)).
func hkSign(signed []byte, key string) string {
	sum := md5.Sum([]byte(base64.StdEncoding.EncodeToString(signed) + key))
	return hex.EncodeToString(sum[:])
}

type hkPayment struct {
	Type          string `json:"type"`
	UUID          string `json:"uuid"`
	OrderID       string `json:"order_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	URL           string `json:"url"`
	PaymentStatus string `json:"payment_status"`
	Status        string `json:"status"`
}

func (p hkPayment) state() string {
	if p.PaymentStatus != "" {
		return p.PaymentStatus
	}
	return p.Status
}

func (p hkPayment) event() Event {
	amount, _ := strconv.ParseFloat(p.Amount, 64)
	st := p.state()
	return Event{Provider: "heleket", OrderID: p.OrderID, ExternalID: p.UUID,
		Paid:   st == "paid" || st == "paid_over",
		Review: st == "wrong_amount" || st == "wrong_amount_waiting",
		Amount: amount, Currency: p.Currency}
}

func (h *Heleket) call(ctx context.Context, path string, in map[string]any) (hkPayment, error) {
	base := h.BaseURL
	if base == "" {
		base = heleketAPI
	}
	body, err := json.Marshal(in) // the exact bytes are signed and sent
	if err != nil {
		return hkPayment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return hkPayment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("merchant", h.MerchantID)
	req.Header.Set("sign", hkSign(body, h.APIKey))
	resp, err := pick(h.Client).Do(req)
	if err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return hkPayment{}, &apiError{"heleket", resp.StatusCode, clip(string(data), 300)}
	}
	var r struct {
		State   int       `json:"state"`
		Message string    `json:"message"`
		Result  hkPayment `json:"result"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return hkPayment{}, fmt.Errorf("heleket: %w", err)
	}
	if r.State != 0 {
		return hkPayment{}, fmt.Errorf("heleket: state %d: %s", r.State, clip(r.Message, 200))
	}
	return r.Result, nil
}

func (h *Heleket) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	// Heleket accepts lifetimes of 5 minutes … 12 hours.
	lifetime := int(min(max(in.ExpiresIn, 5*time.Minute), 12*time.Hour).Seconds())
	req := map[string]any{"amount": money(in.Amount), "currency": "RUB", "order_id": in.OrderID, "lifetime": lifetime}
	if h.CallbackURL != "" {
		req["url_callback"] = h.CallbackURL
	}
	if h.ToCurrency != "" {
		req["to_currency"] = h.ToCurrency
	}
	if h.Network != "" {
		req["network"] = h.Network
	}
	p, err := h.call(ctx, "/payment", req)
	if err != nil {
		return Invoice{}, err
	}
	if p.UUID == "" || p.URL == "" {
		return Invoice{}, fmt.Errorf("heleket: no uuid/url in the response")
	}
	return Invoice{ExternalID: p.UUID, URL: p.URL, Amount: in.Amount, Currency: "RUB"}, nil
}

func (h *Heleket) Status(ctx context.Context, externalID string) (Event, error) {
	p, err := h.call(ctx, "/payment/info", map[string]any{"uuid": externalID})
	return p.event(), err
}

// withoutSign is the webhook body minus the "sign" member: compact, members in
// their original order, each value in its original text (so \uXXXX escapes and
// number formats survive), which is what the sender signed up to slash escaping.
func withoutSign(body []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return "", errors.New("not an object")
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return "", err
		}
		key, ok := kt.(string)
		if !ok {
			return "", errors.New("bad key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", err
		}
		if key == "sign" {
			continue
		}
		var v bytes.Buffer
		if err := json.Compact(&v, raw); err != nil {
			return "", err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v.Bytes())
	}
	b.WriteByte('}')
	return b.String(), nil
}

// validSign accepts the signature over the escaped-slash form (PHP's default,
// what Bedolaga computes) and over the plain-slash form.
func (h *Heleket) validSign(body []byte, got string) bool {
	if got == "" || h.APIKey == "" {
		return false
	}
	rest, err := withoutSign(body)
	if err != nil {
		return false
	}
	plain := strings.ReplaceAll(rest, `\/`, `/`)
	for _, signed := range []string{strings.ReplaceAll(plain, `/`, `\/`), plain} {
		if hmac.Equal([]byte(strings.ToLower(got)), []byte(hkSign([]byte(signed), h.APIKey))) {
			return true
		}
	}
	return false
}

func (h *Heleket) Webhook(_ context.Context, _ http.Header, body []byte) (Event, bool, error) {
	var p hkPayment
	var s struct {
		Sign string `json:"sign"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, false, fmt.Errorf("%w: %v", ErrBadBody, err)
	}
	_ = json.Unmarshal(body, &s)
	if !h.validSign(body, s.Sign) {
		return Event{}, false, ErrBadSignature
	}
	if p.Type != "" && p.Type != "payment" {
		return Event{}, false, nil // e.g. payout notifications
	}
	ev := p.event()
	return ev, ev.Paid || ev.Review, nil
}
```

- [ ] **Step 4: Run** `go vet ./internal/payments/ && go test ./internal/payments/` → PASS. (If `TestHeleketInvoice`'s `lifetime` assertion fails, the clamp is wrong: 48 h must become 43200 s.)
- [ ] **Step 5: Commit** `git add internal/payments && git commit -m "feat(payments): Heleket adapter"`

---

### Task 6: Telegram Stars adapter

**Files:** Create `internal/payments/stars.go`, `internal/payments/stars_test.go`

**Interfaces — Produces:** `type LinkFunc func(ctx context.Context, title, description, payload string, stars int) (string, error)`; `type Stars struct{ RubPerStar float64; Link LinkFunc }` implementing `Provider` only (`ID() == "stars"`, `Title() == "Telegram Stars ⭐"`); `func (s *Stars) Count(rub float64) int`. The bot supplies `Link` after it is constructed (Task 10/12).

- [ ] **Step 1: Failing tests** `stars_test.go`

```go
package payments

import (
	"context"
	"errors"
	"testing"
)

func TestStarsCount(t *testing.T) {
	s := &Stars{RubPerStar: 1.5}
	for rub, want := range map[float64]int{150: 100, 100.01: 67, 1: 1, 0.5: 1, 800: 534, 1.5: 1} {
		if got := s.Count(rub); got != want {
			t.Errorf("Count(%v) = %d, want %d", rub, got, want)
		}
	}
}

func TestStarsInvoice(t *testing.T) {
	var gotTitle, gotDesc, gotPayload string
	var gotStars int
	s := &Stars{RubPerStar: 2, Link: func(_ context.Context, title, desc, payload string, stars int) (string, error) {
		gotTitle, gotDesc, gotPayload, gotStars = title, desc, payload, stars
		return "https://t.me/$abc", nil
	}}
	inv, err := s.Invoice(t.Context(), InvoiceRequest{OrderID: "ord-1", Title: "Продление «Дом» · 3 мес.", Amount: 800})
	if err != nil || inv.URL != "https://t.me/$abc" || inv.Amount != 400 || inv.Currency != "XTR" || inv.ExternalID != "" {
		t.Fatalf("%+v %v", inv, err)
	}
	if gotPayload != "ord-1" || gotStars != 400 || gotTitle == "" || len([]rune(gotTitle)) > 32 || gotDesc != "Продление «Дом» · 3 мес." {
		t.Errorf("link args: %q %q %q %d", gotTitle, gotDesc, gotPayload, gotStars)
	}
	s.Link = func(context.Context, string, string, string, int) (string, error) { return "", errors.New("telegram down") }
	if _, err := s.Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 10}); err == nil {
		t.Error("want the link error")
	}
	if _, err := (&Stars{RubPerStar: 2}).Invoice(t.Context(), InvoiceRequest{OrderID: "o", Title: "t", Amount: 10}); err == nil {
		t.Error("want an error before the bot is wired")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/payments/ -run Stars` → FAIL.
- [ ] **Step 3: `stars.go`**

```go
package payments

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// LinkFunc creates a Telegram invoice link in stars (the bot's createInvoiceLink).
type LinkFunc func(ctx context.Context, title, description, payload string, stars int) (string, error)

// Stars is Telegram's own currency: no merchant account and no webhook — the
// bot receives pre_checkout_query and successful_payment updates itself.
type Stars struct {
	RubPerStar float64
	Link       LinkFunc // set once the bot exists
}

var _ Provider = (*Stars)(nil)

func (s *Stars) ID() string    { return "stars" }
func (s *Stars) Title() string { return "Telegram Stars ⭐" }

// Count is the stars an amount in roubles costs, rounded up (the epsilon
// absorbs float noise such as 100.00000000000001).
func (s *Stars) Count(rub float64) int { return int(math.Ceil(rub/s.RubPerStar - 1e-9)) }

func (s *Stars) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if s.Link == nil {
		return Invoice{}, errors.New("stars: bot is not ready")
	}
	n := max(s.Count(in.Amount), 1)
	link, err := s.Link(ctx, "Оплата VPN", clip(in.Title, 255), in.OrderID, n)
	if err != nil {
		return Invoice{}, fmt.Errorf("stars: %w", err)
	}
	return Invoice{URL: link, Amount: float64(n), Currency: "XTR"}, nil
}
```

- [ ] **Step 4: Run** `go vet ./internal/payments/ && go test ./internal/payments/` → PASS.
- [ ] **Step 5: Commit** `git add internal/payments && git commit -m "feat(payments): Telegram Stars adapter"`

---

### Task 7: Order data model — status, columns, quote methods

**Files:** Modify `internal/store/store.go`, `internal/store/store_test.go`, `internal/orders/order.go`, `internal/orders/repo.go`, `internal/orders/service.go`, `internal/telegram/view/orders.go`; Test `internal/orders/service_test.go`

**Interfaces — Produces:** `orders.StatusAwaitingPayment = "awaiting_payment"`; `func (m Method) IsProvider() bool`; `Order{…, ExternalID, PayURL string; PayAmount float64; PayCurrency string}`; `Quote.Titles map[string]string`; `Service.Providers *payments.Registry`; `ErrProvider`.

- [ ] **Step 1: Failing tests.** Append to `internal/orders/service_test.go` (add imports `payments` and `slices` if missing):

```go
func TestQuoteListsProviders(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	r := Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3}

	q, err := s.Quote(ctx, c, r)
	if err != nil || !slices.Equal(q.Methods, []Method{MethodManual}) || len(q.Titles) != 0 {
		t.Fatalf("no providers: %+v %v", q, err)
	}
	s.Providers = payments.NewRegistry(&payments.Fake{Name: "fake"})
	q, _ = s.Quote(ctx, c, r)
	if !slices.Equal(q.Methods, []Method{MethodManual, "fake"}) || q.Titles["fake"] != "Fake pay" {
		t.Fatalf("with a provider: %+v", q)
	}
	s.Manual = false
	if q, _ = s.Quote(ctx, c, r); !slices.Equal(q.Methods, []Method{"fake"}) {
		t.Fatalf("providers only: %+v", q)
	}
	// Fully covered by the balance: only the balance, whatever is configured.
	c.Balance, c.BalanceOwn = 5000, 5000
	if q, _ = s.Quote(ctx, c, r); !slices.Equal(q.Methods, []Method{MethodBalance}) {
		t.Fatalf("covered: %+v", q)
	}
}

func TestProviderColumnsRoundTrip(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.move(ctx, o.ID, []Status{StatusAwaitingReceipt}, StatusAwaitingPayment, map[string]any{
		"method": "fake", "external_id": "ext-1", "pay_url": "https://pay.test/x", "pay_amount": 66.67, "pay_currency": "XTR"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, o.ID)
	if err != nil || got.Status != StatusAwaitingPayment || got.Method != "fake" || !got.Method.IsProvider() || got.ExternalID != "ext-1" ||
		got.PayURL != "https://pay.test/x" || got.PayAmount != 66.67 || got.PayCurrency != "XTR" {
		t.Fatalf("%+v %v", got, err)
	}
	if MethodManual.IsProvider() || MethodBalance.IsProvider() || Method("").IsProvider() {
		t.Error("manual, balance and empty are not providers")
	}
}
```

Append to `internal/store/store_test.go`'s `TestChatStateAndPrompts` nothing — instead add a new test:

```go
func TestOrdersProviderColumns(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB().Exec(`SELECT external_id, pay_url, pay_amount, pay_currency FROM orders`); err != nil {
		t.Fatalf("provider columns: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/orders/ ./internal/store/` → FAIL (build errors: `Providers`, `Titles`, columns).
- [ ] **Step 3: Implement.**

`internal/store/store.go` — in the `orders` table add after the `error` line:

```sql
		error TEXT NOT NULL DEFAULT '',
		external_id TEXT NOT NULL DEFAULT '',
		pay_url TEXT NOT NULL DEFAULT '',
		pay_amount REAL NOT NULL DEFAULT 0,
		pay_currency TEXT NOT NULL DEFAULT '',
```

`internal/orders/order.go`: add the status after `StatusAwaitingReceipt`:

```go
	StatusAwaitingPayment Status = "awaiting_payment" // an invoice is issued, the provider will confirm
```

Add after `MethodBalance`:

```go
// IsProvider: the method is an online payment provider's id ("yookassa", …).
func (m Method) IsProvider() bool { return m != "" && m != MethodManual && m != MethodBalance }
```

`Quote` gets `Titles map[string]string \`json:"titles,omitempty"\`` (provider id → button label). `Order` gets, after `Error string`:

```go
	ExternalID  string  // provider's payment/invoice id
	PayURL      string  // where the customer pays
	PayAmount   float64 // what was invoiced, in PayCurrency (roubles, or stars for Stars)
	PayCurrency string
```

Add to the `var (…)` errors: `ErrProvider = errors.New("orders: payment provider failed")`.

`internal/orders/repo.go`: in `cols` add `external_id, pay_url, pay_amount, pay_currency,` after `error,`; in `scan` add `&o.ExternalID, &o.PayURL, &o.PayAmount, &o.PayCurrency,` after `&o.Error,`.

`internal/orders/service.go`: add `Providers *payments.Registry` to `Service` (import `"remna-control-bot/internal/payments"`), and replace the `switch` in `Quote` that fills `Methods`:

```go
	q.Methods = []Method{}
	switch {
	case q.ToPay == 0:
		q.Methods = []Method{MethodBalance}
	default:
		if s.Manual {
			q.Methods = append(q.Methods, MethodManual)
		}
		for _, p := range s.Providers.Enabled() {
			q.Methods = append(q.Methods, Method(p.ID()))
			if q.Titles == nil {
				q.Titles = map[string]string{}
			}
			q.Titles[p.ID()] = p.Title()
		}
	}
```

`internal/telegram/view/orders.go`: add `orders.StatusAwaitingPayment: "💳"` to `statusIcon` and `orders.StatusAwaitingPayment: "ждёт оплату"` to `statusText`.
- [ ] **Step 4: Run** `go build ./... && go test ./internal/orders/ ./internal/store/ ./internal/telegram/...` → PASS. Then recreate the dev database: `rm -f data/bot.db*`.
- [ ] **Step 5: Commit** `git add -A internal && git commit -m "feat(orders): awaiting_payment status, provider columns, provider quote methods"`

---

### Task 8: Order service — issue, confirm, check, sweep

**Files:** Create `internal/orders/provider.go`, `internal/orders/provider_test.go`; Modify `internal/orders/service.go` (`Create`, `Cancel`)

**Interfaces — Consumes:** Tasks 2, 7. **Produces:**

```go
func (s *Service) CreateWith(ctx context.Context, c *remnactl.Customer, chatID int64, r Request, method Method) (*Order, error)
func (s *Service) Pay(ctx context.Context, customerID int, id, provider string) (*Order, error)
func (s *Service) Paid(ctx context.Context, ev payments.Event) (*Order, error)
func (s *Service) CheckInvoice(ctx context.Context, provider, id string, chatID int64, amount float64, currency string) error
func (s *Service) SweepProviders(ctx context.Context) error
```

`Create` becomes `CreateWith(…, "")`. Method `""` means the default: the balance when it covers everything, manual when it is on, otherwise an **undecided** order (`new`, empty method) the customer then pays with `Pay`.

- [ ] **Step 1: Failing tests** `provider_test.go`

```go
package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/store"
	"remna-control-bot/internal/remnactl"
)

func setupP(t *testing.T) (*Service, *remnactl.Fake, *spy, *remnactl.Customer, *payments.Fake) {
	t.Helper()
	s, f, sp, c := setup(t)
	fp := &payments.Fake{Name: "fake"}
	s.Providers = payments.NewRegistry(fp)
	return s, f, sp, c, fp
}

var renew3 = Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3} // 900, 100 from balance → 800 to pay

func providerOrder(t *testing.T, s *Service, c *remnactl.Customer) *Order {
	t.Helper()
	o, err := s.CreateWith(context.Background(), c, 77, renew3, "fake")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func paidEvent(o *Order) payments.Event {
	return payments.Event{Provider: "fake", OrderID: o.ID, ExternalID: o.ExternalID, Paid: true, Amount: o.PayAmount, Currency: "RUB"}
}

func TestCreateWithProvider(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	o := providerOrder(t, s, c)
	if o.Status != StatusAwaitingPayment || o.Method != "fake" || o.ExternalID != "ext-1" || o.PayURL != "https://pay.test/"+o.ID ||
		o.PayAmount != 800 || o.PayCurrency != "RUB" || o.ToPay != 800 {
		t.Fatalf("%+v", o)
	}
	if len(fp.Invoices) != 1 || fp.Invoices[0].Amount != 800 || fp.Invoices[0].OrderID != o.ID || fp.Invoices[0].ExpiresIn != 48*time.Hour || fp.Invoices[0].Title != o.Title {
		t.Fatalf("invoice request %+v", fp.Invoices)
	}
	got, err := s.Get(context.Background(), o.ID)
	if err != nil || got.ExternalID != "ext-1" || got.Status != StatusAwaitingPayment {
		t.Fatalf("persisted %+v %v", got, err)
	}
	if _, err := s.CreateWith(context.Background(), c, 77, renew3, "nope"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("unknown method: %v", err)
	}
}

func TestCreateDefaultMethods(t *testing.T) {
	s, _, _, c, _ := setupP(t)
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, renew3)
	if err != nil || o.Method != MethodManual || o.Status != StatusAwaitingReceipt {
		t.Fatalf("manual default: %+v %v", o, err)
	}
	s.Manual = false // providers only: the customer must pick one
	o, err = s.Create(ctx, c, 77, renew3)
	if err != nil || o.Method != "" || o.Status != StatusNew {
		t.Fatalf("undecided: %+v %v", o, err)
	}
	s.Providers = nil
	if _, err := s.Create(ctx, c, 77, renew3); !errors.Is(err, ErrManualOff) {
		t.Errorf("nothing configured: %v", err)
	}
}

func TestProviderInvoiceFailureCancels(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	fp.Err = errors.New("provider is down")
	_, err := s.CreateWith(context.Background(), c, 77, renew3, "fake")
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("want ErrProvider, got %v", err)
	}
	var st Status
	if err := s.DB.QueryRow(`SELECT status FROM orders`).Scan(&st); err != nil || st != StatusCancelled {
		t.Fatalf("a failed invoice must not leave a live order: %q %v", st, err)
	}
	fp.Err = nil
	if _, err := s.CreateWith(context.Background(), c, 77, renew3, "fake"); err != nil {
		t.Fatalf("another attempt works: %v", err)
	}
}

func TestPayOnExistingOrder(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, renew3) // manual, awaiting a receipt
	if _, err := s.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Pay(ctx, c.ID, o.ID, "fake")
	if err != nil || got.Status != StatusAwaitingPayment || got.Method != "fake" || got.PayURL == "" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, ok, _ := s.Chats.GetChatState(ctx, 77); ok {
		t.Error("the receipt wait must end when the customer switches to a provider")
	}
	again, err := s.Pay(ctx, c.ID, o.ID, "fake") // idempotent: same invoice
	if err != nil || again.PayURL != got.PayURL || len(fp.Invoices) != 1 {
		t.Fatalf("repeat: %+v %v invoices=%d", again, err, len(fp.Invoices))
	}
	if _, err := s.Pay(ctx, 999, o.ID, "fake"); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else's order: %v", err)
	}
	if _, err := s.Pay(ctx, c.ID, o.ID, "zzz"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("unknown provider: %v", err)
	}
	s.Providers = payments.NewRegistry(fp, &payments.Fake{Name: "other"})
	if _, err := s.Pay(ctx, c.ID, o.ID, "other"); !errors.Is(err, ErrStateChanged) {
		t.Errorf("switching an issued invoice to another provider: %v", err)
	}
}

func TestPaidAppliesOnce(t *testing.T) {
	s, f, sp, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	ev := paidEvent(o)

	got, err := s.Paid(ctx, ev)
	if err != nil || got.Status != StatusApproved || got.Admin != "provider:fake" || got.Decision != DecideFull {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.Payments) != 1 || f.Payments[0].Method != "fake" || f.Payments[0].Note != "bot order #"+o.ID || f.Payments[0].Amount != 800 {
		t.Fatalf("panel payments %+v", f.Payments)
	}
	if len(sp.sent) != 1 || sp.sent[0].Status != StatusApproved {
		t.Errorf("the customer must be told once: %+v", sp.sent)
	}
	if len(sp.posted) != 0 {
		t.Errorf("a clean provider payment needs no admin card: %d", len(sp.posted))
	}
	for range 3 { // webhook retries, sweep races
		if _, err := s.Paid(ctx, ev); !errors.Is(err, ErrStateChanged) {
			t.Fatalf("replay: %v", err)
		}
	}
	if len(f.Payments) != 1 {
		t.Fatalf("replays moved money again: %d payments", len(f.Payments))
	}
}

func TestPaidTopupCreditsBalance(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	o, err := s.CreateWith(context.Background(), c, 77, Request{Kind: KindTopup, Amount: 250}, "fake")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Paid(context.Background(), paidEvent(o))
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || f.Payments[0].Amount != 250 || len(f.Payments[0].Allocations) != 0 {
		t.Fatalf("%+v %v %+v", got, err, f.Payments)
	}
}

func TestPaidAfterExpiredOrCancelled(t *testing.T) {
	for name, prep := range map[string]func(*Service, *remnactl.Customer, *Order) error{
		"expired":   func(s *Service, _ *remnactl.Customer, o *Order) error { return s.move(context.Background(), o.ID, []Status{StatusAwaitingPayment}, StatusExpired, nil) },
		"cancelled": func(s *Service, c *remnactl.Customer, o *Order) error { return s.Cancel(context.Background(), c.ID, o.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			s, f, _, c, _ := setupP(t)
			o := providerOrder(t, s, c)
			if err := prep(s, c, o); err != nil {
				t.Fatal(err)
			}
			got, err := s.Paid(context.Background(), paidEvent(o))
			if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 {
				t.Fatalf("money that arrived must be applied: %+v %v payments=%d", got, err, len(f.Payments))
			}
		})
	}
}

func TestPaidNeedsReview(t *testing.T) {
	cases := map[string]func(*payments.Event){
		"less money":     func(e *payments.Event) { e.Amount = 500 },
		"review flag":    func(e *payments.Event) { e.Paid, e.Review, e.Amount = false, true, 0 },
		"other currency": func(e *payments.Event) { e.Currency = "USDT" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			s, f, sp, c, _ := setupP(t)
			ctx := context.Background()
			o := providerOrder(t, s, c)
			ev := paidEvent(o)
			mod(&ev)
			got, err := s.Paid(ctx, ev)
			if err != nil || got.Status != StatusPendingReview {
				t.Fatalf("%+v %v", got, err)
			}
			if len(f.Payments) != 0 {
				t.Fatal("a doubtful payment must not reach the panel")
			}
			if len(sp.posted) != 1 || !strings.Contains(got.Comment, "fake") {
				t.Errorf("an admin card explaining it: posted=%d comment=%q", len(sp.posted), got.Comment)
			}
			if name == "less money" && (got.Received == nil || *got.Received != 500) {
				t.Errorf("received %v", got.Received)
			}
			if name != "less money" && got.Received != nil {
				t.Errorf("received must stay empty when the rouble amount is unknown: %v", *got.Received)
			}
			// The admin decides with the existing flow.
			if _, err := s.Approve(ctx, o.ID, "@adm", DecideCredit); err != nil && name == "less money" {
				t.Errorf("approve as credit: %v", err)
			}
		})
	}
}

func TestPaidForeignEvents(t *testing.T) {
	s, f, _, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	for name, mod := range map[string]func(*payments.Event){
		"other provider":  func(e *payments.Event) { e.Provider = "yookassa" },
		"other invoice":   func(e *payments.Event) { e.ExternalID = "zzz" },
		"unknown order":   func(e *payments.Event) { e.OrderID = "00000000-0000-4000-8000-000000000000" },
		"neither paid nor review": func(e *payments.Event) { e.Paid = false },
	} {
		ev := paidEvent(o)
		mod(&ev)
		got, err := s.Paid(ctx, ev)
		if name == "neither paid nor review" {
			if err != nil || got != nil {
				t.Errorf("%s: %+v %v", name, got, err)
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.Payments) != 0 {
		t.Fatalf("nothing may be applied: %+v", f.Payments)
	}
	if cur, _ := s.Get(ctx, o.ID); cur.Status != StatusAwaitingPayment {
		t.Errorf("order moved: %s", cur.Status)
	}
}

func TestCheckInvoice(t *testing.T) {
	s, _, _, c, fp := setupP(t)
	fp.Currency = "XTR"
	ctx := context.Background()
	o := providerOrder(t, s, c) // invoiced 800 XTR
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "XTR"); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 78, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else's chat: %v", err)
	}
	if err := s.CheckInvoice(ctx, "other", o.ID, 77, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other provider: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 700, "XTR"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("changed amount: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "RUB"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("changed currency: %v", err)
	}
	if err := s.CheckInvoice(ctx, "fake", "no-such-order", 77, 800, "XTR"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
	if err := s.Cancel(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckInvoice(ctx, "fake", o.ID, 77, 800, "XTR"); !errors.Is(err, ErrStateChanged) {
		t.Errorf("cancelled order must not be payable: %v", err)
	}
}

func TestSweepPicksUpLostWebhook(t *testing.T) {
	s, f, _, c, fp := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	fp.Statuses = map[string]payments.Event{o.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}

	s.Now = func() time.Time { return now.Add(time.Minute) } // too fresh to poll
	if err := s.SweepProviders(ctx); err != nil || len(f.Payments) != 0 {
		t.Fatalf("fresh order polled: %v payments=%d", err, len(f.Payments))
	}
	s.Now = func() time.Time { return now.Add(3 * time.Minute) }
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, o.ID)
	if got.Status != StatusApproved || len(f.Payments) != 1 {
		t.Fatalf("lost webhook not recovered: %s payments=%d", got.Status, len(f.Payments))
	}
	if err := s.SweepProviders(ctx); err != nil || len(f.Payments) != 1 {
		t.Fatalf("second sweep: %v payments=%d", err, len(f.Payments))
	}
}

func TestSweepExpires(t *testing.T) {
	s, f, _, c, fp := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	late := func() time.Time { return now.Add(49 * time.Hour) } // TTL is 48 h

	// A failing poll must not expire the order: the answer is unknown.
	fp.StatusErr = errors.New("provider unreachable")
	s.Now = late
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusAwaitingPayment {
		t.Fatalf("expired on an unknown answer: %s", got.Status)
	}
	// Still unpaid at the last look → expired.
	fp.StatusErr = nil
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusExpired {
		t.Fatalf("not expired: %s", got.Status)
	}
	// Paid at the last look → applied, not expired.
	o2 := providerOrder(t, s, c)
	fp.Statuses = map[string]payments.Event{o2.ExternalID: {Paid: true, Amount: 800, Currency: "RUB"}}
	s.Now = func() time.Time { return now.Add(98 * time.Hour) }
	if err := s.SweepProviders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o2.ID); got.Status != StatusApproved || len(f.Payments) != 1 {
		t.Fatalf("a payment found at expiry must be applied: %s payments=%d", got.Status, len(f.Payments))
	}
}

func TestCancelAwaitingPayment(t *testing.T) {
	s, _, _, c, _ := setupP(t)
	ctx := context.Background()
	o := providerOrder(t, s, c)
	if err := s.Cancel(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusCancelled {
		t.Fatalf("%s", got.Status)
	}
}

var _ = store.ChatState{} // keep the import used by helpers in this file
```

(Remove the final `var _ = store.ChatState{}` line and the `store` import if the compiler says the import is unused — it is only there in case you add chat-state assertions.)

- [ ] **Step 2: Run** `go test ./internal/orders/ -run 'Provider|Paid|Pay|Sweep|CheckInvoice|CreateDefault|CreateWith|CancelAwaiting'` → FAIL (undefined `CreateWith`, …).
- [ ] **Step 3: `service.go` changes.** Replace `Create` with a delegating wrapper, and move its body to `provider.go` (below). In `Cancel` use `[]Status{StatusNew, StatusAwaitingReceipt, StatusAwaitingPayment}`. Update the doc comment above `Create`:

```go
// Create is CreateWith the default method: the balance when it covers the
// whole price, manual payment when it is on, otherwise an undecided order the
// customer pays with Pay.
func (s *Service) Create(ctx context.Context, c *remnactl.Customer, chatID int64, r Request) (*Order, error) {
	return s.CreateWith(ctx, c, chatID, r, "")
}
```

- [ ] **Step 4: `provider.go`**

```go
package orders

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/remnactl"
)

// CreateWith stores an order paid with method; empty means the default (see
// Create). A provider method issues its invoice at once; if the provider
// fails, the order is cancelled so no dangling order is left.
func (s *Service) CreateWith(ctx context.Context, c *remnactl.Customer, chatID int64, r Request, method Method) (*Order, error) {
	q, err := s.Quote(ctx, c, r)
	if err != nil {
		return nil, err
	}
	if len(q.Methods) == 0 {
		return nil, ErrManualOff
	}
	if method == "" {
		if method = q.Methods[0]; method.IsProvider() {
			method = "" // no default provider: the customer picks one
		}
	}
	if method != "" && !slices.Contains(q.Methods, method) {
		return nil, bad("method %q", method)
	}
	r.Amount = 0
	o := &Order{CustomerID: c.ID, ChatID: chatID, Request: r, Title: q.Title, Price: q.Price,
		FromBalance: q.FromBalance, ToPay: q.ToPay, Method: method, Status: StatusNew}
	if method == MethodManual {
		o.Status = StatusAwaitingReceipt
	}
	if err := s.insert(ctx, o); err != nil {
		return nil, err
	}
	if !method.IsProvider() {
		return o, nil
	}
	p, _ := s.Providers.Get(string(method)) // Quote only offers registered providers
	got, err := s.issue(ctx, o, p)
	if err != nil {
		if cerr := s.move(ctx, o.ID, []Status{StatusNew}, StatusCancelled, nil); cerr != nil {
			s.Log.Error("orders: cancel after a failed invoice", "order", o.ID, "err", cerr)
		}
		return nil, err
	}
	return got, nil
}

// Pay issues an invoice with provider for the customer's own order (a new or
// receipt-waiting one) and returns it with PayURL set. Repeating it for the
// same provider returns the same invoice.
func (s *Service) Pay(ctx context.Context, customerID int, id, provider string) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	p, ok := s.Providers.Get(provider)
	if !ok || o.ToPay <= 0 {
		return nil, bad("provider %q", provider)
	}
	if o.Status == StatusAwaitingPayment && o.Method == Method(provider) && o.PayURL != "" {
		return o, nil
	}
	if o.Status != StatusNew && o.Status != StatusAwaitingReceipt {
		return nil, ErrStateChanged
	}
	return s.issue(ctx, o, p)
}

// issue bills the order: the invoice is created first; if the order moved on
// meanwhile the invoice is simply left to expire at the provider.
func (s *Service) issue(ctx context.Context, o *Order, p payments.Provider) (*Order, error) {
	inv, err := p.Invoice(ctx, payments.InvoiceRequest{OrderID: o.ID, Title: o.Title, Amount: o.ToPay, ExpiresIn: s.TTL})
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrProvider, p.ID(), err)
	}
	if err := s.move(ctx, o.ID, []Status{StatusNew, StatusAwaitingReceipt}, StatusAwaitingPayment, map[string]any{
		"method": p.ID(), "external_id": inv.ExternalID, "pay_url": inv.URL, "pay_amount": inv.Amount, "pay_currency": inv.Currency,
	}); err != nil {
		return nil, err
	}
	if st, ok, _ := s.Chats.GetChatState(ctx, o.ChatID); ok && st.OrderID == o.ID {
		if err := s.Chats.ClearChatState(ctx, o.ChatID); err != nil {
			s.Log.Error("orders: clear chat state", "err", err)
		}
	}
	return s.Get(ctx, o.ID)
}

// paidFrom are the states a provider's confirmation is accepted in: money that
// arrives for an expired or cancelled order is still money.
var paidFrom = []Status{StatusAwaitingPayment, StatusExpired, StatusCancelled}

// Paid applies a provider's statement that an invoice was paid (or looks
// wrong). Replays are harmless: only the first call finds the order in a
// payable state, the rest return ErrStateChanged.
func (s *Service) Paid(ctx context.Context, ev payments.Event) (*Order, error) {
	if !ev.Paid && !ev.Review {
		return nil, nil
	}
	o, err := s.Get(ctx, ev.OrderID)
	if err != nil {
		return nil, err
	}
	if o.Method != Method(ev.Provider) || (o.ExternalID != "" && ev.ExternalID != "" && o.ExternalID != ev.ExternalID) {
		return nil, ErrNotFound // somebody else's order or invoice
	}
	set := map[string]any{"external_id": cmp.Or(ev.ExternalID, o.ExternalID)}
	full := ev.Paid && !ev.Review && strings.EqualFold(ev.Currency, o.PayCurrency) && ev.Amount+0.005 >= o.PayAmount
	if full {
		set["admin"], set["decision"] = "provider:"+ev.Provider, string(DecideFull)
		if err := s.move(ctx, o.ID, paidFrom, StatusApproving, set); err != nil {
			return nil, err
		}
		if o, err = s.Get(ctx, o.ID); err != nil {
			return nil, err
		}
		return s.run(ctx, o)
	}

	// Something is off (less money, another currency, the provider's own flag):
	// an admin decides with the usual card.
	set["comment"] = reviewNote(o, ev)
	if strings.EqualFold(ev.Currency, "RUB") && ev.Amount > 0 {
		set["received"] = round2(ev.Amount)
	}
	if err := s.move(ctx, o.ID, paidFrom, StatusPendingReview, set); err != nil {
		return nil, err
	}
	if o, err = s.Get(ctx, o.ID); err != nil {
		return nil, err
	}
	msgID, err := s.Notify.PostCard(ctx, *o)
	if err != nil {
		s.Log.Error("orders: admin card for a provider payment", "order", o.ID, "err", err)
		return o, nil
	}
	o.AdminMsgID = msgID
	if err := s.set(ctx, o.ID, map[string]any{"admin_msg_id": msgID}); err != nil {
		s.Log.Error("orders: save admin card id", "order", o.ID, "err", err)
	}
	return o, nil
}

func reviewNote(o *Order, ev payments.Event) string {
	billed := strconv.FormatFloat(o.PayAmount, 'f', -1, 64) + " " + o.PayCurrency
	if ev.Amount <= 0 {
		return fmt.Sprintf("%s: провайдер сообщил о неверной сумме (счёт %s)", ev.Provider, billed)
	}
	return fmt.Sprintf("%s: сумма не совпала (счёт %s, получено %s %s)", ev.Provider, billed, strconv.FormatFloat(ev.Amount, 'f', -1, 64), ev.Currency)
}

// CheckInvoice is Telegram's pre-checkout question: may this payment go
// ahead? ErrNotFound hides orders that are not the asker's.
func (s *Service) CheckInvoice(ctx context.Context, provider, id string, chatID int64, amount float64, currency string) error {
	o, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case o.Method != Method(provider) || o.ChatID != chatID:
		return ErrNotFound
	case o.Status != StatusAwaitingPayment:
		return ErrStateChanged
	case !strings.EqualFold(currency, o.PayCurrency) || math.Abs(amount-o.PayAmount) > 0.005:
		return bad("invoice changed")
	}
	return nil
}

// pollAfter: a fresh invoice is left to its webhook for a while.
const pollAfter = 2 * time.Minute

// SweepProviders recovers lost webhooks and ends stale invoices: every
// awaiting_payment order older than pollAfter is asked about at its provider;
// one older than TTL that is still unpaid at that last look expires. An
// unreachable provider changes nothing.
func (s *Service) SweepProviders(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM orders WHERE status = ? ORDER BY created_at`, StatusAwaitingPayment)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() { // collected first: the database has a single connection
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		o, err := s.Get(ctx, id)
		if err != nil || o.Status != StatusAwaitingPayment {
			continue
		}
		age := s.Now().Sub(o.CreatedAt)
		if age < pollAfter {
			continue
		}
		if p, ok := s.Providers.Get(string(o.Method)); ok {
			if poller, ok := p.(payments.Poller); ok && o.ExternalID != "" {
				ev, err := poller.Status(ctx, o.ExternalID)
				if err != nil {
					s.Log.Warn("orders: provider status", "order", o.ID, "provider", o.Method, "err", err)
					continue // unknown: neither apply nor expire
				}
				ev.Provider, ev.OrderID = string(o.Method), o.ID
				if ev.ExternalID == "" {
					ev.ExternalID = o.ExternalID
				}
				if ev.Paid || ev.Review {
					if _, err := s.Paid(ctx, ev); err != nil && !errors.Is(err, ErrStateChanged) {
						s.Log.Error("orders: apply polled payment", "order", o.ID, "err", err)
					}
					continue
				}
			}
		}
		if age >= s.TTL {
			if err := s.move(ctx, o.ID, []Status{StatusAwaitingPayment}, StatusExpired, nil); err != nil && !errors.Is(err, ErrStateChanged) {
				s.Log.Error("orders: expire", "order", o.ID, "err", err)
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Run** `gofmt -l internal; go vet ./... && go test ./internal/orders/` → PASS (fix import lists if the compiler complains about unused ones; `s.Providers` may be nil in older tests — the registry methods are nil-safe).
- [ ] **Step 6: Commit** `git add -A internal && git commit -m "feat(orders): issue invoices, apply provider payments, sweep lost webhooks"`

---

### Task 9: WebApp — webhooks, `method`, `pay_url`

**Files:** Modify `internal/webapp/server.go`, `internal/webapp/orders.go`, `internal/webapp/orders_test.go`

**Interfaces — Consumes:** Task 8. **Produces:** `POST /pay/:provider` (outside the WebApp path and its auth); `POST /api/orders` accepts `"method"`; `orderView` gains `pay_url`; `orders.ErrProvider` → HTTP 424 `provider_failed`.

- [ ] **Step 1: Failing tests** — append to `internal/webapp/orders_test.go` (add imports `net/http`, `errors`, `remna-control-bot/internal/payments`):

```go
// withProvider attaches a fake provider to the service.
func withProvider(d *Deps) *payments.Fake {
	fp := &payments.Fake{Name: "fake"}
	d.Orders.Providers = payments.NewRegistry(fp)
	return fp
}

func hook(t *testing.T, d *Deps, provider, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/pay/"+provider, strings.NewReader(body))
	req.Header.Set("X-Test-Signature", "ok")
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestCreateOrderWithProvider(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)

	code, out := do(t, d, "POST", "/bot/api/orders/quote", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1}`)
	if code != 200 || !strings.Contains(out, `"methods":["manual","fake"]`) || !strings.Contains(out, `"titles":{"fake":"Fake pay"}`) {
		t.Fatalf("quote %d %s", code, out)
	}
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1,"method":"fake"}`)
	if code != 201 || !strings.Contains(out, `"status":"awaiting_payment"`) || !strings.Contains(out, `"method":"fake"`) || !strings.Contains(out, `"pay_url":"https://pay.test/`) {
		t.Fatalf("create %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100,"method":"nope"}`); code != 400 {
		t.Errorf("unknown method: %d", code)
	}
	fp.Err = errors.New("provider is down")
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100,"method":"fake"}`)
	if code != 424 || !strings.Contains(out, `"provider_failed"`) || strings.Contains(out, "down") {
		t.Errorf("provider failure: %d %s", code, out)
	}
}

func TestPayWebhookStatusCodes(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)
	id := newOrder(t, d, `{"kind":"topup","amount":100,"method":"fake"}`)
	ev := payments.Event{Provider: "fake", OrderID: id, ExternalID: "ext-1", Paid: true, Amount: 100, Currency: "RUB"}
	fp.Hook = func(h http.Header, body []byte) (payments.Event, bool, error) {
		switch string(body) {
		case "forged":
			return payments.Event{}, false, payments.ErrBadSignature
		case "garbage":
			return payments.Event{}, false, payments.ErrBadBody
		case "ignore":
			return payments.Event{}, false, nil
		case "boom":
			return payments.Event{}, false, errors.New("provider api down")
		case "stranger":
			e := ev
			e.OrderID = "00000000-0000-4000-8000-000000000000"
			return e, true, nil
		}
		if h.Get("X-Test-Signature") != "ok" {
			t.Errorf("headers were not passed on: %v", h)
		}
		return ev, true, nil
	}
	for body, want := range map[string]int{"forged": 401, "garbage": 400, "ignore": 200, "boom": 500, "stranger": 200} {
		if code, _ := hook(t, d, "fake", body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	if code, _ := hook(t, d, "nope", "x"); code != 404 {
		t.Errorf("unconfigured provider: %d", code)
	}
	if code, _ := hook(t, d, "fake", strings.Repeat("x", 70<<10)); code != 413 {
		t.Errorf("oversized body: %d", code)
	}
	if code, _ := hook(t, d, "fake", "paid"); code != 200 {
		t.Fatalf("paid: %d", code)
	}
	if cur, _ := d.Orders.Get(t.Context(), id); cur.Status != orders.StatusApproved {
		t.Fatalf("order %s", cur.Status)
	}
}

func TestPayWebhookDuplicate(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	fp := withProvider(d)
	id := newOrder(t, d, `{"kind":"topup","amount":100,"method":"fake"}`)
	fp.Hook = func(http.Header, []byte) (payments.Event, bool, error) {
		return payments.Event{Provider: "fake", OrderID: id, ExternalID: "ext-1", Paid: true, Amount: 100, Currency: "RUB"}, true, nil
	}
	for i := range 3 {
		if code, _ := hook(t, d, "fake", "x"); code != 200 {
			t.Fatalf("delivery %d: %d (providers keep retrying anything but 200)", i, code)
		}
	}
	if len(f.Payments) != 1 {
		t.Fatalf("the panel got %d payments for one order", len(f.Payments))
	}
}

func TestPayWebhookNeedsOrders(t *testing.T) {
	d := app(ordersFake()) // no order service
	if code, _ := hook(t, d, "fake", "x"); code != 404 {
		t.Errorf("%d", code)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/webapp/` → FAIL.
- [ ] **Step 3: Implement.**

`internal/webapp/orders.go`:
- `orderView` gets `PayURL string \`json:"pay_url,omitempty"\`` and `orderJSON` sets `PayURL: o.PayURL`.
- `orderErr` gets, before the default: `case errors.Is(err, orders.ErrProvider): s.Log.Warn("webapp orders", "err", err); return fail(c, fiber.StatusFailedDependency, "provider_failed")`.
- Replace the body of `createOrder`:

```go
func (s *server) createOrder(c *fiber.Ctx) error {
	var r struct {
		orders.Request
		Method string `json:"method"`
	}
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	o, err := s.Orders.CreateWith(c.UserContext(), c.Locals("customer").(*remnactl.Customer), c.Locals("tg").(int64), r.Request, orders.Method(r.Method))
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"order": orderJSON(*o), "details": s.Orders.Details})
}
```

New file `internal/webapp/pay.go`:

```go
package webapp

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
)

const maxHookBody = 64 << 10

// payWebhook receives a provider's callback. Providers retry anything but a
// 200, so handled, duplicate, unknown and irrelevant events all answer 200;
// 500 is only for failures a retry can fix.
func (s *server) payWebhook(c *fiber.Ctx) error {
	if s.Orders == nil {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	p, ok := s.Orders.Providers.Get(c.Params("provider"))
	wp, hook := p.(payments.WebhookProvider)
	if !ok || !hook {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	body := c.Body()
	if len(body) > maxHookBody {
		return fail(c, fiber.StatusRequestEntityTooLarge, "too_large")
	}
	h := http.Header{}
	c.Request().Header.VisitAll(func(k, v []byte) { h.Add(string(k), string(v)) })

	ev, relevant, err := wp.Webhook(c.UserContext(), h, body)
	switch {
	case errors.Is(err, payments.ErrBadSignature):
		s.Log.Warn("pay webhook: bad signature", "provider", p.ID())
		return fail(c, fiber.StatusUnauthorized, "bad_signature")
	case errors.Is(err, payments.ErrBadBody):
		return fail(c, fiber.StatusBadRequest, "bad_request")
	case err != nil:
		s.Log.Error("pay webhook", "provider", p.ID(), "err", err)
		return fail(c, fiber.StatusInternalServerError, "unavailable")
	case !relevant:
		return c.SendStatus(fiber.StatusOK)
	}
	if _, err := s.Orders.Paid(c.UserContext(), ev); err != nil {
		if errors.Is(err, orders.ErrNotFound) || errors.Is(err, orders.ErrStateChanged) {
			s.Log.Info("pay webhook: nothing to do", "provider", p.ID(), "order", ev.OrderID, "reason", err)
			return c.SendStatus(fiber.StatusOK)
		}
		s.Log.Error("pay webhook: apply", "provider", p.ID(), "order", ev.OrderID, "err", err)
		return fail(c, fiber.StatusInternalServerError, "unavailable")
	}
	return c.SendStatus(fiber.StatusOK)
}
```

`internal/webapp/server.go` — before the `api := app.Group(…)` line register the webhook (it must not go through `auth`):

```go
	app.Post("/pay/:provider", s.payWebhook)
```

- [ ] **Step 4: Run** `gofmt -l internal; go vet ./... && go test ./internal/webapp/` → PASS.
- [ ] **Step 5: Commit** `git add -A internal && git commit -m "feat(webapp): provider webhooks, method and pay_url on orders"`

---

### Task 10: Bot — Stars events, provider buttons

**Files:** Modify `internal/telegram/private.go`, `internal/telegram/bot.go`, `internal/telegram/orders.go`, `internal/telegram/view/screen.go`, `internal/telegram/view/texts.go`, `internal/telegram/view/orders.go`, `internal/telegram/route.go`; Tests `internal/telegram/private_test.go`, `internal/telegram/route_test.go`, `internal/telegram/view/orders_test.go`

**Interfaces — Consumes:** Task 8 (`Pay`, `Paid`, `CheckInvoice`), Task 6 (`LinkFunc`). **Produces:** `(*Bot).InvoiceLink` (a `payments.LinkFunc`); `view.CbOrderProvider = "pp:"` (data `pp:<provider>:<order uuid>`); `view.ProviderButton{ID, Title}`; `view.Confirm(o, providers ...ProviderButton)`; `view.ProviderPay(o, title) Screen`; `view.BtnPay`.

- [ ] **Step 1: Failing tests.**

`internal/telegram/private_test.go` — append:

```go
func TestAllowedPreCheckout(t *testing.T) {
	u := &models.Update{PreCheckoutQuery: &models.PreCheckoutQuery{ID: "1", From: &models.User{ID: 5}}}
	if !allowed(u, -100) {
		t.Fatal("pre_checkout_query must reach the bot: Telegram wants an answer within 10 s")
	}
	u.PreCheckoutQuery.From = nil
	if allowed(u, -100) {
		t.Fatal("a query without a sender is not ours")
	}
}
```

`internal/telegram/route_test.go` — append:

```go
func TestParseProviderCallback(t *testing.T) {
	const id = "0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c"
	p, o, ok := parseProviderCallback("pp:yookassa:" + id)
	if !ok || p != "yookassa" || o != id {
		t.Fatalf("%q %q %v", p, o, ok)
	}
	for _, bad := range []string{"pp:yookassa", "pp::" + id, "pp:yookassa:1", "pp:yoo kassa:" + id, "pp:a:b:" + id, "pr:yookassa:" + id, "pp:" + strings.Repeat("a", 40) + ":" + id} {
		if _, _, ok := parseProviderCallback(bad); ok {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
```

(add `"strings"` to that file's imports.)

`internal/telegram/view/orders_test.go` — append:

```go
func TestConfirmWithProviders(t *testing.T) {
	o := order() // manual
	s := Confirm(o, ProviderButton{ID: "yookassa", Title: "Карта / СБП"}, ProviderButton{ID: "stars", Title: "Telegram Stars ⭐"})
	b := buttons(s)
	if !has(b, CbOrderPay+o.ID) || !has(b, CbOrderProvider+"yookassa:"+o.ID) || !has(b, CbOrderProvider+"stars:"+o.ID) || !has(b, CbOrderCancel+o.ID) {
		t.Fatalf("buttons %v", b)
	}
	for _, cb := range b {
		if len(cb) > 64 {
			t.Errorf("callback data %q is over Telegram's 64 bytes", cb)
		}
	}
	undecided := o
	undecided.Method, undecided.Status = "", orders.StatusNew
	if b := buttons(Confirm(undecided, ProviderButton{ID: "stars", Title: "S"})); has(b, CbOrderPay+o.ID) || !has(b, CbOrderProvider+"stars:"+o.ID) {
		t.Errorf("an undecided order offers only providers: %v", b)
	}
	bal := o
	bal.Method = orders.MethodBalance
	if b := buttons(Confirm(bal, ProviderButton{ID: "stars", Title: "S"})); has(b, CbOrderProvider+"stars:"+o.ID) {
		t.Errorf("a balance order offers no provider: %v", b)
	}
}

func TestProviderPay(t *testing.T) {
	o := order()
	o.Status, o.Method, o.PayURL = orders.StatusAwaitingPayment, "yookassa", "https://yoomoney.ru/pay/p1"
	s := ProviderPay(o, "Карта / СБП")
	if !strings.Contains(s.Text, "Карта / СБП") || !strings.Contains(s.Text, "800") {
		t.Fatalf("text:\n%s", s.Text)
	}
	var url string
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if b.URL != "" {
				url = b.URL
			}
		}
	}
	if url != o.PayURL || !has(buttons(s), CbOrderCancel+o.ID) {
		t.Fatalf("url %q buttons %v", url, buttons(s))
	}
}
```

(`buttons`/`has` are the test helpers already used in that file — `buttons` returns callback data strings.)

- [ ] **Step 2: Run** `go test ./internal/telegram/...` → FAIL (undefined).
- [ ] **Step 3: Implement.**

`view/screen.go` — add to the constants after `CbOrderCancel`:

```go
	CbOrderProvider = "pp:" // + provider id : order id — pay with that provider
```

`view/texts.go` — add `BtnPay = "💳 Оплатить"` next to `BtnSendReceipt`.

`view/orders.go` — replace `Confirm` and add the new pieces:

```go
// ProviderButton is an online payment method offered on the confirm screen.
type ProviderButton struct{ ID, Title string }

// Confirm is the pay screen before payment is chosen (bot renew flow): a
// receipt for manual orders, one button per provider, cancel.
func Confirm(o orders.Order, providers ...ProviderButton) Screen {
	if o.Method == orders.MethodBalance {
		return Screen{Text: summary(o) + "\n\nВся сумма спишется с баланса.", Keyboard: kb(
			[]btn{cb(BtnPayBalance, CbOrderBal+o.ID)}, []btn{cb(BtnCancel, CbOrderCancel+o.ID)})}
	}
	var rows [][]btn
	if o.Method == orders.MethodManual {
		rows = append(rows, []btn{cb(BtnSendReceipt, CbOrderPay+o.ID)})
	}
	for _, p := range providers {
		rows = append(rows, []btn{cb("💳 "+p.Title, CbOrderProvider+p.ID+":"+o.ID)})
	}
	rows = append(rows, []btn{cb(BtnCancel, CbOrderCancel+o.ID)})
	return Screen{Text: summary(o), Keyboard: kb(rows...)}
}

// ProviderPay carries the payment link of a provider order.
func ProviderPay(o orders.Order, title string) Screen {
	text := summary(o) + "\n\n💳 <b>Оплата:</b> " + Esc(title) + "\nНажмите кнопку — после оплаты всё применится автоматически."
	return Screen{Text: text, Keyboard: kb([]btn{{Text: BtnPay, URL: o.PayURL}}, []btn{cb(BtnCancel, CbOrderCancel+o.ID)})}
}
```

`route.go` — add (import `github.com/google/uuid` is already there):

```go
// parseProviderCallback parses "pp:<provider>:<order uuid>".
func parseProviderCallback(data string) (provider, orderID string, ok bool) {
	rest, ok := strings.CutPrefix(data, view.CbOrderProvider)
	if !ok {
		return "", "", false
	}
	provider, orderID, ok = strings.Cut(rest, ":")
	if !ok || provider == "" || len(provider) > 20 || strings.ContainsAny(provider, ": ") || len(orderID) != 36 {
		return "", "", false
	}
	if _, err := uuid.Parse(orderID); err != nil {
		return "", "", false
	}
	return provider, orderID, true
}
```

`private.go` — in `allowed` add a case before the final `return false`:

```go
	case u.PreCheckoutQuery != nil:
		return u.PreCheckoutQuery.From != nil
```

`bot.go` — register the new callback and route the new updates. Change the order-callback prefix list to `[]string{view.CbRenewPick, view.CbOrderPay, view.CbOrderBal, view.CbOrderCancel, view.CbOrderProvider}`, and `tgbot.WithDefaultHandler(b.onText)` to `tgbot.WithDefaultHandler(b.onUpdate)`. Add:

```go
// onUpdate routes what no handler matched: Stars' payment events, else text.
func (b *Bot) onUpdate(ctx context.Context, tb *tgbot.Bot, u *models.Update) {
	switch {
	case u.PreCheckoutQuery != nil:
		b.onPreCheckout(ctx, u.PreCheckoutQuery)
	case u.Message != nil && u.Message.SuccessfulPayment != nil:
		b.onStarsPaid(ctx, u.Message)
	default:
		b.onText(ctx, tb, u)
	}
}
```

`orders.go` — add the Stars pieces and provider flow (imports: `payments`, `uuid` not needed here):

```go
// InvoiceLink is payments.Stars' LinkFunc: a Telegram Stars invoice link.
func (b *Bot) InvoiceLink(ctx context.Context, title, description, payload string, stars int) (string, error) {
	return b.tg.CreateInvoiceLink(ctx, &tgbot.CreateInvoiceLinkParams{
		Title: title, Description: description, Payload: payload, Currency: "XTR",
		Prices: []models.LabeledPrice{{Label: "Оплата", Amount: stars}},
	})
}

// onPreCheckout answers Telegram's "may this payment go ahead?" within its
// 10 s limit: only for an order of this very user that still waits for it.
func (b *Bot) onPreCheckout(ctx context.Context, q *models.PreCheckoutQuery) {
	ans := &tgbot.AnswerPreCheckoutQueryParams{PreCheckoutQueryID: q.ID, OK: true}
	if b.Orders == nil || q.Currency != "XTR" {
		ans.OK, ans.ErrorMessage = false, "Оплата недоступна."
	} else if err := b.Orders.CheckInvoice(ctx, "stars", q.InvoicePayload, q.From.ID, float64(q.TotalAmount), q.Currency); err != nil {
		ans.OK, ans.ErrorMessage = false, "Этот счёт больше недействителен. Создайте заказ заново."
		b.Log.Warn("pre-checkout refused", "payload", q.InvoicePayload, "user", q.From.ID, "err", err)
	}
	if _, err := b.tg.AnswerPreCheckoutQuery(ctx, ans); err != nil {
		b.Log.Error("answer pre-checkout", "err", err)
	}
}

// onStarsPaid applies a Stars payment; the customer's notice comes from the
// order service. Telegram may repeat the update: a replay is ErrStateChanged.
func (b *Bot) onStarsPaid(ctx context.Context, m *models.Message) {
	sp := m.SuccessfulPayment
	if b.Orders == nil || sp.Currency != "XTR" {
		return
	}
	_, err := b.Orders.Paid(ctx, payments.Event{Provider: "stars", OrderID: sp.InvoicePayload, ExternalID: sp.TelegramPaymentChargeID,
		Paid: true, Amount: float64(sp.TotalAmount), Currency: sp.Currency})
	switch {
	case err == nil, errors.Is(err, orders.ErrStateChanged):
	default:
		// The money is taken but the order was not applied: this needs a human.
		b.Log.Error("stars payment not applied", "order", sp.InvoicePayload, "charge", sp.TelegramPaymentChargeID, "err", err)
	}
}

func (b *Bot) providerButtons() []view.ProviderButton {
	var out []view.ProviderButton
	for _, p := range b.Orders.Providers.Enabled() {
		out = append(out, view.ProviderButton{ID: p.ID(), Title: p.Title()})
	}
	return out
}
```

In `onOrderCallback`: replace `b.show(ctx, chatID, msgID, view.Confirm(*o))` with `b.show(ctx, chatID, msgID, view.Confirm(*o, b.providerButtons()...))`, and insert before the `for prefix, act := range …` loop:

```go
	if provider, id, ok := parseProviderCallback(q.Data); ok {
		o, err := b.Orders.Pay(ctx, c.ID, id, provider)
		switch {
		case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound), errors.Is(err, orders.ErrBadRequest):
			answer.Text = view.TxtOrderDone
		case err != nil:
			b.Log.Error("pay with provider", "provider", provider, "err", err)
			answer.Text, answer.ShowAlert = view.TxtProviderDown, true
		default:
			b.show(ctx, chatID, msgID, view.ProviderPay(*o, b.Orders.Providers.Title(provider)))
		}
		return
	}
```

Add `TxtProviderDown = "Не удалось создать счёт у платёжной системы. Попробуйте другой способ."` to `view/texts.go`. Add the import `"remna-control-bot/internal/payments"` to `telegram/orders.go`.

- [ ] **Step 4: Run** `gofmt -l internal; go vet ./... && go test ./internal/telegram/...` → PASS.
- [ ] **Step 5: Commit** `git add -A internal && git commit -m "feat(bot): Telegram Stars payments and provider buttons"`

---

### Task 11: WebApp frontend — provider buttons and history

**Files:** Modify `web/src/types.ts`, `web/src/telegram.ts`, `web/src/api.ts`, `web/src/pages/Checkout.tsx`, `web/src/ui/OrdersList.tsx`, `web/public/locales/ru/bot.json`

- [ ] **Step 1: Types.** `types.ts`: `OrderQuote.methods: string[]` and add `titles?: Record<string, string>`; `OrderRequest` gets `method?: string`; `Order.method: string` (was `'manual' | 'balance'`), `pay_url?: string`, and the status union gains `'awaiting_payment'`.
- [ ] **Step 2: `telegram.ts`.** Add to the `WebApp` interface: `openInvoice(url: string, cb?: (status: 'paid' | 'cancelled' | 'failed' | 'pending') => void): void`.
- [ ] **Step 3: `api.ts`.** `createOrder` takes `OrderRequest` (which now carries `method`) — no signature change needed; make sure the response type stays `{ order: Order; details: string }`.
- [ ] **Step 4: Locales** (`bot.json`): in `checkout` add `"provider_failed": "Не удалось создать счёт у платёжной системы. Попробуйте другой способ."`; in `orders` add `"pay": "Оплатить"` and, under `status`, `"awaiting_payment": "Ждёт оплату"`.
- [ ] **Step 5: `Checkout.tsx`.**
  - Imports: add `IconCreditCard` to the tabler import; `openExternal` to the `../telegram` import (`import { haptic, openExternal, tg } from '../telegram'`).
  - `run`'s catch: `setFailed(e.code === 'manual_off' ? t('checkout.manual_off') : e.code === 'provider_failed' ? t('checkout.provider_failed') : t('checkout.unavailable'))`.
  - After `payManual`, add:

```tsx
    // An online provider: the order is created together with its invoice, then the customer pays on the provider's page
    // (Stars: Telegram's own invoice sheet) and finds the result in the history.
    const payProvider = (method: string) =>
        run(async () => {
            const { order } = await createOrder({ ...request!, method })
            const url = order.pay_url!
            const refresh = () => {
                qc.invalidateQueries({ queryKey: ['me'] })
                qc.invalidateQueries({ queryKey: ['orders'] })
            }
            refresh()
            if (method === 'stars' && tg?.openInvoice) {
                tg.openInvoice(url, (status) => {
                    if (status === 'paid') {
                        haptic()
                        refresh()
                        navigate('/balance')
                    }
                })
                return
            }
            openExternal(url)
            navigate('/balance')
        })
```

  - In the methods `Stack`, after the `manual` button block and before the `locked` cancel button:

```tsx
                            {q.methods
                                .filter((m) => m !== 'balance' && m !== 'manual')
                                .map((m) => (
                                    <Button
                                        disabled={settling || locked}
                                        fullWidth
                                        key={m}
                                        leftSection={<IconCreditCard size={20} />}
                                        loading={busy}
                                        onClick={() => payProvider(m)}
                                        radius="xl"
                                        size="lg"
                                        variant="light"
                                    >
                                        {q.titles?.[m] ?? m}
                                    </Button>
                                ))}
```

- [ ] **Step 6: `OrdersList.tsx`.** Add `awaiting_payment: 'yellow'` to `colors`; import `openExternal` from `../telegram`; add the handler and block:

```tsx
    const payAgain = (o: Order) => {
        if (!o.pay_url) return
        if (o.method === 'stars' && tg?.openInvoice) tg.openInvoice(o.pay_url, (s) => s === 'paid' && qc.invalidateQueries({ queryKey: ['orders'] }))
        else openExternal(o.pay_url)
    }
```

```tsx
                        {o.status === 'awaiting_payment' && (
                            <Group gap="xs">
                                <Button disabled={pending !== null} onClick={() => payAgain(o)} radius="xl" size="xs" variant="light">{t('orders.pay')}</Button>
                                <Button color="gray" disabled={pending !== null} onClick={() => cancel(o.id)} radius="xl" size="xs" variant="subtle">{t('orders.cancel')}</Button>
                            </Group>
                        )}
```

- [ ] **Step 7: Typecheck** — run in `web/`: `bun run typecheck` → no errors.
- [ ] **Step 8: Commit** `git add web && git commit -m "feat(web): pay with online providers, pay again from the history"`

---

### Task 12: Wiring, docs, final verification

**Files:** Modify `cmd/bot/main.go`, `.env.example`, `README.md`

- [ ] **Step 1: `main.go`.** Imports: `strings`, `remna-control-bot/internal/payments`. After `svc := &orders.Service{…}` construction and before `telegram.New`, build the providers:

```go
	var stars *payments.Stars
	var provs []payments.Provider
	pc := cfg.Pay
	if pc.YooKassaOn() {
		provs = append(provs, &payments.YooKassa{ShopID: pc.YooKassaShopID, Secret: pc.YooKassaSecret, ReturnURL: pc.YooKassaReturnURL,
			ReceiptEmail: pc.YooKassaReceiptEmail, VAT: pc.YooKassaVAT})
	}
	if pc.CryptoBotOn() {
		provs = append(provs, &payments.CryptoBot{Token: pc.CryptoBotToken, Testnet: pc.CryptoBotTestnet, Assets: pc.CryptoBotAssets})
	}
	if pc.HeleketOn() {
		provs = append(provs, &payments.Heleket{MerchantID: pc.HeleketMerchantID, APIKey: pc.HeleketAPIKey, ToCurrency: pc.HeleketToCurrency,
			Network: pc.HeleketNetwork, CallbackURL: cfg.PublicURL + "/pay/heleket"})
	}
	if pc.StarsOn() {
		stars = &payments.Stars{RubPerStar: pc.StarsRubRate}
		provs = append(provs, stars)
	}
	svc.Providers = payments.NewRegistry(provs...)
	if len(provs) > 0 && !strings.HasPrefix(cfg.PublicURL, "https://") {
		log.Warn("payment providers are on but PUBLIC_URL is not https: webhooks will not reach the bot", "url", cfg.PublicURL)
	}
```

After `svc.Notify = bot` add `if stars != nil { stars.Link = bot.InvoiceLink }`. After the notify/sweep goroutines add `if len(provs) > 0 { go providerLoop(ctx, svc, log) }`, and add `"providers", svc.Providers.IDs()` to the `started` log line. Add:

```go
// providerLoop recovers lost provider webhooks and expires stale invoices.
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
```

- [ ] **Step 2: `.env.example`** — insert after the `ORDER_TTL` line:

```
# Online payment providers; each turns on when its keys are set. Webhooks go to
# PUBLIC_URL/pay/<provider> (yookassa, cryptobot, heleket) — PUBLIC_URL must be
# https and reachable from the internet.
YOOKASSA_SHOP_ID=
YOOKASSA_SECRET_KEY=
# After paying the customer lands here (default: the WebApp)
YOOKASSA_RETURN_URL=
# Fiscal receipt: set the customer e-mail (and the VAT code, 1..6) to send one
YOOKASSA_RECEIPT_EMAIL=
YOOKASSA_VAT_CODE=1
CRYPTOBOT_TOKEN=
CRYPTOBOT_TESTNET=false
# Accepted assets, e.g. USDT,TON (empty: all)
CRYPTOBOT_ASSETS=
HELEKET_MERCHANT_ID=
HELEKET_API_KEY=
HELEKET_TO_CURRENCY=
HELEKET_NETWORK=
# Roubles per Telegram Star; > 0 turns Stars on
STARS_RUB_RATE=
```

- [ ] **Step 3: `README.md`** — add rows to the configuration table (same style) for `YOOKASSA_SHOP_ID`, `YOOKASSA_SECRET_KEY`, `YOOKASSA_RETURN_URL`, `YOOKASSA_RECEIPT_EMAIL`, `YOOKASSA_VAT_CODE`, `CRYPTOBOT_TOKEN`, `CRYPTOBOT_TESTNET`, `CRYPTOBOT_ASSETS`, `HELEKET_MERCHANT_ID`, `HELEKET_API_KEY`, `HELEKET_TO_CURRENCY`, `HELEKET_NETWORK`, `STARS_RUB_RATE`, then a section after the table:

```markdown
## Payment providers

Customers can pay with **Telegram Stars**, **YooKassa** (cards, SBP), **CryptoBot** and **Heleket** (crypto) next to
manual transfer and the balance. A provider appears in the checkout when its keys are set. The payment is confirmed
automatically and applied exactly once; a payment that arrives for an expired or cancelled order is still applied, and
a wrong amount goes to the admin chat as a card.

Webhooks: the bot serves `POST {PUBLIC_URL}/pay/yookassa`, `/pay/cryptobot`, `/pay/heleket` (not under `WEBAPP_PATH`) —
route `/pay/*` to the bot's `HTTP_ADDR` in your reverse proxy, and register the URL in the provider's cabinet
(YooKassa: HTTP notifications for `payment.succeeded`; CryptoBot: Crypto Pay → My Apps → Webhooks; Heleket sends to
the callback URL the bot passes with every invoice). Stars need no webhook. If a webhook is lost, the bot asks the
provider about waiting invoices every minute.

Smoke test per provider (use the provider's test mode): YooKassa — a test shop and test card; CryptoBot — `CRYPTOBOT_TESTNET=true`
with @CryptoTestnetBot; Heleket — a test-mode merchant; Stars — pay a small order from a test account. Create a top-up
of the smallest amount, pay it, and check that the order turns *Оплачен*, the panel has a payment with the provider's
name as its method, and a second delivery of the webhook changes nothing.
```

- [ ] **Step 4: Verify everything.** Run, in order, and expect all to pass:

```bash
gofmt -l .                      # no output
go vet ./...
go test ./...
(cd web && bun run typecheck)
rm -f data/bot.db*              # dev database predates the new columns
```

- [ ] **Step 5: Update the spec** (`docs/superpowers/specs/2026-10-07-payment-providers-design.md`) with the plan's deviations listed at the top of this plan (method = provider id, `pay_amount`/`pay_currency`, `Pay(customerID, …)`, dedicated sweep loop, `Quote.Titles`, no `orders_pay` index), so spec and code agree.
- [ ] **Step 6: Commit** `git add -A && git commit -m "feat: wire payment providers, document setup"`

---

## Self-Review (done while writing)

**Spec coverage:** goal/flow → Tasks 7–10; framework + Registry → 2; YooKassa/CryptoBot/Heleket/Stars → 3–6; config table → 1 and 12; `awaiting_payment`, columns → 7; `Pay`/`Paid`/sweep/TTL, late money, amount check, review → 8; webhook route and status codes → 9; bot flow, `pre_checkout_query`, `successful_payment` → 10; WebApp/`openInvoice`/`openLink`, history "Оплатить" → 11; startup warning, README smoke tests → 12; security items map to the signature/API-read tests (3–5), body cap (9) and `CheckInvoice` (8). Admin card showing provider/external id: the card reuses the existing `comment` line (`reviewNote` names the provider) — a separate line was dropped as unnecessary (YAGNI).

**Placeholders:** none; every code step is complete. Two places say "if the compiler complains about unused imports" — those are import-list housekeeping, not missing logic.

**Type consistency:** `Event{Provider, OrderID, ExternalID, Paid, Review, Amount, Currency}`, `Invoice{ExternalID, URL, Amount, Currency}`, `InvoiceRequest{OrderID, Title, Amount, ExpiresIn}`, `Registry.{Get,Enabled,IDs,Title}`, `Service.{CreateWith, Pay, Paid, CheckInvoice, SweepProviders, Providers, NewID}`, `Order.{ExternalID, PayURL, PayAmount, PayCurrency}`, `view.{ProviderButton, Confirm, ProviderPay, CbOrderProvider, BtnPay, TxtProviderDown}`, `parseProviderCallback`, `(*Bot).InvoiceLink` are used identically across tasks.
