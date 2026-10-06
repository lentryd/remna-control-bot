# Payments: order core + manual payment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Customers pay for renewals, new subscriptions, add-ons, tariff changes and top-ups by sending a receipt; an admin approves (or corrects the amount / rejects) with buttons in a Telegram chat; the bot applies it in vpn-control and notifies the customer, who sees the history in the bot and WebApp.

**Architecture:** A new `internal/orders` package owns the order lifecycle (SQLite tables created by `internal/store`) and is the only code that moves money in the panel, through new `vpnctl.Client` methods over existing vpn-control endpoints. Telegram and WebApp are thin adapters: the bot implements `orders.Notifier` (admin card, customer messages), the WebApp exposes `/api/orders*`.

**Tech Stack:** Go 1.26, go-telegram/bot v1.23, Fiber v2, modernc SQLite; React 19 + Mantine 9 + TanStack Query (Bun build).

**Spec:** `docs/superpowers/specs/2026-10-07-payments-manual-design.md`

## Global Constraints

- Money is `float64` in major units (rubles) everywhere in the bot, rounded to kopecks with `math.Round(v*100)/100`; the panel API takes/returns major units.
- Price is computed server-side at order creation and never recomputed on approval; client-sent prices are ignored.
- Every status change goes through `UPDATE … WHERE id=? AND status IN (…)`; a lost race returns `orders.ErrStateChanged`.
- Panel payments carry `method` = order method (`manual`) and `note` = `"bot order #<id>"`.
- Manual payment is enabled only when `ADMIN_CHAT_ID`, `ADMIN_IDS` and `MANUAL_PAY_DETAILS` are all set; `ORDER_TTL` defaults to `48h`.
- Only Telegram ids in `ADMIN_IDS` may press admin-card buttons or answer admin prompts.
- Bot texts are Russian, HTML parse mode, escaped with `view.Esc`; view functions stay pure (golden/unit tested).
- Commits: no `Co-Authored-By` / Claude attribution lines (user rule).

## Review Focus

1. Admin double-taps "✅" (or two admins press at once) → the payment is recorded in the panel exactly once. Test in Task 5 (`TestApproveTwiceRecordsOnePayment`).
2. A customer sends a sticker/voice/video instead of a receipt → order stays `awaiting_receipt`, customer gets a hint. Test in Task 5 (`TestAttachReceiptRejectsUnknownKind`) + routing in Task 7.
3. A customer calls `/api/orders/{id}` or the bot `pc:<id>` with someone else's order id → 404 / ignored. Tests in Task 5 (`TestCancelOtherCustomer`) and Task 8 (`TestOrdersOwnership`).
4. Admin types "500,50" or "abc" as the corrected amount → comma accepted, garbage re-prompted, ≤0 rejected. Test in Task 7 (`TestParseAmount`).
5. Group members who are not admins press buttons or send `/start` in the admin group → nothing happens except an alert. Test in Task 7 (`TestAdminGate`).

---

## File Structure

**vpn-control (panel)**
- Modify `web/src/components/api-tokens/modals/create-api-token-modal.tsx` — bot preset scopes.

**vpn-control-bot**
- Modify `internal/config/config.go` (+test) — admin/manual settings.
- Create `internal/vpnctl/billing.go` (+`billing_test.go`) — payment/extend/provision API methods and types; modify `client.go` (interface), `fake.go` (fake behaviour).
- Modify `internal/store/store.go` (+test) — `orders`, `chat_state`, `admin_prompts` tables; `DB()` accessor; chat state / prompt helpers.
- Create `internal/orders/` — `order.go` (types), `repo.go` (SQL), `pricing.go` (term cost port), `service.go` (quote/create/receipt/cancel/expire), `fulfill.go` (admin actions + panel calls), tests alongside.
- Delete `internal/billing/billing.go` stub (keep `split.go`).
- Create `internal/telegram/view/orders.go` (+test) — pay screen, period picker, admin card, customer notices, history.
- Create `internal/telegram/orders.go` (+test) — `Notifier` implementation, receipt intake, customer order callbacks, renew-in-bot flow.
- Create `internal/telegram/admin.go` (+test) — admin gate, admin callbacks, prompt replies, amount parsing.
- Modify `internal/telegram/bot.go`, `route.go`, `private.go`, `view/screen.go`, `view/texts.go`, `view/screens.go`.
- Create `internal/webapp/orders.go` (+test); modify `server.go`.
- Modify `internal/notify/runner.go` — expiry sweep hook; `cmd/bot/main.go` — wiring.
- Web: modify `src/types.ts`, `src/api.ts`, `src/App.tsx`, `src/renew.ts`, `src/pages/{SubscriptionDetails,Balance,Tariffs}.tsx`, `src/ui/BalanceCard.tsx`, `public/locales/ru/bot.json`; create `src/pages/Checkout.tsx`, `src/ui/OrdersList.tsx`.
- Modify `README` / `.env.example` (whichever documents env; check `ls`) — new variables.

---

### Task 1: Panel — extend the `bot` token preset

**Files:** Modify `/Users/victor/Projects/Go/vpn-control/web/src/components/api-tokens/modals/create-api-token-modal.tsx:28-39`

- [ ] **Step 1: Replace the preset**

```ts
// What vpn-control-bot needs: find/create customers, link a Telegram id,
// read their card (subscriptions, balance, referrals) and the tariffs;
// the two lists are for notifications (traffic, payment reminders); the
// rest takes payments: quote and extend items, record payments, provision
// subscriptions, connect add-ons and change tariffs.
const BOT_PRESET_KEYS = [
    'customers:lookup',
    'customers:get',
    'customers:create',
    'customers:update',
    'customers:list',
    'customers:payment_preview',
    'customers:pay',
    'subscriptions:list',
    'subscriptions:provision',
    'subscriptions:connect_addon',
    'items:quote',
    'items:extend',
    'items:tariff_quote',
    'items:tariff',
    'tariffs:list'
]
```

- [ ] **Step 2: Typecheck** — Run (in `/Users/victor/Projects/Go/vpn-control/web`): `bun run typecheck` (or the script in its package.json). Expected: no errors.
- [ ] **Step 3: Commit (in vpn-control repo)**

```bash
git -C /Users/victor/Projects/Go/vpn-control add web/src/components/api-tokens/modals/create-api-token-modal.tsx
git -C /Users/victor/Projects/Go/vpn-control commit -m "feat(tokens): bot preset can take payments"
```

---

### Task 2: Config — admin chat, admins, payment details, TTL

**Files:** Modify `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces — Produces:** `Config.AdminChatID int64`, `Config.AdminIDs []int64`, `Config.ManualPayDetails string`, `Config.OrderTTL time.Duration`, `func (c Config) ManualEnabled() bool`, `func (c Config) IsAdmin(id int64) bool`.

- [ ] **Step 1: Failing test** (append to `config_test.go`; it already has a helper building `get` from a map — reuse it; if it is named differently, use that name)

```go
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
```

- [ ] **Step 2: Run** `go test ./internal/config/ -run TestManualPayment` → FAIL (undefined fields).
- [ ] **Step 3: Implement.** Add fields to `Config`:

```go
	AdminChatID      int64         // where receipts go; 0 disables manual payment
	AdminIDs         []int64       // who may decide on them
	ManualPayDetails string        // where to send money, shown with the amount
	OrderTTL         time.Duration // unpaid orders expire after this
```

Methods:

```go
// ManualEnabled: receipts need somewhere to go, someone to decide and
// details to pay by.
func (c Config) ManualEnabled() bool {
	return c.AdminChatID != 0 && len(c.AdminIDs) > 0 && c.ManualPayDetails != ""
}

func (c Config) IsAdmin(id int64) bool { return slices.Contains(c.AdminIDs, id) }
```

In `load`, before the `BOT_MODE` check (`MANUAL_PAY_DETAILS` keeps inner newlines; `or` trims only the ends):

```go
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
```

Add `"slices"` to imports.
- [ ] **Step 4: Run** `go test ./internal/config/` → PASS.
- [ ] **Step 5: Commit** `git add internal/config && git commit -m "feat(config): admin chat, admins, manual payment details, order TTL"`

---

### Task 3: vpnctl — payment, extension, provisioning methods

**Files:** Create `internal/vpnctl/billing.go`, `internal/vpnctl/billing_test.go`; modify `internal/vpnctl/client.go` (interface), `internal/vpnctl/fake.go`.

**Interfaces — Produces** (package `vpnctl`):

```go
type ExtendQuote struct {
	CustomerID int       `json:"customer_id"`
	Amount     float64   `json:"amount"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Monthly    float64   `json:"monthly"`
	Periods    []Period  `json:"periods"`
	Balance    float64   `json:"balance"`
}
type TariffQuote struct {
	OldMonthly float64    `json:"old_monthly"`
	NewMonthly float64    `json:"new_monthly"`
	ExpireAt   *time.Time `json:"expire_at"`
	Surcharge  float64    `json:"surcharge"`
}
type Allocation struct {
	Kind   string  `json:"kind"` // subscription | addon
	ID     int     `json:"id"`
	Months int     `json:"months"`
	Days   int     `json:"days"`
	Amount float64 `json:"amount"`
}
type PaymentInput struct {
	Amount      float64      `json:"amount"`
	Method      string       `json:"method"`
	Note        string       `json:"note"`
	Allocations []Allocation `json:"allocations"`
}
type Extension struct {
	Kind   string     `json:"kind"`
	ID     int        `json:"id"`
	Title  string     `json:"title"`
	Months int        `json:"months"`
	Days   int        `json:"days"`
	Amount float64    `json:"amount"`
	From   *time.Time `json:"from"`
	To     *time.Time `json:"to"`
	OK     bool       `json:"ok"`
	Error  string     `json:"error"`
}
type PaymentResult struct {
	PaymentID  int         `json:"payment_id"`
	Extensions []Extension `json:"extensions"`
	Balance    float64     `json:"balance"`
}
type ExtendInput struct {
	Months    int     `json:"months"`
	Days      int     `json:"days"`
	Amount    float64 `json:"amount"`
	AllowDebt bool    `json:"allow_debt"`
}
type ProvisionInput struct {
	CustomerID int     `json:"customer_id"`
	TariffID   int     `json:"tariff_id"`
	Username   string  `json:"username"`
	Label      string  `json:"label"`
	Months     int     `json:"months"`
	Days       int     `json:"days"`
	Amount     float64 `json:"amount"`
	AllowDebt  bool    `json:"allow_debt"`
}
type ConnectAddonInput struct {
	TariffID  int     `json:"tariff_id"`
	Months    int     `json:"months"`
	Days      int     `json:"days"`
	Amount    float64 `json:"amount"`
	AllowDebt bool    `json:"allow_debt"`
}
type ChangeTariffInput struct {
	TariffID  int     `json:"tariff_id"`
	Surcharge float64 `json:"surcharge"`
}
```

Interface additions to `Client`:

```go
	QuoteExtend(ctx context.Context, kind string, id, months, days int) (*ExtendQuote, error)
	Extend(ctx context.Context, kind string, id int, in ExtendInput) (*Extension, error)
	QuoteTariff(ctx context.Context, kind string, id, tariffID int) (*TariffQuote, error)
	ChangeTariff(ctx context.Context, kind string, id int, in ChangeTariffInput) error
	Pay(ctx context.Context, customerID int, in PaymentInput) (*PaymentResult, error)
	Provision(ctx context.Context, in ProvisionInput) (int, error)
	ConnectAddon(ctx context.Context, subscriptionID int, in ConnectAddonInput) (int, error)
```

Fake additions (exported fields tests use): `Quotes map[string]ExtendQuote` (key `"subscription:3"`), `TariffQuotes map[string]TariffQuote` (key `"subscription:3:9"`), `Payments []PaymentInput`, `Extends []ExtendCall`, `Provisions []ProvisionInput`, `AddonCalls []AddonCall`, `TariffChanges []TariffCall`, `FailExtend string` (non-empty → extensions/Extend fail with this error), `FailStep error` (returned by Provision/ConnectAddon/ChangeTariff), `nextPaymentID`.

```go
type ExtendCall struct {
	Kind string
	ID   int
	In   ExtendInput
}
type AddonCall struct {
	SubscriptionID int
	In             ConnectAddonInput
}
type TariffCall struct {
	Kind string
	ID   int
	In   ChangeTariffInput
}
```

- [ ] **Step 1: Failing HTTP test** `internal/vpnctl/billing_test.go`

```go
package vpnctl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPay(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/customers/5/payments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		var in PaymentInput
		json.NewDecoder(r.Body).Decode(&in)
		if in.Amount != 700 || in.Method != "manual" || len(in.Allocations) != 1 || in.Allocations[0].Amount != 900 {
			t.Errorf("body %+v", in)
		}
		w.Write([]byte(`{"payment_id":42,"balance":0,"extensions":[{"kind":"subscription","id":3,"ok":true,"to":"2027-01-12T00:00:00Z"}]}`))
	})
	res, err := c.Pay(context.Background(), 5, PaymentInput{Amount: 700, Method: "manual", Note: "bot order #1",
		Allocations: []Allocation{{Kind: "subscription", ID: 3, Months: 3, Amount: 900}}})
	if err != nil || res.PaymentID != 42 || !res.Extensions[0].OK || res.Extensions[0].To.Year() != 2027 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestQuoteExtendAndProvision(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/items/addon/8/quote":
			if r.URL.Query().Get("months") != "3" || r.URL.Query().Get("days") != "0" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"customer_id":5,"amount":450.5,"from":"2026-10-07T00:00:00Z","to":"2027-01-07T00:00:00Z","monthly":150,"periods":[],"balance":10}`))
		case "/api/v1/subscriptions/provision":
			w.WriteHeader(201)
			w.Write([]byte(`{"id":77}`))
		case "/api/v1/subscriptions/3/addons":
			w.WriteHeader(201)
			w.Write([]byte(`{"id":78}`))
		case "/api/v1/items/subscription/3/tariff-quote":
			w.Write([]byte(`{"old_monthly":100,"new_monthly":200,"surcharge":66.67}`))
		case "/api/v1/items/subscription/3/tariff":
			w.WriteHeader(204)
		case "/api/v1/items/subscription/3/extend":
			w.Write([]byte(`{"kind":"subscription","id":3,"ok":true}`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	q, err := c.QuoteExtend(ctx, "addon", 8, 3, 0)
	if err != nil || q.Amount != 450.5 || q.CustomerID != 5 {
		t.Fatalf("quote %+v %v", q, err)
	}
	if id, err := c.Provision(ctx, ProvisionInput{CustomerID: 5, TariffID: 2, Username: "bot_c5_o1", Months: 1, Amount: 100}); err != nil || id != 77 {
		t.Fatalf("provision %d %v", id, err)
	}
	if id, err := c.ConnectAddon(ctx, 3, ConnectAddonInput{TariffID: 9, Months: 1, Amount: 50}); err != nil || id != 78 {
		t.Fatalf("addon %d %v", id, err)
	}
	if tq, err := c.QuoteTariff(ctx, "subscription", 3, 9); err != nil || tq.Surcharge != 66.67 {
		t.Fatalf("tariff quote %+v %v", tq, err)
	}
	if err := c.ChangeTariff(ctx, "subscription", 3, ChangeTariffInput{TariffID: 9, Surcharge: 66.67}); err != nil {
		t.Fatal(err)
	}
	if e, err := c.Extend(ctx, "subscription", 3, ExtendInput{Months: 1, Amount: 100}); err != nil || !e.OK {
		t.Fatalf("extend %+v %v", e, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/vpnctl/ -run 'TestPay|TestQuoteExtend'` → FAIL (undefined).
- [ ] **Step 3: Implement `billing.go`** (types above, then):

```go
func itemPath(kind string, id int) string { return "/items/" + kind + "/" + strconv.Itoa(id) }

func (c *HTTPClient) QuoteExtend(ctx context.Context, kind string, id, months, days int) (*ExtendQuote, error) {
	var out ExtendQuote
	q := url.Values{"months": {strconv.Itoa(months)}, "days": {strconv.Itoa(days)}}
	return &out, c.do(ctx, http.MethodGet, itemPath(kind, id)+"/quote?"+q.Encode(), nil, &out)
}

func (c *HTTPClient) Extend(ctx context.Context, kind string, id int, in ExtendInput) (*Extension, error) {
	var out Extension
	return &out, c.do(ctx, http.MethodPost, itemPath(kind, id)+"/extend", in, &out)
}

func (c *HTTPClient) QuoteTariff(ctx context.Context, kind string, id, tariffID int) (*TariffQuote, error) {
	var out TariffQuote
	return &out, c.do(ctx, http.MethodGet, itemPath(kind, id)+"/tariff-quote?tariff_id="+strconv.Itoa(tariffID), nil, &out)
}

func (c *HTTPClient) ChangeTariff(ctx context.Context, kind string, id int, in ChangeTariffInput) error {
	return c.do(ctx, http.MethodPost, itemPath(kind, id)+"/tariff", in, nil)
}

func (c *HTTPClient) Pay(ctx context.Context, customerID int, in PaymentInput) (*PaymentResult, error) {
	if in.Allocations == nil {
		in.Allocations = []Allocation{}
	}
	var out PaymentResult
	return &out, c.do(ctx, http.MethodPost, "/customers/"+strconv.Itoa(customerID)+"/payments", in, &out)
}

func (c *HTTPClient) Provision(ctx context.Context, in ProvisionInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	return out.ID, c.do(ctx, http.MethodPost, "/subscriptions/provision", in, &out)
}

func (c *HTTPClient) ConnectAddon(ctx context.Context, subscriptionID int, in ConnectAddonInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	return out.ID, c.do(ctx, http.MethodPost, "/subscriptions/"+strconv.Itoa(subscriptionID)+"/addons", in, &out)
}
```

Note: `do` already handles a 204 when `out == nil`. Add the interface lines to `client.go`.

- [ ] **Step 4: Implement the Fake** (append to `fake.go`):

```go
func itemKey(kind string, id int) string { return kind + ":" + strconv.Itoa(id) }

func (f *Fake) QuoteExtend(_ context.Context, kind string, id, months, days int) (*ExtendQuote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	q, ok := f.Quotes[itemKey(kind, id)]
	if !ok {
		return nil, &APIError{Status: 404}
	}
	q.Amount *= float64(months) + float64(days)/30 // Quotes hold the one-month price
	q.Amount = math.Round(q.Amount*100) / 100
	return &q, nil
}

func (f *Fake) extension(kind string, id, months, days int, amount float64) Extension {
	e := Extension{Kind: kind, ID: id, Months: months, Days: days, Amount: amount, OK: f.FailExtend == ""}
	if e.OK {
		to := time.Date(2027, 1, 12, 0, 0, 0, 0, time.UTC)
		e.To = &to
	} else {
		e.Error = f.FailExtend
	}
	return e
}

func (f *Fake) Extend(_ context.Context, kind string, id int, in ExtendInput) (*Extension, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.Extends = append(f.Extends, ExtendCall{Kind: kind, ID: id, In: in})
	e := f.extension(kind, id, in.Months, in.Days, in.Amount)
	if !e.OK {
		return nil, &APIError{Status: 400, Message: e.Error}
	}
	return &e, nil
}

func (f *Fake) QuoteTariff(_ context.Context, kind string, id, tariffID int) (*TariffQuote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	q, ok := f.TariffQuotes[itemKey(kind, id)+":"+strconv.Itoa(tariffID)]
	if !ok {
		return nil, &APIError{Status: 400}
	}
	return &q, nil
}

func (f *Fake) ChangeTariff(_ context.Context, kind string, id int, in ChangeTariffInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if f.FailStep != nil {
		return f.FailStep
	}
	f.TariffChanges = append(f.TariffChanges, TariffCall{Kind: kind, ID: id, In: in})
	return nil
}

func (f *Fake) Pay(_ context.Context, customerID int, in PaymentInput) (*PaymentResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.Payments = append(f.Payments, in)
	f.nextPaymentID++
	res := &PaymentResult{PaymentID: f.nextPaymentID}
	for _, a := range in.Allocations {
		res.Extensions = append(res.Extensions, f.extension(a.Kind, a.ID, a.Months, a.Days, a.Amount))
	}
	return res, nil
}

func (f *Fake) Provision(_ context.Context, in ProvisionInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	if f.FailStep != nil {
		return 0, f.FailStep
	}
	f.Provisions = append(f.Provisions, in)
	return 100 + len(f.Provisions), nil
}

func (f *Fake) ConnectAddon(_ context.Context, subscriptionID int, in ConnectAddonInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	if f.FailStep != nil {
		return 0, f.FailStep
	}
	f.AddonCalls = append(f.AddonCalls, AddonCall{SubscriptionID: subscriptionID, In: in})
	return 200 + len(f.AddonCalls), nil
}
```

Add fields to the `Fake` struct and imports `math`, `strconv`, `time`.
- [ ] **Step 5: Run** `go test ./internal/vpnctl/ && go vet ./...` → PASS (the other packages still compile since `*HTTPClient`/`*Fake` satisfy the wider interface).
- [ ] **Step 6: Commit** `git add internal/vpnctl && git commit -m "feat(vpnctl): payments, extensions, provisioning and tariff change"`

---

### Task 4: Store — orders, chat state, admin prompts

**Files:** Modify `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces — Produces:**
- `func (s *Store) DB() *sql.DB`
- `type ChatState struct{ Kind string; OrderID int }`; `SetChatState(ctx, chatID int64, st ChatState) error`, `GetChatState(ctx, chatID int64) (ChatState, bool, error)`, `ClearChatState(ctx, chatID int64) error`
- `type Prompt struct{ Kind string; OrderID int }`; `SavePrompt(ctx, chatID int64, msgID int, p Prompt) error`, `TakePrompt(ctx, chatID int64, msgID int) (Prompt, bool, error)` (deletes it).

- [ ] **Step 1: Failing test** (append to `store_test.go`; reuse its existing opener helper — the file opens a temp DB; if the helper is named differently, use it)

```go
func TestChatStateAndPrompts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, ok, _ := s.GetChatState(ctx, 1); ok {
		t.Fatal("state before set")
	}
	s.SetChatState(ctx, 1, ChatState{Kind: "receipt", OrderID: 9})
	s.SetChatState(ctx, 1, ChatState{Kind: "receipt", OrderID: 10}) // replaces
	if st, ok, err := s.GetChatState(ctx, 1); !ok || err != nil || st.OrderID != 10 {
		t.Fatalf("state %+v %v %v", st, ok, err)
	}
	s.ClearChatState(ctx, 1)
	if _, ok, _ := s.GetChatState(ctx, 1); ok {
		t.Fatal("state after clear")
	}
	s.SavePrompt(ctx, -100, 55, Prompt{Kind: "reject", OrderID: 3})
	if p, ok, _ := s.TakePrompt(ctx, -100, 55); !ok || p.OrderID != 3 || p.Kind != "reject" {
		t.Fatalf("prompt %+v", p)
	}
	if _, ok, _ := s.TakePrompt(ctx, -100, 55); ok {
		t.Fatal("prompt taken twice")
	}
	if _, err := s.DB().Exec(`SELECT id, status FROM orders`); err != nil {
		t.Fatalf("orders table: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/store/` → FAIL.
- [ ] **Step 3: Implement.** Extend the `CREATE TABLE` batch in `Open`:

```sql
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_id INTEGER NOT NULL,
		chat_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		item_kind TEXT NOT NULL DEFAULT '',
		item_id INTEGER NOT NULL DEFAULT 0,
		tariff_id INTEGER NOT NULL DEFAULT 0,
		months INTEGER NOT NULL DEFAULT 0,
		days INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL,
		price REAL NOT NULL,
		from_balance REAL NOT NULL,
		to_pay REAL NOT NULL,
		received REAL,
		method TEXT NOT NULL,
		status TEXT NOT NULL,
		receipt_kind TEXT NOT NULL DEFAULT '',
		receipt_file_id TEXT NOT NULL DEFAULT '',
		comment TEXT NOT NULL DEFAULT '',
		reject_reason TEXT NOT NULL DEFAULT '',
		admin TEXT NOT NULL DEFAULT '',
		admin_msg_id INTEGER NOT NULL DEFAULT 0,
		panel_payment_id INTEGER NOT NULL DEFAULT 0,
		result_until TIMESTAMP,
		error TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL);
	CREATE INDEX IF NOT EXISTS orders_customer ON orders (customer_id, id);
	CREATE INDEX IF NOT EXISTS orders_status ON orders (status, created_at);
	CREATE TABLE IF NOT EXISTS chat_state (
		chat_id INTEGER PRIMARY KEY,
		kind TEXT NOT NULL,
		order_id INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS admin_prompts (
		chat_id INTEGER NOT NULL,
		msg_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		order_id INTEGER NOT NULL,
		PRIMARY KEY (chat_id, msg_id))
```

(The spec put prompts into `chat_state`; a separate table keyed by message keeps several pending prompts in one group apart — same behaviour, simpler queries.)

Helpers:

```go
func (s *Store) DB() *sql.DB { return s.db }

// ChatState is what the next message in a chat means (e.g. a receipt for an order).
type ChatState struct {
	Kind    string
	OrderID int
}

func (s *Store) SetChatState(ctx context.Context, chatID int64, st ChatState) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO chat_state (chat_id, kind, order_id) VALUES (?, ?, ?)`, chatID, st.Kind, st.OrderID)
	return err
}

func (s *Store) GetChatState(ctx context.Context, chatID int64) (ChatState, bool, error) {
	var st ChatState
	err := s.db.QueryRowContext(ctx, `SELECT kind, order_id FROM chat_state WHERE chat_id = ?`, chatID).Scan(&st.Kind, &st.OrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	return st, err == nil, err
}

func (s *Store) ClearChatState(ctx context.Context, chatID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_state WHERE chat_id = ?`, chatID)
	return err
}

// Prompt is an admin question (reject reason, received amount) awaiting a reply.
type Prompt struct {
	Kind    string
	OrderID int
}

func (s *Store) SavePrompt(ctx context.Context, chatID int64, msgID int, p Prompt) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO admin_prompts (chat_id, msg_id, kind, order_id) VALUES (?, ?, ?, ?)`, chatID, msgID, p.Kind, p.OrderID)
	return err
}

// TakePrompt returns and forgets the prompt a reply answers.
func (s *Store) TakePrompt(ctx context.Context, chatID int64, msgID int) (Prompt, bool, error) {
	var p Prompt
	err := s.db.QueryRowContext(ctx, `DELETE FROM admin_prompts WHERE chat_id = ? AND msg_id = ? RETURNING kind, order_id`, chatID, msgID).Scan(&p.Kind, &p.OrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
```

Update the package doc comment: "…which notifications were already sent, payment orders and what a chat is expected to send next." Add `"errors"` import.
- [ ] **Step 4: Run** `go test ./internal/store/` → PASS.
- [ ] **Step 5: Commit** `git add internal/store && git commit -m "feat(store): orders, chat state and admin prompts tables"`

---

### Task 5: `internal/orders` — model, repo, pricing, quote/create/receipt/cancel/expire

**Files:** Create `internal/orders/order.go`, `repo.go`, `pricing.go`, `service.go`, `pricing_test.go`, `service_test.go`, `helpers_test.go`.

**Interfaces — Consumes:** Task 3 `vpnctl` methods; Task 4 `store.Store` (`DB()`, chat state); `billing.Split`.

**Produces:**

```go
type Kind string   // KindRenew "renew", KindSubscribe "subscribe", KindAddon "addon", KindChangeTariff "change_tariff", KindTopup "topup"
type Status string // StatusNew "new", StatusAwaitingReceipt "awaiting_receipt", StatusPendingReview "pending_review",
                   // StatusApproving "approving", StatusApproved "approved", StatusCredited "credited", StatusFailed "failed",
                   // StatusRejected "rejected", StatusExpired "expired", StatusCancelled "cancelled"
type Method string // MethodManual "manual", MethodBalance "balance"
type ReceiptKind string // ReceiptPhoto "photo", ReceiptDocument "document", ReceiptText "text"

type Request struct {
	Kind     Kind    `json:"kind"`
	ItemKind string  `json:"item_kind"` // renew, change_tariff: subscription | addon
	ItemID   int     `json:"item_id"`   // renew, change_tariff: the item; addon: the subscription
	TariffID int     `json:"tariff_id"` // subscribe, addon, change_tariff
	Months   int     `json:"months"`
	Days     int     `json:"days"`
	Amount   float64 `json:"amount"`    // topup only
}
type Quote struct {
	Title       string   `json:"title"`
	Price       float64  `json:"price"`
	FromBalance float64  `json:"from_balance"`
	ToPay       float64  `json:"to_pay"`
	Methods     []Method `json:"methods"`
}
type Order struct {
	ID, CustomerID int; ChatID int64
	Request                         // embedded: Kind, ItemKind, ItemID, TariffID, Months, Days, Amount (Amount unused after creation)
	Title string; Price, FromBalance, ToPay float64; Received *float64
	Method Method; Status Status
	ReceiptKind ReceiptKind; ReceiptFileID, Comment, RejectReason, Admin string
	AdminMsgID, PanelPaymentID int; ResultUntil *time.Time; Error string
	CreatedAt, UpdatedAt time.Time
}
func (o Order) Paid() float64 // *Received if set, else ToPay

type Notifier interface {
	PayScreen(ctx context.Context, o Order, details string) error // customer chat: amount + details + buttons
	PostCard(ctx context.Context, o Order) (msgID int, err error)    // admin chat
	UpdateCard(ctx context.Context, o Order) error
	Customer(ctx context.Context, o Order) error                     // decision notice to the customer
}

type ChatStates interface { // implemented by *store.Store
	SetChatState(ctx context.Context, chatID int64, st store.ChatState) error
	GetChatState(ctx context.Context, chatID int64) (store.ChatState, bool, error)
	ClearChatState(ctx context.Context, chatID int64) error
}

type Service struct {
	DB      *sql.DB
	Chats   ChatStates
	API     vpnctl.Client
	PayMode config.PayMode
	Manual  bool
	Details string
	TTL     time.Duration
	Notify  Notifier
	Now     func() time.Time
	Log     *slog.Logger
}

var (
	ErrStateChanged = errors.New("orders: state changed")
	ErrNotFound     = errors.New("orders: not found")
	ErrBadRequest   = errors.New("orders: bad request")
	ErrManualOff    = errors.New("orders: manual payment is off")
	ErrUnderpaid    = errors.New("orders: received less than to pay")
)

func (s *Service) Quote(ctx context.Context, c *vpnctl.Customer, r Request) (Quote, error)
func (s *Service) Create(ctx context.Context, c *vpnctl.Customer, chatID int64, r Request) (*Order, error)
func (s *Service) RequestReceipt(ctx context.Context, customerID, id int) (*Order, error)
func (s *Service) AttachReceipt(ctx context.Context, chatID int64, kind ReceiptKind, fileID, comment string) (*Order, error)
func (s *Service) Cancel(ctx context.Context, customerID, id int) error
func (s *Service) Get(ctx context.Context, id int) (*Order, error)
func (s *Service) ForCustomer(ctx context.Context, customerID, id int) (*Order, error)
func (s *Service) List(ctx context.Context, customerID, limit, offset int) ([]Order, int, error) // orders, total
func (s *Service) ExpireStale(ctx context.Context) error
```

- [ ] **Step 1: Pricing test** `pricing_test.go` (mirrors vpn-control `TestTermCost`, in rubles):

```go
package orders

import (
	"testing"

	"vpn-control-bot/internal/vpnctl"
)

func TestTermCost(t *testing.T) {
	periods := []vpnctl.Period{{Months: 3, Price: 250}, {Days: 7, Price: 0}, {Months: 1, Days: 15, Price: 120}}
	cases := []struct {
		months, days int
		want         float64
	}{{0, 7, 0}, {1, 15, 120}, {0, 6, 20}, {3, 0, 250}, {3, 7, 250 + 23.33}, {1, 0, 100}}
	for _, c := range cases {
		if got := TermCost(100, periods, c.months, c.days); got != c.want {
			t.Errorf("TermCost(%d, %d) = %v, want %v", c.months, c.days, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/orders/` → FAIL (no package).
- [ ] **Step 3: `pricing.go`** — port of vpn-control `billing.Cost/DaysCost/TermCost` in kopecks:

```go
package orders

import (
	"math"

	"vpn-control-bot/internal/vpnctl"
)

// TermCost prices months+days of a tariff like vpn-control does (keep in
// sync with its internal/billing/pricing.go): an exact package wins,
// otherwise the cheapest mix of whole-month periods plus monthly/30 a day.
func TermCost(monthly float64, periods []vpnctl.Period, months, days int) float64 {
	if days > 0 {
		for _, p := range periods {
			if p.Months == months && p.Days == days {
				return p.Price
			}
		}
	}
	m := kop(monthly)
	best := make([]int64, max(months, 0)+1)
	for i := 1; i <= months; i++ {
		best[i] = best[i-1] + m
		for _, p := range periods {
			if p.Days == 0 && p.Months > 0 && p.Months <= i && best[i-p.Months]+kop(p.Price) < best[i] {
				best[i] = best[i-p.Months] + kop(p.Price)
			}
		}
	}
	total := best[max(months, 0)] + int64(math.Round(float64(m)*float64(days)/30))
	return float64(total) / 100
}

func kop(v float64) int64        { return int64(math.Round(v * 100)) }
func round2(v float64) float64   { return math.Round(v*100) / 100 }
```

- [ ] **Step 4: Run** `go test ./internal/orders/ -run TestTermCost` → PASS.
- [ ] **Step 5: `order.go`** — the types listed under Produces (constants, `Request`, `Quote`, `Order`, `Paid()`, errors, `Notifier`, `ChatStates`), plus:

```go
// Final statuses never change again.
func (s Status) Final() bool {
	switch s {
	case StatusApproved, StatusCredited, StatusRejected, StatusExpired, StatusCancelled:
		return true
	}
	return false
}

func (o Order) Paid() float64 {
	if o.Received != nil {
		return *o.Received
	}
	return o.ToPay
}

// Note links the panel payment back to the order.
func (o Order) Note() string { return fmt.Sprintf("bot order #%d", o.ID) }
```

- [ ] **Step 6: `repo.go`** — SQL over `s.DB`:

```go
package orders

const cols = `id, customer_id, chat_id, kind, item_kind, item_id, tariff_id, months, days, title, price, from_balance, to_pay,
	received, method, status, receipt_kind, receipt_file_id, comment, reject_reason, admin, admin_msg_id, panel_payment_id,
	result_until, error, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(r scanner) (*Order, error) {
	var o Order
	var received sql.NullFloat64
	var until sql.NullTime
	err := r.Scan(&o.ID, &o.CustomerID, &o.ChatID, &o.Kind, &o.ItemKind, &o.ItemID, &o.TariffID, &o.Months, &o.Days,
		&o.Title, &o.Price, &o.FromBalance, &o.ToPay, &received, &o.Method, &o.Status, &o.ReceiptKind, &o.ReceiptFileID,
		&o.Comment, &o.RejectReason, &o.Admin, &o.AdminMsgID, &o.PanelPaymentID, &until, &o.Error, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if received.Valid {
		o.Received = &received.Float64
	}
	if until.Valid {
		o.ResultUntil = &until.Time
	}
	return &o, err
}

func (s *Service) insert(ctx context.Context, o *Order) error {
	now := s.Now()
	o.CreatedAt, o.UpdatedAt = now, now
	res, err := s.DB.ExecContext(ctx, `INSERT INTO orders (customer_id, chat_id, kind, item_kind, item_id, tariff_id, months, days,
		title, price, from_balance, to_pay, method, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.CustomerID, o.ChatID, o.Kind, o.ItemKind, o.ItemID, o.TariffID, o.Months, o.Days,
		o.Title, o.Price, o.FromBalance, o.ToPay, o.Method, o.Status, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	o.ID = int(id)
	return err
}

func (s *Service) Get(ctx context.Context, id int) (*Order, error) {
	return scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM orders WHERE id = ?`, id))
}

// ForCustomer is Get limited to the customer's own orders.
func (s *Service) ForCustomer(ctx context.Context, customerID, id int) (*Order, error) {
	return scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM orders WHERE id = ? AND customer_id = ?`, id, customerID))
}

// List is the customer's orders, newest first, without drafts nobody paid
// for (expired/cancelled ones stay — they explain a missing renewal).
func (s *Service) List(ctx context.Context, customerID, limit, offset int) ([]Order, int, error) {
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE customer_id = ? AND status != ?`, customerID, StatusNew).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM orders WHERE customer_id = ? AND status != ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		customerID, StatusNew, limit, offset)
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

// move changes the status only from one of the expected ones; set holds
// extra columns to write in the same statement ("col = ?" → value).
func (s *Service) move(ctx context.Context, id int, from []Status, to Status, set map[string]any) error {
	q := `UPDATE orders SET status = ?, updated_at = ?`
	args := []any{to, s.Now()}
	for _, col := range slices.Sorted(maps.Keys(set)) {
		q += ", " + col + " = ?"
		args = append(args, set[col])
	}
	q += ` WHERE id = ? AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(from)), ",") + `)`
	args = append(args, id)
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStateChanged
	}
	return nil
}

// set writes columns without a status change (e.g. the admin card id).
func (s *Service) set(ctx context.Context, id int, cols map[string]any) error {
	o, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.move(ctx, id, []Status{o.Status}, o.Status, cols)
}
```

Imports: `context`, `database/sql`, `errors`, `maps`, `slices`, `strings`.

- [ ] **Step 7: Test helpers + service tests** `helpers_test.go`:

```go
package orders

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

type spy struct {
	mu                         sync.Mutex
	pay, posted, updated, sent []Order
}

func (s *spy) PayScreen(_ context.Context, o Order, _ string) error { s.mu.Lock(); defer s.mu.Unlock(); s.pay = append(s.pay, o); return nil }
func (s *spy) PostCard(_ context.Context, o Order) (int, error)   { s.mu.Lock(); defer s.mu.Unlock(); s.posted = append(s.posted, o); return 500 + o.ID, nil }
func (s *spy) UpdateCard(_ context.Context, o Order) error        { s.mu.Lock(); defer s.mu.Unlock(); s.updated = append(s.updated, o); return nil }
func (s *spy) Customer(_ context.Context, o Order) error          { s.mu.Lock(); defer s.mu.Unlock(); s.sent = append(s.sent, o); return nil }

func ptr[T any](v T) *T { return &v }

// setup: customer 5 (tg 77) with subscription 3 (tariff 1, 300/month) and
// add-on 8 on it; balance own 100, referral 0; tariffs: base 1 (300/mo,
// 3 months for 800), base 2 (500/mo), addon 9 (50/mo).
func setup(t *testing.T) (*Service, *vpnctl.Fake, *spy, *vpnctl.Customer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := vpnctl.NewFake()
	exp := now.AddDate(0, 0, 2)
	c := f.Add(vpnctl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Balance: 100, BalanceOwn: 100,
		Subscriptions: []vpnctl.Subscription{{ID: 3, Title: "Дом", TariffID: ptr(1), Price: 300,
			RwUser: &vpnctl.RwUser{ExpireAt: &exp},
			Addons: []vpnctl.Addon{{ID: 8, AddonName: "Доп. устройства", Price: 50, RwUser: &vpnctl.RwUser{ExpireAt: &exp}}}}}})
	f.Tariffs = []vpnctl.Tariff{
		{ID: 1, Kind: "base", Name: "Базовый", MonthlyPrice: 300, Active: true, Periods: []vpnctl.Period{{Months: 3, Price: 800}}},
		{ID: 2, Kind: "base", Name: "Про", MonthlyPrice: 500, Active: true},
		{ID: 9, Kind: "addon", AddonID: ptr(4), AddonName: "Устройства", Name: "+2", MonthlyPrice: 50, Active: true},
	}
	f.Quotes = map[string]vpnctl.ExtendQuote{"subscription:3": {CustomerID: 5, Amount: 300}, "addon:8": {CustomerID: 5, Amount: 50}}
	f.TariffQuotes = map[string]vpnctl.TariffQuote{"subscription:3:2": {Surcharge: 13.33}}
	sp := &spy{}
	s := &Service{DB: st.DB(), Chats: st, API: f, PayMode: config.PayMixed, Manual: true, Details: "Карта 2200",
		TTL: 48 * time.Hour, Notify: sp, Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s, f, sp, c
}

// submitted creates a renew order for 3 months and attaches a photo.
func submitted(t *testing.T, s *Service, c *vpnctl.Customer) *Order {
	t.Helper()
	ctx := context.Background()
	o, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if o, err = s.AttachReceipt(ctx, 77, ReceiptPhoto, "file1", "перевёл"); err != nil {
		t.Fatal(err)
	}
	return o
}
```

`service_test.go`:

```go
package orders

import (
	"context"
	"errors"
	"testing"
)

func TestQuote(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	cases := []struct {
		name  string
		r     Request
		price float64
		title string
	}{
		{"renew", Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3}, 900, "Продление «Дом» · 3 мес."},
		{"renew addon", Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1}, 50, "Продление «Доп. устройства» · 1 мес."},
		{"subscribe period", Request{Kind: KindSubscribe, TariffID: 1, Months: 3}, 800, "Новая подписка «Базовый» · 3 мес."},
		{"addon", Request{Kind: KindAddon, ItemID: 3, TariffID: 9, Months: 2}, 100, "Устройства «+2» к «Дом» · 2 мес."},
		{"change tariff", Request{Kind: KindChangeTariff, ItemKind: "subscription", ItemID: 3, TariffID: 2}, 13.33, "Смена тарифа «Дом» → «Про»"},
		{"topup", Request{Kind: KindTopup, Amount: 250}, 250, "Пополнение баланса"},
	}
	for _, tc := range cases {
		q, err := s.Quote(ctx, c, tc.r)
		if err != nil || q.Price != tc.price || q.Title != tc.title {
			t.Errorf("%s: %+v %v", tc.name, q, err)
		}
	}
	q, _ := s.Quote(ctx, c, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3})
	if q.FromBalance != 100 || q.ToPay != 800 || len(q.Methods) != 1 || q.Methods[0] != MethodManual {
		t.Errorf("split: %+v", q)
	}
	q, _ = s.Quote(ctx, c, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1})
	if q.ToPay != 0 || q.Methods[0] != MethodBalance {
		t.Errorf("covered by balance: %+v", q)
	}
	if q, _ := s.Quote(ctx, c, Request{Kind: KindTopup, Amount: 250}); q.FromBalance != 0 || q.ToPay != 250 {
		t.Errorf("topup never uses the balance: %+v", q)
	}
	for _, bad := range []Request{
		{Kind: KindRenew, ItemKind: "subscription", ItemID: 3},             // no term
		{Kind: KindRenew, ItemKind: "subscription", ItemID: 99, Months: 1}, // not theirs
		{Kind: KindAddon, ItemID: 99, TariffID: 9, Months: 1},              // not theirs
		{Kind: KindSubscribe, TariffID: 9, Months: 1},                      // addon tariff as a subscription
		{Kind: KindTopup, Amount: -5},
		{Kind: "nope"},
	} {
		if _, err := s.Quote(ctx, c, bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: want ErrBadRequest, got %v", bad, err)
		}
	}
}

func TestManualOffOnlyBalance(t *testing.T) {
	s, _, _, c := setup(t)
	s.Manual = false
	ctx := context.Background()
	if _, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "subscription", ItemID: 3, Months: 1}); !errors.Is(err, ErrManualOff) {
		t.Fatalf("want ErrManualOff, got %v", err)
	}
	if _, err := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1}); err != nil {
		t.Fatalf("balance order: %v", err)
	}
}

func TestReceiptFlow(t *testing.T) {
	s, _, sp, c := setup(t)
	o := submitted(t, s, c)
	if o.Status != StatusPendingReview || o.ReceiptFileID != "file1" || o.Comment != "перевёл" || o.AdminMsgID != 500+o.ID {
		t.Fatalf("%+v", o)
	}
	if len(sp.pay) != 1 || len(sp.posted) != 1 {
		t.Fatalf("pay screens %d, cards %d", len(sp.pay), len(sp.posted))
	}
	if _, ok, _ := s.Chats.GetChatState(context.Background(), 77); ok {
		t.Fatal("chat still waits for a receipt")
	}
	if _, err := s.AttachReceipt(context.Background(), 77, ReceiptPhoto, "f2", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second receipt without a waiting order: %v", err)
	}
}

func TestAttachReceiptRejectsUnknownKind(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	s.RequestReceipt(ctx, c.ID, o.ID)
	if _, err := s.AttachReceipt(ctx, 77, "sticker", "x", ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusAwaitingReceipt {
		t.Fatalf("status %s", got.Status)
	}
}

func TestCancelOtherCustomer(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	if err := s.Cancel(ctx, 6, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign cancel: %v", err)
	}
	if err := s.Cancel(ctx, 5, o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(ctx, 5, o.ID); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("cancel twice: %v", err)
	}
}

func TestExpireStale(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 100})
	s.RequestReceipt(ctx, c.ID, o.ID)
	fresh := submitted(t, s, c) // pending_review never expires
	s.Now = func() time.Time { return now.Add(49 * time.Hour) }
	if err := s.ExpireStale(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, o.ID); got.Status != StatusExpired {
		t.Fatalf("status %s", got.Status)
	}
	if got, _ := s.Get(ctx, fresh.ID); got.Status != StatusPendingReview {
		t.Fatalf("reviewed order expired: %s", got.Status)
	}
}
```

(add `"time"` import for `TestExpireStale`.)

- [ ] **Step 8: Run** `go test ./internal/orders/` → FAIL (undefined Service methods).
- [ ] **Step 9: `service.go`**

```go
package orders

// Quote prices r for c on the server; nothing the client sends but the
// choice itself is trusted.
func (s *Service) Quote(ctx context.Context, c *vpnctl.Customer, r Request) (Quote, error) {
	q, err := s.price(ctx, c, r)
	if err != nil {
		return Quote{}, err
	}
	if r.Kind != KindTopup {
		sp := billing.Split(q.Price, c.BalanceOwn, c.BalanceReferral, s.PayMode)
		q.FromBalance, q.ToPay = round2(sp.FromOwn+sp.FromReferral), sp.ToPay
	} else {
		q.ToPay = q.Price
	}
	switch {
	case q.ToPay == 0:
		q.Methods = []Method{MethodBalance}
	case s.Manual:
		q.Methods = []Method{MethodManual}
	default:
		q.Methods = []Method{}
	}
	return q, nil
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBadRequest}, a...)...)
}

func term(months, days int) string {
	var p []string
	if months > 0 {
		p = append(p, fmt.Sprintf("%d мес.", months))
	}
	if days > 0 {
		p = append(p, fmt.Sprintf("%d дн.", days))
	}
	return strings.Join(p, " ")
}

func (s *Service) price(ctx context.Context, c *vpnctl.Customer, r Request) (Quote, error) {
	if r.Kind != KindTopup && r.Kind != KindChangeTariff && (r.Months < 0 || r.Days < 0 || r.Months+r.Days == 0 || r.Months > 36) {
		return Quote{}, bad("term")
	}
	switch r.Kind {
	case KindTopup:
		if r.Amount <= 0 || r.Amount > 1_000_000 {
			return Quote{}, bad("amount")
		}
		return Quote{Title: "Пополнение баланса", Price: round2(r.Amount)}, nil
	case KindRenew:
		title, ok := itemTitle(c, r.ItemKind, r.ItemID)
		if !ok {
			return Quote{}, bad("item")
		}
		eq, err := s.API.QuoteExtend(ctx, r.ItemKind, r.ItemID, r.Months, r.Days)
		if err != nil {
			return Quote{}, apiErr(err)
		}
		return Quote{Title: fmt.Sprintf("Продление «%s» · %s", title, term(r.Months, r.Days)), Price: eq.Amount}, nil
	case KindSubscribe, KindAddon:
		t, err := s.tariff(ctx, r.TariffID)
		if err != nil {
			return Quote{}, err
		}
		price := TermCost(t.MonthlyPrice, t.Periods, r.Months, r.Days)
		if r.Kind == KindSubscribe {
			if t.Kind != "base" {
				return Quote{}, bad("tariff kind")
			}
			return Quote{Title: fmt.Sprintf("Новая подписка «%s» · %s", t.Name, term(r.Months, r.Days)), Price: price}, nil
		}
		sub, ok := itemTitle(c, "subscription", r.ItemID)
		if t.Kind != "addon" || !ok {
			return Quote{}, bad("addon")
		}
		return Quote{Title: fmt.Sprintf("%s «%s» к «%s» · %s", t.AddonName, t.Name, sub, term(r.Months, r.Days)), Price: price}, nil
	case KindChangeTariff:
		title, ok := itemTitle(c, r.ItemKind, r.ItemID)
		if !ok {
			return Quote{}, bad("item")
		}
		t, err := s.tariff(ctx, r.TariffID)
		if err != nil {
			return Quote{}, err
		}
		tq, err := s.API.QuoteTariff(ctx, r.ItemKind, r.ItemID, r.TariffID)
		if err != nil {
			return Quote{}, apiErr(err)
		}
		return Quote{Title: fmt.Sprintf("Смена тарифа «%s» → «%s»", title, t.Name), Price: math.Max(tq.Surcharge, 0)}, nil
	}
	return Quote{}, bad("kind %q", r.Kind)
}

// apiErr: a 4xx from the panel means the choice is impossible (unlimited
// subscription, inactive tariff…); anything else is passed on.
func apiErr(err error) error {
	var ae *vpnctl.APIError
	if errors.As(err, &ae) && ae.Status < 500 {
		return bad("%s", ae.Message)
	}
	return err
}

func (s *Service) tariff(ctx context.Context, id int) (*vpnctl.Tariff, error) {
	ts, err := s.API.ListTariffs(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.ID == id && t.Active {
			return &t, nil
		}
	}
	return nil, bad("tariff %d", id)
}

// itemTitle finds the customer's own subscription or add-on.
func itemTitle(c *vpnctl.Customer, kind string, id int) (string, bool) {
	for _, s := range c.Subscriptions {
		if kind == "subscription" && s.ID == id {
			return s.Title, true
		}
		for _, a := range s.Addons {
			if kind == "addon" && a.ID == id {
				return a.AddonName, true
			}
		}
	}
	return "", false
}

// Create stores an order with the quoted price. Fully covered by the
// balance → StatusNew (method balance, Fulfill via PayFromBalance);
// otherwise a manual order waiting for its receipt.
func (s *Service) Create(ctx context.Context, c *vpnctl.Customer, chatID int64, r Request) (*Order, error) {
	q, err := s.Quote(ctx, c, r)
	if err != nil {
		return nil, err
	}
	if len(q.Methods) == 0 {
		return nil, ErrManualOff
	}
	r.Amount = 0
	o := &Order{CustomerID: c.ID, ChatID: chatID, Request: r, Title: q.Title, Price: q.Price,
		FromBalance: q.FromBalance, ToPay: q.ToPay, Method: q.Methods[0], Status: StatusNew}
	if o.Method == MethodManual {
		o.Status = StatusAwaitingReceipt
	}
	return o, s.insert(ctx, o)
}

// RequestReceipt makes the customer's chat wait for this order's receipt
// and shows the amount with the payment details there.
func (s *Service) RequestReceipt(ctx context.Context, customerID, id int) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusAwaitingReceipt {
		return nil, ErrStateChanged
	}
	if err := s.Chats.SetChatState(ctx, o.ChatID, store.ChatState{Kind: "receipt", OrderID: o.ID}); err != nil {
		return nil, err
	}
	return o, s.Notify.PayScreen(ctx, *o, s.Details)
}

// AttachReceipt takes the receipt the chat was asked for and hands the
// order to the admins. ErrNotFound: the chat waits for nothing.
func (s *Service) AttachReceipt(ctx context.Context, chatID int64, kind ReceiptKind, fileID, comment string) (*Order, error) {
	st, ok, err := s.Chats.GetChatState(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !ok || st.Kind != "receipt" {
		return nil, ErrNotFound
	}
	if kind != ReceiptPhoto && kind != ReceiptDocument && kind != ReceiptText {
		return nil, bad("receipt kind %q", kind)
	}
	if len([]rune(comment)) > 500 {
		comment = string([]rune(comment)[:500])
	}
	err = s.move(ctx, st.OrderID, []Status{StatusAwaitingReceipt}, StatusPendingReview,
		map[string]any{"receipt_kind": kind, "receipt_file_id": fileID, "comment": comment})
	if cerr := s.Chats.ClearChatState(ctx, chatID); cerr != nil {
		s.Log.Error("orders: clear chat state", "err", cerr)
	}
	if err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, st.OrderID)
	if err != nil {
		return nil, err
	}
	msgID, err := s.Notify.PostCard(ctx, *o)
	if err != nil {
		s.Log.Error("orders: admin card", "order", o.ID, "err", err)
		return o, nil // the order is safe; admins see it in the next card update or the logs
	}
	o.AdminMsgID = msgID
	return o, s.set(ctx, o.ID, map[string]any{"admin_msg_id": msgID})
}

func (s *Service) Cancel(ctx context.Context, customerID, id int) error {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return err
	}
	if err := s.move(ctx, o.ID, []Status{StatusNew, StatusAwaitingReceipt}, StatusCancelled, nil); err != nil {
		return err
	}
	if st, ok, _ := s.Chats.GetChatState(ctx, o.ChatID); ok && st.OrderID == o.ID {
		return s.Chats.ClearChatState(ctx, o.ChatID)
	}
	return nil
}

// ExpireStale ends orders nobody sent a receipt for within TTL.
func (s *Service) ExpireStale(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE orders SET status = ?, updated_at = ? WHERE status IN (?, ?) AND created_at < ?`,
		StatusExpired, s.Now(), StatusNew, StatusAwaitingReceipt, s.Now().Add(-s.TTL))
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM chat_state WHERE kind = 'receipt' AND order_id IN (SELECT id FROM orders WHERE status = ?)`, StatusExpired)
	return err
}
```

Imports: `context`, `errors`, `fmt`, `math`, `strings`, `billing`, `store`, `vpnctl`.

- [ ] **Step 10: Run** `go test ./internal/orders/` → PASS.
- [ ] **Step 11: Commit** `git add internal/orders && git commit -m "feat(orders): order model, server-side pricing, receipts, cancel and expiry"`

---

### Task 6: `internal/orders` — Fulfill and admin decisions

**Files:** Create `internal/orders/fulfill.go`, `internal/orders/fulfill_test.go`.

**Produces:**

```go
type Decision string // DecideFull "full", DecideDebt "debt", DecideCredit "credit"

func (s *Service) Approve(ctx context.Context, id int, admin string, d Decision) (*Order, error)
func (s *Service) SetReceived(ctx context.Context, id int, amount float64, admin string) (*Order, error)
func (s *Service) Reject(ctx context.Context, id int, reason, admin string) (*Order, error)
func (s *Service) Retry(ctx context.Context, id int, admin string) (*Order, error)
func (s *Service) PayFromBalance(ctx context.Context, customerID, id int) (*Order, error)
```

Semantics (each ends with `Notify.UpdateCard` when `AdminMsgID != 0` and `Notify.Customer` on a final or failed status; notifier errors are logged, never returned):
- `Approve`: `pending_review → approving`. `DecideFull` requires `Paid() ≥ ToPay` (else revert and `ErrUnderpaid`); `DecideDebt` only for `KindRenew`; `DecideCredit` pays `Paid()` with no allocations → `credited` (for `KindTopup` → `approved`). Panel `ErrUnavailable` → revert to `pending_review`, return the error.
- `Retry`: `failed → approving`, re-runs only what is missing (pay is skipped when `PanelPaymentID != 0`).
- `SetReceived`: only in `pending_review`; amount > 0.
- `Reject`: `pending_review → rejected`.
- `PayFromBalance`: `new → approving` for method balance, fulfil with paid amount 0.

- [ ] **Step 1: Failing tests** `fulfill_test.go`

```go
package orders

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vpn-control-bot/internal/vpnctl"
)

func TestApproveRenew(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	got, err := s.Approve(context.Background(), o.ID, "@boss", DecideFull)
	if err != nil || got.Status != StatusApproved || got.Admin != "@boss" || got.ResultUntil == nil || got.PanelPaymentID != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	p := f.Payments[0]
	if p.Amount != 800 || p.Method != "manual" || p.Note != "bot order #1" || len(p.Allocations) != 1 ||
		p.Allocations[0] != (vpnctl.Allocation{Kind: "subscription", ID: 3, Months: 3, Amount: 900}) {
		t.Fatalf("payment %+v", p)
	}
	if len(sp.updated) != 1 || len(sp.sent) != 1 || sp.sent[0].Status != StatusApproved {
		t.Fatalf("notifications: cards %d, customer %d", len(sp.updated), len(sp.sent))
	}
}

func TestApproveTwiceRecordsOnePayment(t *testing.T) {
	s, f, _, c := setup(t)
	o := submitted(t, s, c)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = s.Approve(context.Background(), o.ID, "@a", DecideFull) }()
	}
	wg.Wait()
	if len(f.Payments) != 1 || (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("payments %d, errs %v", len(f.Payments), errs)
	}
	if !errors.Is(errs[0], ErrStateChanged) && !errors.Is(errs[1], ErrStateChanged) {
		t.Fatalf("loser should get ErrStateChanged: %v", errs)
	}
}

func TestRenewExtensionFailsThenRetry(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	f.FailExtend = "remnawave down"
	got, err := s.Approve(context.Background(), o.ID, "@a", DecideFull)
	if err != nil || got.Status != StatusFailed || got.Error != "remnawave down" || got.PanelPaymentID == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if sp.sent[len(sp.sent)-1].Status != StatusFailed {
		t.Fatal("customer not told about the failure")
	}
	f.FailExtend = ""
	got, err = s.Retry(context.Background(), o.ID, "@a")
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || len(f.Extends) != 1 ||
		f.Extends[0].In != (vpnctl.ExtendInput{Months: 3, Amount: 900, AllowDebt: true}) {
		t.Fatalf("retry %+v %v; payments %d extends %+v", got, err, len(f.Payments), f.Extends)
	}
}

func TestPanelDownKeepsOrderForReview(t *testing.T) {
	s, f, _, c := setup(t)
	o := submitted(t, s, c)
	f.Err = vpnctl.ErrUnavailable
	if _, err := s.Approve(context.Background(), o.ID, "@a", DecideFull); !errors.Is(err, vpnctl.ErrUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
	if got, _ := s.Get(context.Background(), o.ID); got.Status != StatusPendingReview {
		t.Fatalf("status %s", got.Status)
	}
}

func TestSubscribeAddonChangeTariff(t *testing.T) {
	ctx := context.Background()
	for _, r := range []Request{
		{Kind: KindSubscribe, TariffID: 1, Months: 3},
		{Kind: KindAddon, ItemID: 3, TariffID: 9, Months: 2},
		{Kind: KindChangeTariff, ItemKind: "subscription", ItemID: 3, TariffID: 2},
	} {
		s, f, _, c := setup(t)
		c.BalanceOwn = 0
		o, err := s.Create(ctx, c, 77, r)
		if err != nil {
			t.Fatal(err)
		}
		s.RequestReceipt(ctx, c.ID, o.ID)
		s.AttachReceipt(ctx, 77, ReceiptText, "", "оплатил")
		got, err := s.Approve(ctx, o.ID, "@a", DecideFull)
		if err != nil || got.Status != StatusApproved {
			t.Fatalf("%s: %+v %v", r.Kind, got, err)
		}
		if len(f.Payments) != 1 || f.Payments[0].Amount != o.ToPay || len(f.Payments[0].Allocations) != 0 {
			t.Fatalf("%s: payment %+v", r.Kind, f.Payments)
		}
		switch r.Kind {
		case KindSubscribe:
			p := f.Provisions[0]
			if p.CustomerID != 5 || p.TariffID != 1 || p.Months != 3 || p.Amount != 800 || p.AllowDebt || p.Username != "bot_c5_o1" {
				t.Fatalf("provision %+v", p)
			}
		case KindAddon:
			a := f.AddonCalls[0]
			if a.SubscriptionID != 3 || a.In.TariffID != 9 || a.In.Amount != 100 || a.In.AllowDebt {
				t.Fatalf("addon %+v", a)
			}
		case KindChangeTariff:
			if tc := f.TariffChanges[0]; tc.ID != 3 || tc.In.TariffID != 2 || tc.In.Surcharge != 13.33 {
				t.Fatalf("tariff %+v", tc)
			}
		}
	}
}

func TestSecondStepFailsRetryDoesNotPayAgain(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
	s.RequestReceipt(ctx, c.ID, o.ID)
	s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
	f.FailStep = &vpnctl.APIError{Status: 400, Message: "no free squad"}
	got, _ := s.Approve(ctx, o.ID, "@a", DecideFull)
	if got.Status != StatusFailed || got.Error == "" {
		t.Fatalf("%+v", got)
	}
	f.FailStep = nil
	got, err := s.Retry(ctx, o.ID, "@a")
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 1 || len(f.Provisions) != 1 {
		t.Fatalf("%+v %v payments %d", got, err, len(f.Payments))
	}
}

func TestCorrectedAmount(t *testing.T) {
	ctx := context.Background()
	t.Run("underpaid full is refused", func(t *testing.T) {
		s, _, _, c := setup(t)
		o := submitted(t, s, c)
		if _, err := s.SetReceived(ctx, o.ID, 500, "@a"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Approve(ctx, o.ID, "@a", DecideFull); !errors.Is(err, ErrUnderpaid) {
			t.Fatalf("want ErrUnderpaid, got %v", err)
		}
		if got, _ := s.Get(ctx, o.ID); got.Status != StatusPendingReview {
			t.Fatalf("status %s", got.Status)
		}
	})
	t.Run("debt", func(t *testing.T) {
		s, f, _, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 500, "@a")
		got, err := s.Approve(ctx, o.ID, "@a", DecideDebt)
		if err != nil || got.Status != StatusApproved || f.Payments[0].Amount != 500 || f.Payments[0].Allocations[0].Amount != 900 {
			t.Fatalf("%+v %v %+v", got, err, f.Payments)
		}
	})
	t.Run("credit", func(t *testing.T) {
		s, f, sp, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 500, "@a")
		got, err := s.Approve(ctx, o.ID, "@a", DecideCredit)
		if err != nil || got.Status != StatusCredited || f.Payments[0].Amount != 500 || len(f.Payments[0].Allocations) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
		if sp.sent[0].Status != StatusCredited || *sp.sent[0].Received != 500 {
			t.Fatalf("customer notice %+v", sp.sent[0])
		}
	})
	t.Run("overpaid stays on balance", func(t *testing.T) {
		s, f, _, c := setup(t)
		o := submitted(t, s, c)
		s.SetReceived(ctx, o.ID, 1000, "@a")
		if got, err := s.Approve(ctx, o.ID, "@a", DecideFull); err != nil || got.Status != StatusApproved || f.Payments[0].Amount != 1000 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("debt only for renewals", func(t *testing.T) {
		s, _, _, c := setup(t)
		o, _ := s.Create(ctx, c, 77, Request{Kind: KindSubscribe, TariffID: 2, Months: 1})
		s.RequestReceipt(ctx, c.ID, o.ID)
		s.AttachReceipt(ctx, 77, ReceiptPhoto, "f", "")
		if _, err := s.Approve(ctx, o.ID, "@a", DecideDebt); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("want ErrBadRequest, got %v", err)
		}
	})
	t.Run("bad amounts", func(t *testing.T) {
		s, _, _, c := setup(t)
		o := submitted(t, s, c)
		if _, err := s.SetReceived(ctx, o.ID, 0, "@a"); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("zero: %v", err)
		}
	})
}

func TestReject(t *testing.T) {
	s, f, sp, c := setup(t)
	o := submitted(t, s, c)
	got, err := s.Reject(context.Background(), o.ID, "нет перевода", "@a")
	if err != nil || got.Status != StatusRejected || got.RejectReason != "нет перевода" || len(f.Payments) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if sp.sent[0].RejectReason != "нет перевода" {
		t.Fatal("customer not told the reason")
	}
	if _, err := s.Approve(context.Background(), o.ID, "@a", DecideFull); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("approve after reject: %v", err)
	}
}

func TestPayFromBalance(t *testing.T) {
	s, f, _, c := setup(t)
	ctx := context.Background()
	o, _ := s.Create(ctx, c, 77, Request{Kind: KindRenew, ItemKind: "addon", ItemID: 8, Months: 1})
	if o.Method != MethodBalance || o.Status != StatusNew {
		t.Fatalf("%+v", o)
	}
	got, err := s.PayFromBalance(ctx, c.ID, o.ID)
	if err != nil || got.Status != StatusApproved || len(f.Payments) != 0 ||
		f.Extends[0] != (vpnctl.ExtendCall{Kind: "addon", ID: 8, In: vpnctl.ExtendInput{Months: 1, Amount: 50}}) {
		t.Fatalf("%+v %v %+v", got, err, f.Extends)
	}
	if _, err := s.PayFromBalance(ctx, 6, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/orders/` → FAIL.
- [ ] **Step 3: `fulfill.go`**

```go
package orders

type Decision string

const (
	DecideFull   Decision = "full"   // received covers it (or no correction)
	DecideDebt   Decision = "debt"   // renew anyway, the balance goes negative
	DecideCredit Decision = "credit" // only put the received money on the balance
)

func (s *Service) Approve(ctx context.Context, id int, admin string, d Decision) (*Order, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case d == DecideDebt && o.Kind != KindRenew:
		return nil, bad("debt only for renewals")
	case d == DecideFull && o.Paid() < o.ToPay:
		return nil, ErrUnderpaid
	}
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusApproving, map[string]any{"admin": admin}); err != nil {
		return nil, err
	}
	o.Status, o.Admin = StatusApproving, admin
	return s.run(ctx, o, d, StatusPendingReview)
}

func (s *Service) Retry(ctx context.Context, id int, admin string) (*Order, error) {
	if err := s.move(ctx, id, []Status{StatusFailed}, StatusApproving, map[string]any{"admin": admin, "error": ""}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.run(ctx, o, DecideDebt, StatusFailed) // the money is already in; finishing may use the balance
}

func (s *Service) PayFromBalance(ctx context.Context, customerID, id int) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	if o.Method != MethodBalance {
		return nil, bad("not a balance order")
	}
	if err := s.move(ctx, id, []Status{StatusNew}, StatusApproving, nil); err != nil {
		return nil, err
	}
	o.Status = StatusApproving
	return s.run(ctx, o, DecideFull, StatusNew)
}

func (s *Service) SetReceived(ctx context.Context, id int, amount float64, admin string) (*Order, error) {
	if amount <= 0 || amount > 1_000_000 {
		return nil, bad("amount")
	}
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusPendingReview,
		map[string]any{"received": round2(amount), "admin": admin}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.card(ctx, o)
	return o, nil
}

func (s *Service) Reject(ctx context.Context, id int, reason, admin string) (*Order, error) {
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusRejected,
		map[string]any{"reject_reason": strings.TrimSpace(reason), "admin": admin}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.card(ctx, o)
	s.customer(ctx, o)
	return o, nil
}

// run applies an approving order; on a panel outage before any money was
// recorded the order goes back to `back` so it can be decided again.
func (s *Service) run(ctx context.Context, o *Order, d Decision, back Status) (*Order, error) {
	status, until, ferr := s.fulfill(ctx, o, d)
	if ferr != nil && o.PanelPaymentID == 0 && errors.Is(ferr, vpnctl.ErrUnavailable) {
		if err := s.move(ctx, o.ID, []Status{StatusApproving}, back, nil); err != nil {
			s.Log.Error("orders: revert", "order", o.ID, "err", err)
		}
		return nil, ferr
	}
	set := map[string]any{"panel_payment_id": o.PanelPaymentID, "result_until": until}
	if ferr != nil {
		status = StatusFailed
		set["error"] = errText(ferr)
	}
	if err := s.move(ctx, o.ID, []Status{StatusApproving}, status, set); err != nil {
		return nil, err
	}
	got, err := s.Get(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	s.card(ctx, got)
	s.customer(ctx, got)
	return got, nil
}

func errText(err error) string {
	var ae *vpnctl.APIError
	if errors.As(err, &ae) && ae.Message != "" {
		return ae.Message
	}
	return err.Error()
}

// fulfill is the only place that moves money in the panel. It records the
// payment first (once: PanelPaymentID marks it done) and then applies the
// purchase; o.PanelPaymentID is updated in place.
func (s *Service) fulfill(ctx context.Context, o *Order, d Decision) (Status, *time.Time, error) {
	amount := o.Paid()
	if o.Method == MethodBalance {
		amount = 0
	}
	pay := func(alloc []vpnctl.Allocation) (*vpnctl.PaymentResult, error) {
		res, err := s.API.Pay(ctx, o.CustomerID, vpnctl.PaymentInput{Amount: amount, Method: string(o.Method), Note: o.Note(), Allocations: alloc})
		if err == nil {
			o.PanelPaymentID = res.PaymentID
		}
		return res, err
	}
	if d == DecideCredit || o.Kind == KindTopup {
		if _, err := pay(nil); err != nil {
			return "", nil, err
		}
		if o.Kind == KindTopup {
			return StatusApproved, nil, nil
		}
		return StatusCredited, nil, nil
	}

	if o.Kind == KindRenew {
		if o.PanelPaymentID == 0 && amount > 0 {
			res, err := pay([]vpnctl.Allocation{{Kind: o.ItemKind, ID: o.ItemID, Months: o.Months, Days: o.Days, Amount: o.Price}})
			if err != nil {
				return "", nil, err
			}
			if len(res.Extensions) == 0 || !res.Extensions[0].OK {
				msg := "extension skipped"
				if len(res.Extensions) > 0 {
					msg = res.Extensions[0].Error
				}
				return "", nil, errors.New(msg)
			}
			return StatusApproved, res.Extensions[0].To, nil
		}
		// Paid from the balance, or a retry after the payment was recorded.
		e, err := s.API.Extend(ctx, o.ItemKind, o.ItemID, vpnctl.ExtendInput{
			Months: o.Months, Days: o.Days, Amount: o.Price, AllowDebt: o.PanelPaymentID != 0,
		})
		if err != nil {
			return "", nil, err
		}
		return StatusApproved, e.To, nil
	}

	if o.PanelPaymentID == 0 && amount > 0 {
		if _, err := pay(nil); err != nil {
			return "", nil, err
		}
	}
	var err error
	switch o.Kind {
	case KindSubscribe:
		_, err = s.API.Provision(ctx, vpnctl.ProvisionInput{
			CustomerID: o.CustomerID, TariffID: o.TariffID, Username: fmt.Sprintf("bot_c%d_o%d", o.CustomerID, o.ID),
			Months: o.Months, Days: o.Days, Amount: o.Price,
		})
	case KindAddon:
		_, err = s.API.ConnectAddon(ctx, o.ItemID, vpnctl.ConnectAddonInput{TariffID: o.TariffID, Months: o.Months, Days: o.Days, Amount: o.Price})
	case KindChangeTariff:
		err = s.API.ChangeTariff(ctx, o.ItemKind, o.ItemID, vpnctl.ChangeTariffInput{TariffID: o.TariffID, Surcharge: o.Price})
	default:
		err = bad("kind %q", o.Kind)
	}
	if err != nil {
		return "", nil, err
	}
	return StatusApproved, nil, nil
}

func (s *Service) card(ctx context.Context, o *Order) {
	if o.AdminMsgID == 0 {
		return
	}
	if err := s.Notify.UpdateCard(ctx, *o); err != nil {
		s.Log.Warn("orders: update card", "order", o.ID, "err", err)
	}
}

func (s *Service) customer(ctx context.Context, o *Order) {
	if err := s.Notify.Customer(ctx, *o); err != nil {
		s.Log.Warn("orders: notify customer", "order", o.ID, "err", err)
	}
}
```

Note `run` for `Retry` passes `back = StatusFailed`. `result_until` with a nil `*time.Time` writes NULL (database/sql handles a typed nil pointer via `driver.DefaultParameterConverter`; if modernc rejects it, convert: `var u any; if until != nil { u = *until }`).

Imports: `context`, `errors`, `fmt`, `strings`, `time`, `vpnctl`.

- [ ] **Step 4: Run** `go test -race ./internal/orders/` → PASS.
- [ ] **Step 5: Remove the stub.** Delete `internal/billing/billing.go`; update `internal/billing` package doc to live in `split.go` (`// Package billing decides how a price is covered by the customer's balance.`). The build breaks in `telegram` and `main` until Task 7/9 — so in this commit replace the `Billing` dep minimally: in `internal/telegram/bot.go` remove the `Billing` field and make the `CbRenew` case answer `view.TxtSoon` (Task 7 replaces it); in `cmd/bot/main.go` drop `Billing: billing.NotImplemented{}` and the import. Run `go build ./... && go test ./...` → PASS.
- [ ] **Step 6: Commit** `git add -A internal cmd && git commit -m "feat(orders): fulfil orders in the panel; admin approve, correct, reject, retry"`

---

### Task 7: Bot — views, admin chat, receipts, renew flow, history

**Files:**
- Create `internal/telegram/view/orders.go`, `internal/telegram/view/orders_test.go`
- Create `internal/telegram/orders.go`, `internal/telegram/admin.go`, `internal/telegram/admin_test.go`
- Modify `internal/telegram/bot.go`, `route.go`, `private.go`, `private_test.go`, `view/screen.go`, `view/texts.go`, `view/screens.go`

**Interfaces — Consumes:** `orders.Service`, `orders.Order`, statuses, `orders.Decision`, `store.Store` prompts (Task 4).
**Produces:** `*telegram.Bot` implements `orders.Notifier`; `telegram.Deps` gains `Orders *orders.Service`, `Prompts PromptStore`, `AdminChatID int64`, `IsAdmin func(int64) bool`.

Callback data (all ≤ 64 bytes):

| Data | Who | Action |
|---|---|---|
| `r:<subID>` | customer | period picker for renewing a subscription |
| `rp:<subID>:<months>:<days>` | customer | create renew order → pay screen (or balance confirm) |
| `pr:<orderID>` | customer | "📎 Отправить квитанцию" → `RequestReceipt` |
| `pb:<orderID>` | customer | pay from balance |
| `pc:<orderID>` | customer | cancel |
| `m:pay` / `po:<page>` | customer | orders history |
| `oa:<id>` / `od:<id>` / `oc:<id>` | admin | approve full / debt / credit |
| `oe:<id>` | admin | ask corrected amount |
| `ox:<id>` / `on:<id>` | admin | ask reject reason / reject without reason |
| `oy:<id>` | admin | retry |

- [ ] **Step 1: View tests** `view/orders_test.go` (reuse `fixture()`, `links`, `now`, `buttons`, `has` helpers of the package tests)

```go
package view

import (
	"strings"
	"testing"

	"vpn-control-bot/internal/orders"
)

func order() orders.Order {
	return orders.Order{ID: 12, CustomerID: 5, ChatID: 77, Request: orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3},
		Title: "Продление «Дом» · 3 мес.", Price: 900, FromBalance: 100, ToPay: 800, Method: orders.MethodManual,
		Status: orders.StatusPendingReview, ReceiptKind: orders.ReceiptPhoto, Comment: "перевёл <сбер>", CreatedAt: now}
}

func TestPayScreen(t *testing.T) {
	s := PayScreen(order(), "Карта 2200 <b>")
	for _, want := range []string{"Заказ №12", "900", "100", "<b>800 ₽</b>", "Карта 2200 &lt;b&gt;"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("no %q in\n%s", want, s.Text)
		}
	}
	if b := buttons(s); !has(b, CbOrderCancel+"12") {
		t.Errorf("buttons %v", b)
	}
}

func TestAdminCard(t *testing.T) {
	o := order()
	c := AdminCard(o, AdminCustomer{Name: "Ann", Username: "ann", TelegramID: 77})
	if !strings.Contains(c.Text, "перевёл &lt;сбер&gt;") || !strings.Contains(c.Text, "@ann") || len(c.Text) > 1024 {
		t.Fatalf("card:\n%s", c.Text)
	}
	if b := buttons(c); !has(b, CbAdminApprove+"12") || !has(b, CbAdminEdit+"12") || !has(b, CbAdminReject+"12") {
		t.Fatalf("buttons %v", b)
	}
	r := 500.0
	o.Received = &r
	b := buttons(AdminCard(o, AdminCustomer{Name: "Ann"}))
	if has(b, CbAdminApprove+"12") || !has(b, CbAdminDebt+"12") || !has(b, CbAdminCredit+"12") {
		t.Fatalf("underpaid buttons %v", b)
	}
	o.Kind = orders.KindSubscribe
	if has(buttons(AdminCard(o, AdminCustomer{})), CbAdminDebt+"12") {
		t.Fatal("debt offered for a new subscription")
	}
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	done := AdminCard(o, AdminCustomer{})
	if done.Keyboard != nil || !strings.Contains(done.Text, "@boss") || !strings.Contains(done.Text, "800 → 500") {
		t.Fatalf("decided card:\n%s %v", done.Text, done.Keyboard)
	}
	o.Status, o.Error = orders.StatusFailed, "no squad"
	if !has(buttons(AdminCard(o, AdminCustomer{})), CbAdminRetry+"12") {
		t.Fatal("no retry on a failed order")
	}
}

func TestOrderNotice(t *testing.T) {
	o := order()
	for st, want := range map[orders.Status]string{
		orders.StatusApproved: "подтверждена", orders.StatusRejected: "отклонён", orders.StatusFailed: "администратор разберётся",
		orders.StatusCredited: "зачислено на баланс",
	} {
		o.Status = st
		r := 500.0
		if st == orders.StatusCredited {
			o.Received = &r
		}
		if s := OrderNotice(o); !strings.Contains(s.Text, want) {
			t.Errorf("%s: %s", st, s.Text)
		}
	}
}

func TestOrdersHistory(t *testing.T) {
	list := []orders.Order{order(), order()}
	list[1].ID, list[1].Status = 11, orders.StatusApproved
	s := Orders(list, 0, 2)
	if !strings.Contains(s.Text, "⏳") || !strings.Contains(s.Text, "✅") || !strings.Contains(s.Text, "№11") {
		t.Fatal(s.Text)
	}
	if e := Orders(nil, 0, 0); !strings.Contains(e.Text, "пока нет") {
		t.Fatal(e.Text)
	}
}

func TestRenewPeriods(t *testing.T) {
	s := RenewPeriods(3, "Дом", []RenewOption{{Months: 1, Price: 300}, {Months: 3, Price: 800}})
	if b := buttons(s); !has(b, CbRenewPick+"3:1:0") || !has(b, CbRenewPick+"3:3:0") {
		t.Fatalf("%v", b)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/telegram/view/` → FAIL.
- [ ] **Step 3: Implement view.** Add to `screen.go` constants:

```go
	CbOrders     = "m:pay"
	CbOrdersPage = "po:" // + page number, from 0
	CbRenewPick  = "rp:" // + subID:months:days
	CbOrderPay   = "pr:" // + order id: send the receipt
	CbOrderBal   = "pb:" // + order id: pay from the balance
	CbOrderCancel = "pc:" // + order id

	CbAdminApprove = "oa:" // + order id
	CbAdminDebt    = "od:"
	CbAdminCredit  = "oc:"
	CbAdminEdit    = "oe:"
	CbAdminReject  = "ox:"
	CbAdminNoReason = "on:"
	CbAdminRetry   = "oy:"
```

Texts in `texts.go`:

```go
	BtnOrders     = "🧾 Платежи"
	BtnSendReceipt = "📎 Отправить квитанцию"
	BtnPayBalance = "💰 Оплатить с баланса"
	BtnCancel     = "Отмена"
	BtnApprove    = "✅ Подтвердить"
	BtnDebt       = "✅ Продлить в долг"
	BtnCredit     = "💰 Только на баланс"
	BtnEditAmount = "✏️ Изменить сумму"
	BtnReject     = "❌ Отклонить"
	BtnNoReason   = "Без причины"
	BtnRetry      = "🔁 Повторить"

	TxtReceiptHint   = "Пришлите фото или PDF квитанции (можно с подписью) — или нажмите «Отмена»."
	TxtReceiptSent   = "📨 Квитанция отправлена. Как только администратор проверит оплату, я напишу."
	TxtOrderDone     = "Этот заказ уже обработан."
	TxtNoRights      = "Нет прав"
	TxtAskAmount     = "Сколько фактически получено по заказу №%d? Ответьте числом на это сообщение."
	TxtAskReason     = "Причина отклонения заказа №%d? Ответьте на это сообщение или нажмите «Без причины»."
	TxtBadAmount     = "Не понял сумму — ответьте числом, например 700 или 699,50."
	TxtPanelDown     = "⚠️ Панель недоступна, попробуйте позже."
	TxtPickPeriod    = "💳 <b>Продление «%s»</b>\n\nВыберите срок:"
	TxtPayManualOff  = "Оплата переводом не настроена — пополнить баланс можно у администратора."
```

`view/orders.go`:

```go
package view

import (
	"fmt"
	"strconv"
	"strings"

	"vpn-control-bot/internal/orders"
)

var statusIcon = map[orders.Status]string{
	orders.StatusAwaitingReceipt: "📎", orders.StatusPendingReview: "⏳", orders.StatusApproving: "⏳",
	orders.StatusApproved: "✅", orders.StatusCredited: "💰", orders.StatusFailed: "⚠️",
	orders.StatusRejected: "❌", orders.StatusExpired: "⌛", orders.StatusCancelled: "✖️",
}

var statusText = map[orders.Status]string{
	orders.StatusAwaitingReceipt: "ждёт квитанцию", orders.StatusPendingReview: "на проверке", orders.StatusApproving: "проводится",
	orders.StatusApproved: "оплачен", orders.StatusCredited: "зачислен на баланс", orders.StatusFailed: "ошибка, разбираемся",
	orders.StatusRejected: "отклонён", orders.StatusExpired: "истёк", orders.StatusCancelled: "отменён",
}

func id(prefix string, n int) string { return prefix + strconv.Itoa(n) }

// summary: what is bought and how it is paid.
func summary(o orders.Order) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <b>Заказ №%d</b>\n%s\n\n", o.ID, Esc(o.Title))
	fmt.Fprintf(&b, "Цена: %s\n", Rub(o.Price))
	if o.FromBalance > 0 {
		fmt.Fprintf(&b, "С баланса: −%s\n", Rub(o.FromBalance))
	}
	fmt.Fprintf(&b, "К оплате: <b>%s</b>", Rub(o.ToPay))
	return b.String()
}

func PayScreen(o orders.Order, details string) Screen {
	text := summary(o) + "\n\n💳 <b>Как оплатить</b>\n" + Esc(details) + "\n\n" + TxtReceiptHint
	return Screen{Text: text, Keyboard: kb([]btn{cb(BtnCancel, id(CbOrderCancel, o.ID))})}
}

// Confirm is the pay screen before the receipt is asked for (bot renew flow).
func Confirm(o orders.Order) Screen {
	if o.Method == orders.MethodBalance {
		return Screen{Text: summary(o) + "\n\nВся сумма спишется с баланса.", Keyboard: kb(
			[]btn{cb(BtnPayBalance, id(CbOrderBal, o.ID))}, []btn{cb(BtnCancel, id(CbOrderCancel, o.ID))})}
	}
	return Screen{Text: summary(o), Keyboard: kb(
		[]btn{cb(BtnSendReceipt, id(CbOrderPay, o.ID))}, []btn{cb(BtnCancel, id(CbOrderCancel, o.ID))})}
}

type RenewOption struct {
	Months, Days int
	Price        float64
}

func RenewPeriods(subID int, title string, opts []RenewOption) Screen {
	var rows [][]btn
	for _, o := range opts {
		label := fmt.Sprintf("%s — %s", termText(o.Months, o.Days), Rub(o.Price))
		rows = append(rows, []btn{cb(label, fmt.Sprintf("%s%d:%d:%d", CbRenewPick, subID, o.Months, o.Days))})
	}
	rows = append(rows, back(id(CbSub, subID)))
	return Screen{Text: fmt.Sprintf(TxtPickPeriod, Esc(clip(title, maxTitle))), Keyboard: kb(rows...)}
}

func termText(months, days int) string {
	var p []string
	if months > 0 {
		p = append(p, fmt.Sprintf("%d мес.", months))
	}
	if days > 0 {
		p = append(p, fmt.Sprintf("%d дн.", days))
	}
	return strings.Join(p, " ")
}

// AdminCustomer is who sent the receipt, as the admins see them.
type AdminCustomer struct {
	Name       string
	Username   string
	TelegramID int64
}

// AdminCard is the caption of the receipt in the admin chat (≤1024 chars).
func AdminCard(o orders.Order, c AdminCustomer) Screen {
	var b strings.Builder
	b.WriteString(summary(o))
	who := Esc(clip(c.Name, 40))
	if c.Username != "" {
		who += " @" + Esc(c.Username)
	}
	fmt.Fprintf(&b, "\n\n👤 %s · клиент #%d", who, o.CustomerID)
	if o.Comment != "" && o.ReceiptKind != orders.ReceiptText {
		fmt.Fprintf(&b, "\n💬 %s", Esc(clip(o.Comment, 300)))
	}
	if o.Received != nil {
		fmt.Fprintf(&b, "\n✏️ Сумма исправлена %s: %s → %s", Esc(o.Admin), plain(o.ToPay), Rub(*o.Received))
	}
	switch o.Status {
	case orders.StatusPendingReview:
		return Screen{Text: b.String(), Keyboard: adminButtons(o)}
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
		return Screen{Text: b.String(), Keyboard: kb([]btn{cb(BtnRetry, id(CbAdminRetry, o.ID))})}
	default:
		fmt.Fprintf(&b, "\n\n%s %s", statusIcon[o.Status], statusText[o.Status])
	}
	return Screen{Text: b.String()}
}

// plain is an amount without the currency (for "700 → 500 ₽").
func plain(v float64) string { return strings.TrimSuffix(Rub(v), " ₽") }

func adminButtons(o orders.Order) *models.InlineKeyboardMarkup {
	first := []btn{cb(BtnApprove, id(CbAdminApprove, o.ID))}
	if o.Paid() < o.ToPay {
		first = nil
		if o.Kind == orders.KindRenew {
			first = append(first, cb(BtnDebt, id(CbAdminDebt, o.ID)))
		}
		first = append(first, cb(BtnCredit, id(CbAdminCredit, o.ID)))
	}
	return kb(first, []btn{cb(BtnEditAmount, id(CbAdminEdit, o.ID)), cb(BtnReject, id(CbAdminReject, o.ID))})
}

// OrderNotice tells the customer what was decided.
func OrderNotice(o orders.Order) Screen {
	var text string
	switch o.Status {
	case orders.StatusApproved:
		text = fmt.Sprintf("✅ Оплата №%d подтверждена: %s", o.ID, Esc(o.Title))
		if o.ResultUntil != nil {
			text += fmt.Sprintf(", действует до <b>%s</b>", Date(o.ResultUntil))
		}
	case orders.StatusCredited:
		text = fmt.Sprintf("💰 По заказу №%d получено %s из %s — зачислено на баланс. Доплатите разницу, чтобы завершить покупку.",
			o.ID, Rub(o.Paid()), Rub(o.ToPay))
	case orders.StatusRejected:
		text = fmt.Sprintf("❌ Платёж №%d отклонён", o.ID)
		if o.RejectReason != "" {
			text += ": " + Esc(o.RejectReason)
		}
	case orders.StatusFailed:
		text = fmt.Sprintf("⚠️ Оплата №%d принята, но применить её не удалось — деньги на балансе, администратор разберётся.", o.ID)
	default:
		text = fmt.Sprintf("%s Заказ №%d: %s", statusIcon[o.Status], o.ID, statusText[o.Status])
	}
	if o.Received != nil && o.Status == orders.StatusApproved && *o.Received != o.ToPay {
		text += fmt.Sprintf("\nПолучено: %s", Rub(*o.Received))
	}
	return Screen{Text: text, Keyboard: kb([]btn{cb(BtnOrders, CbOrders)})}
}

const ordersPerPage = 10

func Orders(list []orders.Order, n, total int) Screen {
	if total == 0 {
		return Screen{Text: "🧾 <b>Платежи</b>\n\nПлатежей пока нет.", Keyboard: kb(back(CbHome))}
	}
	cur, pages, _, _ := page(n, total, ordersPerPage)
	var b strings.Builder
	b.WriteString("🧾 <b>Платежи</b>\n")
	for _, o := range list {
		fmt.Fprintf(&b, "\n%s №%d · %s · %s\n%s — %s", statusIcon[o.Status], o.ID, Date(&o.CreatedAt), Rub(o.Paid()),
			Esc(clip(o.Title, maxTitle)), statusText[o.Status])
	}
	rows := pager(cur, pages, CbOrdersPage)
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(CbHome))...)}
}
```

`page`/`pager` come from `paging.go`: check their signatures (`page(n, total, per) (cur, pages, from, to int)` as used in `Subscriptions`); if there is no `pager` returning `[][]btn`, use the helper `Subscriptions` uses to build its ‹/› row with prefix `CbOrdersPage`. `Rub`, `Date`, `Esc`, `clip`, `maxTitle` exist in `format.go`/`screens.go`. Add `models` import for `adminButtons`.

Home keyboard (`screens.go` `Home`): add `cb(BtnOrders, CbOrders)` as a new last row `[]btn{cb(BtnOrders, CbOrders)}`; update any golden file the existing tests compare (`go test ./internal/telegram/view -update` if the package has that flag; otherwise edit the golden in `testdata/`).
- [ ] **Step 4: Run** `go test ./internal/telegram/view/` → PASS.
- [ ] **Step 5: Bot tests** `internal/telegram/admin_test.go`

```go
package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]float64{"700": 700, "699,50": 699.5, " 1 200 ₽ ": 1200, "500.5р": 500.5} {
		if got, ok := parseAmount(in); !ok || got != want {
			t.Errorf("%q = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", "0", "-5", "1e9"} {
		if _, ok := parseAmount(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestAdminGate(t *testing.T) {
	const admin = int64(-100)
	group := models.Chat{ID: admin, Type: models.ChatTypeSupergroup}
	cases := []struct {
		name string
		u    *models.Update
		want bool
	}{
		{"private message", &models.Update{Message: &models.Message{From: &models.User{ID: 1}, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}}}, true},
		{"admin button", &models.Update{CallbackQuery: &models.CallbackQuery{Data: "oa:1", Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: group}}}}, true},
		{"customer button in admin chat", &models.Update{CallbackQuery: &models.CallbackQuery{Data: "m:home", Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: group}}}}, false},
		{"reply in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, ReplyToMessage: &models.Message{ID: 3}}}, true},
		{"/start in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, Text: "/start"}}, false},
		{"other group", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: models.Chat{ID: -7, Type: models.ChatTypeGroup}, ReplyToMessage: &models.Message{}}}, false},
	}
	for _, c := range cases {
		if got := allowed(c.u, admin); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
```

(Non-admin *members* pressing admin buttons pass the gate; the handler answers "Нет прав" — the handler checks `IsAdmin(q.From.ID)` first thing, see Step 7.)
- [ ] **Step 6: Run** `go test ./internal/telegram/` → FAIL.
- [ ] **Step 7: Implement.**

`private.go` — replace `privateOnly`/`privateMiddleware` with:

```go
// allowed lets through private chats with a known sender and, in the admin
// chat, only admin-card buttons and replies to the bot's prompts: in any
// group the customer screens would post one member's data for everyone.
func allowed(u *models.Update, adminChat int64) bool {
	switch {
	case u.Message != nil:
		m := u.Message
		if m.From == nil {
			return false
		}
		if m.Chat.Type == models.ChatTypePrivate {
			return true
		}
		return adminChat != 0 && m.Chat.ID == adminChat && m.ReplyToMessage != nil
	case u.CallbackQuery != nil:
		m := u.CallbackQuery.Message.Message
		if m == nil {
			return false
		}
		if m.Chat.Type == models.ChatTypePrivate {
			return true
		}
		return adminChat != 0 && m.Chat.ID == adminChat && strings.HasPrefix(u.CallbackQuery.Data, "o")
	}
	return false
}

func gate(adminChat int64) tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			if allowed(u, adminChat) {
				next(ctx, b, u)
			}
		}
	}
}
```

Update `private_test.go` to call `allowed(u, 0)` where it called `privateOnly(u)`. Also: callback prefix `"o"` must not collide with customer prefixes — none of `m: s: r: rp: ps: pt: po: pr: pb: pc:` start with `o`. A private admin chat (`ADMIN_CHAT_ID` = admin's own id) is private, so it passes and is routed by prefix.

`bot.go`:
- `Deps` += `Orders *orders.Service`, `Prompts PromptStore`, `AdminChatID int64`, `IsAdmin func(int64) bool`, `ManualDetails string` (unused by bot when Orders handles details — omit if not needed).
- `type PromptStore interface { SavePrompt(ctx context.Context, chatID int64, msgID int, p store.Prompt) error; TakePrompt(ctx context.Context, chatID int64, msgID int) (store.Prompt, bool, error) }`
- `New`: `tgbot.WithMiddlewares(gate(d.AdminChatID))`; register prefixes: customer `[]string{"m:", view.CbSub, view.CbSubsPage, view.CbTariffsPage, view.CbRenew, view.CbOrdersPage}` → `onCallback` (note `CbRenewPick` "rp:" is matched by `"r:"`? No — "rp:" does not start with "r:"; register `view.CbRenewPick`, `view.CbOrderPay`, `view.CbOrderBal`, `view.CbOrderCancel` → `onOrderCallback`); admin `"o"` prefix → `onAdmin`.
- `onCallback` `CbRenew` case → `b.renewPeriods(ctx, chatID, msgID, r.Customer, rt.id)`; new case `view.CbOrders, view.CbOrdersPage` → `b.showOrders(ctx, chatID, msgID, r.Customer.ID, rt.id)`.
- `onText`: before the awaiting-link check:

```go
	if m.Chat.Type != models.ChatTypePrivate || (b.AdminChatID != 0 && m.Chat.ID == b.AdminChatID && m.ReplyToMessage != nil) {
		b.onAdminReply(ctx, m)
		return
	}
	if handled := b.onReceipt(ctx, m); handled {
		return
	}
```

`route.go` — `parseRoute`: add `view.CbOrders` to the plain list and `view.CbOrdersPage: 0` to the prefix map. Add:

```go
// parseIDs parses "prefix<a>:<b>:<c>" into ints.
func parseIDs(data, prefix string, n int) ([]int, bool) {
	rest, ok := strings.CutPrefix(data, prefix)
	if !ok {
		return nil, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != n {
		return nil, false
	}
	out := make([]int, n)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}
```

`orders.go` (package telegram) — Notifier + customer flow:

```go
package telegram

var _ orders.Notifier = (*Bot)(nil)

func (b *Bot) PayScreen(ctx context.Context, o orders.Order, details string) error {
	s := view.PayScreen(o, details)
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	return err
}

func (b *Bot) adminCustomer(ctx context.Context, o orders.Order) view.AdminCustomer {
	ac := view.AdminCustomer{TelegramID: o.ChatID}
	if c, err := b.API.GetCustomer(ctx, o.CustomerID); err == nil {
		ac.Name = c.Name
	}
	if ch, err := b.tg.GetChat(ctx, &tgbot.GetChatParams{ChatID: o.ChatID}); err == nil {
		ac.Username = ch.Username
	}
	return ac
}

// PostCard re-sends the receipt into the admin chat with the order card.
func (b *Bot) PostCard(ctx context.Context, o orders.Order) (int, error) {
	s := view.AdminCard(o, b.adminCustomer(ctx, o))
	var markup models.ReplyMarkup
	if s.Keyboard != nil {
		markup = s.Keyboard
	}
	var m *models.Message
	var err error
	switch o.ReceiptKind {
	case orders.ReceiptPhoto:
		m, err = b.tg.SendPhoto(ctx, &tgbot.SendPhotoParams{ChatID: b.AdminChatID, Photo: &models.InputFileString{Data: o.ReceiptFileID},
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	case orders.ReceiptDocument:
		m, err = b.tg.SendDocument(ctx, &tgbot.SendDocumentParams{ChatID: b.AdminChatID, Document: &models.InputFileString{Data: o.ReceiptFileID},
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	default:
		m, err = b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: b.AdminChatID,
			Text: s.Text + "\n\n📝 " + view.Esc(o.Comment), ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	}
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

func (b *Bot) UpdateCard(ctx context.Context, o orders.Order) error {
	s := view.AdminCard(o, b.adminCustomer(ctx, o))
	markup := s.Keyboard
	if markup == nil {
		markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	}
	var err error
	if o.ReceiptKind == orders.ReceiptText {
		_, err = b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: b.AdminChatID, MessageID: o.AdminMsgID,
			Text: s.Text + "\n\n📝 " + view.Esc(o.Comment), ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	} else {
		_, err = b.tg.EditMessageCaption(ctx, &tgbot.EditMessageCaptionParams{ChatID: b.AdminChatID, MessageID: o.AdminMsgID,
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	}
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

func (b *Bot) Customer(ctx context.Context, o orders.Order) error {
	s := view.OrderNotice(o)
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	return err
}

// onReceipt takes a receipt when the chat waits for one; false: it doesn't.
func (b *Bot) onReceipt(ctx context.Context, m *models.Message) bool {
	if b.Orders == nil {
		return false
	}
	kind, fileID, comment := orders.ReceiptKind(""), "", m.Caption
	switch {
	case len(m.Photo) > 0:
		kind, fileID = orders.ReceiptPhoto, m.Photo[len(m.Photo)-1].FileID
	case m.Document != nil:
		kind, fileID = orders.ReceiptDocument, m.Document.FileID
	case m.Text != "" && !strings.HasPrefix(m.Text, "/"):
		kind, comment = orders.ReceiptText, m.Text
	default:
		kind = "other"
	}
	_, err := b.Orders.AttachReceipt(ctx, m.Chat.ID, kind, fileID, comment)
	switch {
	case errors.Is(err, orders.ErrNotFound):
		return false
	case errors.Is(err, orders.ErrBadRequest):
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtReceiptHint))
	case errors.Is(err, orders.ErrStateChanged):
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtOrderDone))
	case err != nil:
		b.Log.Error("receipt", "err", err)
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtDown))
	default:
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtReceiptSent))
	}
	return true
}

// renewPeriods offers the subscription tariff's periods (or 1/3/6/12 months).
func (b *Bot) renewPeriods(ctx context.Context, chatID int64, msgID int, c *vpnctl.Customer, subID int) {
	var sub *vpnctl.Subscription
	for i := range c.Subscriptions {
		if c.Subscriptions[i].ID == subID {
			sub = &c.Subscriptions[i]
		}
	}
	if sub == nil {
		return
	}
	terms := []view.RenewOption{{Months: 1}, {Months: 3}, {Months: 6}, {Months: 12}}
	if ts, err := b.API.ListTariffs(ctx); err == nil && sub.TariffID != nil {
		for _, t := range ts {
			if t.ID == *sub.TariffID && len(t.Periods) > 0 {
				terms = []view.RenewOption{{Months: 1}}
				for _, p := range t.Periods {
					terms = append(terms, view.RenewOption{Months: p.Months, Days: p.Days})
				}
			}
		}
	}
	var opts []view.RenewOption
	for _, t := range terms {
		q, err := b.Orders.Quote(ctx, c, orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: subID, Months: t.Months, Days: t.Days})
		if err != nil {
			continue
		}
		t.Price = q.Price
		opts = append(opts, t)
	}
	if len(opts) == 0 {
		b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
		return
	}
	b.show(ctx, chatID, msgID, view.RenewPeriods(subID, sub.Title, opts))
}

func (b *Bot) showOrders(ctx context.Context, chatID int64, msgID, customerID, page int) {
	list, total, err := b.Orders.List(ctx, customerID, 10, page*10)
	if err != nil {
		b.Log.Error("orders list", "err", err)
		b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
		return
	}
	b.show(ctx, chatID, msgID, view.Orders(list, page, total))
}

// onOrderCallback: rp: pr: pb: pc: from the customer.
func (b *Bot) onOrderCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	c := r.Customer
	if v, ok := parseIDs(q.Data, view.CbRenewPick, 3); ok {
		o, err := b.Orders.Create(ctx, c, chatID, orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: v[0], Months: v[1], Days: v[2]})
		switch {
		case errors.Is(err, orders.ErrManualOff):
			answer.Text, answer.ShowAlert = view.TxtPayManualOff, true
		case err != nil:
			b.Log.Error("create order", "err", err)
			answer.Text, answer.ShowAlert = view.TxtDown, true
		default:
			b.show(ctx, chatID, msgID, view.Confirm(*o))
		}
		return
	}
	for prefix, act := range map[string]func(int) error{
		view.CbOrderPay: func(id int) error { _, err := b.Orders.RequestReceipt(ctx, c.ID, id); return err },
		view.CbOrderBal: func(id int) error { _, err := b.Orders.PayFromBalance(ctx, c.ID, id); return err },
		view.CbOrderCancel: func(id int) error {
			if err := b.Orders.Cancel(ctx, c.ID, id); err != nil {
				return err
			}
			b.show(ctx, chatID, msgID, view.Plain("Заказ отменён."))
			return nil
		},
	} {
		v, ok := parseIDs(q.Data, prefix, 1)
		if !ok {
			continue
		}
		err := act(v[0])
		switch {
		case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound):
			answer.Text = view.TxtOrderDone
		case err != nil:
			b.Log.Error("order action", "data", q.Data, "err", err)
			answer.Text, answer.ShowAlert = view.TxtDown, true
		}
		return
	}
}
```

(`PayFromBalance` and `RequestReceipt` notify through the Notifier — the decision notice / pay screen arrive as new messages.)

`admin.go`:

```go
package telegram

func parseAmount(s string) (float64, bool) {
	s = strings.NewReplacer(" ", "", " ", "", "₽", "", "р", "", "руб", "", ",", ".").Replace(strings.ToLower(strings.TrimSpace(s)))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || v > 1_000_000 || math.IsNaN(v) {
		return 0, false
	}
	return math.Round(v*100) / 100, true
}

func adminName(u *models.User) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

func (b *Bot) onAdmin(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()
	if b.Orders == nil || !b.IsAdmin(q.From.ID) {
		answer.Text, answer.ShowAlert = view.TxtNoRights, true
		return
	}
	chatID := q.Message.Message.Chat.ID
	who := adminName(&q.From)
	for prefix, d := range map[string]orders.Decision{view.CbAdminApprove: orders.DecideFull, view.CbAdminDebt: orders.DecideDebt, view.CbAdminCredit: orders.DecideCredit} {
		if v, ok := parseIDs(q.Data, prefix, 1); ok {
			_, err := b.Orders.Approve(ctx, v[0], who, d)
			b.adminResult(answer, err)
			return
		}
	}
	if v, ok := parseIDs(q.Data, view.CbAdminRetry, 1); ok {
		_, err := b.Orders.Retry(ctx, v[0], who)
		b.adminResult(answer, err)
		return
	}
	if v, ok := parseIDs(q.Data, view.CbAdminNoReason, 1); ok {
		_, err := b.Orders.Reject(ctx, v[0], "", who)
		b.adminResult(answer, err)
		return
	}
	if v, ok := parseIDs(q.Data, view.CbAdminEdit, 1); ok {
		b.prompt(ctx, chatID, q.Message.Message.ID, fmt.Sprintf(view.TxtAskAmount, v[0]), store.Prompt{Kind: "amount", OrderID: v[0]}, nil)
		return
	}
	if v, ok := parseIDs(q.Data, view.CbAdminReject, 1); ok {
		noReason := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: view.BtnNoReason, CallbackData: view.CbAdminNoReason + strconv.Itoa(v[0])}}}}
		b.prompt(ctx, chatID, q.Message.Message.ID, fmt.Sprintf(view.TxtAskReason, v[0]), store.Prompt{Kind: "reject", OrderID: v[0]}, noReason)
	}
}

func (b *Bot) adminResult(answer *tgbot.AnswerCallbackQueryParams, err error) {
	switch {
	case err == nil:
	case errors.Is(err, orders.ErrStateChanged):
		answer.Text = view.TxtOrderDone
	case errors.Is(err, vpnctl.ErrUnavailable):
		answer.Text, answer.ShowAlert = view.TxtPanelDown, true
	default:
		b.Log.Error("admin action", "err", err)
		answer.Text, answer.ShowAlert = err.Error(), true
	}
}

// prompt asks the admins a question answered by replying to it. A forced
// reply only works for a reply markup, so the "no reason" button goes in a
// second message when given.
func (b *Bot) prompt(ctx context.Context, chatID int64, cardID int, text string, p store.Prompt, extra *models.InlineKeyboardMarkup) {
	m, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text,
		ReplyParameters: &models.ReplyParameters{MessageID: cardID},
		ReplyMarkup:     &models.ForceReply{ForceReply: true, Selective: true}})
	if err != nil {
		b.Log.Error("admin prompt", "err", err)
		return
	}
	if err := b.Prompts.SavePrompt(ctx, chatID, m.ID, p); err != nil {
		b.Log.Error("admin prompt", "err", err)
	}
	if extra != nil {
		b.tg.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{ChatID: chatID, MessageID: m.ID, ReplyMarkup: extra})
	}
}

// onAdminReply handles a reply to a prompt (amount or reject reason).
func (b *Bot) onAdminReply(ctx context.Context, m *models.Message) {
	if b.Orders == nil || m.ReplyToMessage == nil || !b.IsAdmin(m.From.ID) {
		return
	}
	p, ok, err := b.Prompts.TakePrompt(ctx, m.Chat.ID, m.ReplyToMessage.ID)
	if err != nil || !ok {
		return
	}
	who := adminName(m.From)
	reply := func(text string) {
		b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: text, ReplyParameters: &models.ReplyParameters{MessageID: m.ID}})
	}
	switch p.Kind {
	case "amount":
		v, ok := parseAmount(m.Text)
		if !ok {
			b.Prompts.SavePrompt(ctx, m.Chat.ID, m.ReplyToMessage.ID, p) // keep asking
			reply(view.TxtBadAmount)
			return
		}
		if _, err := b.Orders.SetReceived(ctx, p.OrderID, v, who); err != nil {
			reply(errText(err))
		}
	case "reject":
		if _, err := b.Orders.Reject(ctx, p.OrderID, m.Text, who); err != nil {
			reply(errText(err))
		}
	}
}

func errText(err error) string {
	if errors.Is(err, orders.ErrStateChanged) {
		return view.TxtOrderDone
	}
	return "⚠️ " + err.Error()
}
```

Note on the "Без причины" button: Telegram does not allow an inline keyboard and ForceReply on one message; the `EditMessageReplyMarkup` call replaces ForceReply with the inline button after the reply prompt is shown, which keeps the message replyable (replying works on any message) — the ForceReply only pre-opens the reply UI. Acceptable; if the edit fails, the admin can still reply.

Check go-telegram/bot v1.23 field names while implementing: `SendPhotoParams.Photo models.InputFile`, `models.InputFileString{Data}`, `ReplyParameters`, `models.ForceReply{ForceReply, Selective}`, `GetChatParams`, `EditMessageCaptionParams`, `EditMessageReplyMarkupParams`; `ChatFullInfo.Username`. Adjust to the actual names (`go doc github.com/go-telegram/bot SendPhotoParams`).

- [ ] **Step 8: Run** `go build ./... && go test ./internal/telegram/...` → PASS (main still compiles: `Orders` nil means order handlers are inert; the `CbRenew` case must guard `b.Orders == nil` by answering `view.TxtSoon`).
- [ ] **Step 9: Commit** `git add internal/telegram && git commit -m "feat(bot): receipts, admin chat card with approve/correct/reject, renew flow and payments history"`

---

### Task 8: WebApp API — `/api/orders*`

**Files:** Create `internal/webapp/orders.go`, `internal/webapp/orders_test.go`; modify `internal/webapp/server.go`, `internal/webapp/server_test.go` (helper `app` gets an `Orders` service).

**Interfaces — Consumes:** `orders.Service` (Quote, Create, RequestReceipt, PayFromBalance, Cancel, List, ForCustomer).
**Produces (JSON):** `Quote` as in Task 5; order view:

```go
type orderView struct {
	ID          int       `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Price       float64   `json:"price"`
	FromBalance float64   `json:"from_balance"`
	ToPay       float64   `json:"to_pay"`
	Paid        float64   `json:"paid"`
	Method      string    `json:"method"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
```

Routes (behind `auth`):
- `POST /api/orders/quote` body `orders.Request` → `Quote` | 400 `{"code":"bad_request"}`.
- `POST /api/orders` → `{"order": orderView, "details": string}` (201) | 400 | 409 `{"code":"manual_off"}`.
- `POST /api/orders/:id/receipt` → 204 (sets chat state, bot sends pay screen) | 404 | 409 `state_changed`.
- `POST /api/orders/:id/pay-balance` → `orderView` | 404 | 409.
- `POST /api/orders/:id/cancel` → 204 | 404 | 409.
- `GET /api/orders?page=0` → `{"orders": [...], "total": n}`.
Panel unavailable → 503 `unavailable`.

`auth` additionally stores `c.Locals("tg", u.ID)`; the order's `ChatID` is that id (private chat id == user id).

- [ ] **Step 1: Failing test** `orders_test.go` — build a Service like orders' `setup` (copy the minimal parts: temp store, Fake with customer 5/tg 77 and quotes, a no-op Notifier):

```go
package webapp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

type nopNotify struct{ pay int }

func (n *nopNotify) PayScreen(context.Context, orders.Order, string) error { n.pay++; return nil }
func (*nopNotify) PostCard(context.Context, orders.Order) (int, error)     { return 1, nil }
func (*nopNotify) UpdateCard(context.Context, orders.Order) error          { return nil }
func (*nopNotify) Customer(context.Context, orders.Order) error            { return nil }

func withOrders(t *testing.T, f *vpnctl.Fake, d *Deps) *nopNotify {
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	n := &nopNotify{}
	d.Orders = &orders.Service{DB: st.DB(), Chats: st, API: f, PayMode: config.PayMixed, Manual: true, Details: "Карта",
		TTL: time.Hour, Notify: n, Now: time.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return n
}

func do(t *testing.T, d *Deps, method, path, auth, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func ordersFake() *vpnctl.Fake {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77)), Subscriptions: []vpnctl.Subscription{{ID: 3, Title: "Дом"}}})
	f.Add(vpnctl.Customer{ID: 6, Name: "Bob", TelegramID: ptr(int64(88))})
	f.Quotes = map[string]vpnctl.ExtendQuote{"subscription:3": {CustomerID: 5, Amount: 300}}
	return f
}

func TestOrdersFlow(t *testing.T) {
	f := ordersFake()
	d := app(f)
	n := withOrders(t, f, d)
	body := `{"kind":"renew","item_kind":"subscription","item_id":3,"months":1}`
	code, out := do(t, d, "POST", "/bot/api/orders/quote", tma(77), body)
	if code != 200 || !strings.Contains(out, `"to_pay":300`) {
		t.Fatalf("quote %d %s", code, out)
	}
	code, out = do(t, d, "POST", "/bot/api/orders", tma(77), body)
	var created struct {
		Order   struct{ ID int; Status string } `json:"order"`
		Details string                          `json:"details"`
	}
	json.Unmarshal([]byte(out), &created)
	if code != 201 || created.Order.Status != "awaiting_receipt" || created.Details != "Карта" {
		t.Fatalf("create %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders/1/receipt", tma(77), ""); code != 204 || n.pay != 1 {
		t.Fatalf("receipt %d pay screens %d", code, n.pay)
	}
	code, out = do(t, d, "GET", "/bot/api/orders", tma(77), "")
	if code != 200 || !strings.Contains(out, `"total":1`) {
		t.Fatalf("list %d %s", code, out)
	}
}

func TestOrdersOwnership(t *testing.T) {
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"topup","amount":100}`)
	for _, p := range []string{"/bot/api/orders/1/receipt", "/bot/api/orders/1/cancel", "/bot/api/orders/1/pay-balance"} {
		if code, _ := do(t, d, "POST", p, tma(88), ""); code != 404 {
			t.Errorf("%s by another customer: %d", p, code)
		}
	}
	if code, out := do(t, d, "GET", "/bot/api/orders", tma(88), ""); !strings.Contains(out, `"total":0`) {
		t.Errorf("foreign list %d %s", code, out)
	}
	if code, _ := do(t, d, "POST", "/bot/api/orders", tma(77), `{"kind":"renew","item_kind":"subscription","item_id":99,"months":1}`); code != 400 {
		t.Errorf("foreign item: %d", code)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/webapp/` → FAIL.
- [ ] **Step 3: Implement.** `server.go`: `Deps.Orders *orders.Service`; in `auth` add `c.Locals("tg", u.ID)`; routes:

```go
	api.Get("/orders", s.listOrders)
	api.Post("/orders/quote", s.quoteOrder)
	api.Post("/orders", s.createOrder)
	api.Post("/orders/:id/receipt", s.orderAction(s.receipt))
	api.Post("/orders/:id/pay-balance", s.orderAction(s.payBalance))
	api.Post("/orders/:id/cancel", s.orderAction(s.cancel))
```

`orders.go`:

```go
package webapp

func view(o orders.Order) orderView {
	return orderView{ID: o.ID, Kind: string(o.Kind), Title: o.Title, Price: o.Price, FromBalance: o.FromBalance, ToPay: o.ToPay,
		Paid: o.Paid(), Method: string(o.Method), Status: string(o.Status), Reason: o.RejectReason, Until: o.ResultUntil, CreatedAt: o.CreatedAt}
}

func (s *server) orderErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, orders.ErrBadRequest):
		return fail(c, fiber.StatusBadRequest, "bad_request")
	case errors.Is(err, orders.ErrNotFound):
		return fail(c, fiber.StatusNotFound, "not_found")
	case errors.Is(err, orders.ErrStateChanged):
		return fail(c, fiber.StatusConflict, "state_changed")
	case errors.Is(err, orders.ErrManualOff):
		return fail(c, fiber.StatusConflict, "manual_off")
	}
	s.Log.Error("webapp orders", "err", err)
	return fail(c, fiber.StatusServiceUnavailable, "unavailable")
}

func (s *server) quoteOrder(c *fiber.Ctx) error {
	var r orders.Request
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	q, err := s.Orders.Quote(c.UserContext(), c.Locals("customer").(*vpnctl.Customer), r)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.JSON(q)
}

func (s *server) createOrder(c *fiber.Ctx) error {
	var r orders.Request
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	o, err := s.Orders.Create(c.UserContext(), c.Locals("customer").(*vpnctl.Customer), c.Locals("tg").(int64), r)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"order": view(*o), "details": s.Orders.Details})
}

func (s *server) listOrders(c *fiber.Ctx) error {
	page := max(c.QueryInt("page", 0), 0)
	list, total, err := s.Orders.List(c.UserContext(), c.Locals("customer").(*vpnctl.Customer).ID, 20, page*20)
	if err != nil {
		return s.orderErr(c, err)
	}
	out := make([]orderView, 0, len(list))
	for _, o := range list {
		out = append(out, view(o))
	}
	return c.JSON(fiber.Map{"orders": out, "total": total})
}

// orderAction resolves :id for the caller's customer and runs fn.
func (s *server) orderAction(fn func(c *fiber.Ctx, customerID, id int) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil || id < 1 {
			return fail(c, fiber.StatusNotFound, "not_found")
		}
		return fn(c, c.Locals("customer").(*vpnctl.Customer).ID, id)
	}
}

func (s *server) receipt(c *fiber.Ctx, customerID, id int) error {
	if _, err := s.Orders.RequestReceipt(c.UserContext(), customerID, id); err != nil {
		return s.orderErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) payBalance(c *fiber.Ctx, customerID, id int) error {
	o, err := s.Orders.PayFromBalance(c.UserContext(), customerID, id)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.JSON(view(*o))
}

func (s *server) cancel(c *fiber.Ctx, customerID, id int) error {
	if err := s.Orders.Cancel(c.UserContext(), customerID, id); err != nil {
		return s.orderErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
```

Rename the helper `view` → `orderJSON` if it clashes with anything in the package. `vpnctl.ErrUnavailable` from Quote falls into the 503 branch.
- [ ] **Step 4: Run** `go test ./internal/webapp/` → PASS.
- [ ] **Step 5: Commit** `git add internal/webapp && git commit -m "feat(webapp): orders API — quote, create, receipt, pay from balance, cancel, history"`

---

### Task 9: Wiring — main, expiry sweep, docs

**Files:** Modify `cmd/bot/main.go`, `internal/notify/runner.go`, `internal/notify/runner_test.go`, README/env docs.

- [ ] **Step 1: Failing test** (append to `runner_test.go`; reuse its runner setup — it builds `&Runner{API: f, Store: …, Send: …, Log: …}`):

```go
func TestRunOnceCallsSweep(t *testing.T) {
	r := newTestRunner(t) // the existing constructor in this file; if none, build Runner like the other tests do
	called := false
	r.Sweep = func(context.Context) error { called = true; return nil }
	r.RunOnce(context.Background(), time.Now())
	if !called {
		t.Fatal("sweep not called")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/notify/` → FAIL.
- [ ] **Step 3: Implement.** `Runner` gets `Sweep func(ctx context.Context) error // housekeeping on every tick (expired orders); optional`; at the start of `RunOnce`:

```go
	if r.Sweep != nil {
		if err := r.Sweep(ctx); err != nil {
			r.Log.Error("notify: sweep", "err", err)
		}
	}
```

`main.go`:

```go
	svc := &orders.Service{
		DB: st.DB(), Chats: st, API: api, PayMode: cfg.PayMode, Manual: cfg.ManualEnabled(),
		Details: cfg.ManualPayDetails, TTL: cfg.OrderTTL, Now: time.Now, Log: log,
	}
	bot, err := telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Orders: svc, Prompts: st, AdminChatID: cfg.AdminChatID, IsAdmin: cfg.IsAdmin,
		WebAppURL: cfg.WebAppURL(), PayMode: cfg.PayMode, Log: log,
	})
	if err != nil {
		return err
	}
	svc.Notify = bot
```

`webapp.Deps` += `Orders: svc`. Notify runner: `Sweep: svc.ExpireStale`. If `NOTIFY_INTERVAL=0`, still sweep: start a small goroutine with a 10-minute ticker calling `svc.ExpireStale` only when `cfg.NotifyInterval == 0`. Log line: add `"manual", cfg.ManualEnabled()`. If manual is on but the bot is not a member of `ADMIN_CHAT_ID`, the first card fails — log at startup: `bot.CheckAdminChat(ctx)` calling `GetChat`, warning on error (add a 6-line method in `admin.go`).

- [ ] **Step 4: Docs.** In README (and `.env.example`/compose if they list env vars) add:

```
ADMIN_CHAT_ID       chat (private or group) that receives payment receipts; add the bot to the group
ADMIN_IDS           comma-separated Telegram ids allowed to approve/reject (e.g. 12345,67890)
MANUAL_PAY_DETAILS  how to pay, shown with the amount; "\n" for line breaks (e.g. "Карта 2200 0000 0000 0000\nСБП +7…")
ORDER_TTL           unpaid orders expire after this (default 48h)
```

and a note: "vpn-control token: create it with the *bot* preset (it now includes payment scopes) — re-issue the existing token."
- [ ] **Step 5: Run** `go build ./... && go vet ./... && go test -race ./...` → PASS.
- [ ] **Step 6: Commit** `git add -A cmd internal README* .env* compose* 2>/dev/null; git commit -m "feat: wire orders into the bot, WebApp and notifier; document payment settings"`

---

### Task 10: WebApp UI — checkout and payment history

**Files:** Modify `web/src/types.ts`, `web/src/api.ts`, `web/src/App.tsx`, `web/src/renew.ts`, `web/src/pages/SubscriptionDetails.tsx`, `web/src/pages/Balance.tsx`, `web/src/pages/Tariffs.tsx`, `web/src/ui/BalanceCard.tsx`, `web/public/locales/ru/bot.json`; create `web/src/pages/Checkout.tsx`, `web/src/ui/OrdersList.tsx`.

**Consumes:** Task 8 JSON.

- [ ] **Step 1: Types** (`types.ts`):

```ts
export type OrderKind = 'renew' | 'subscribe' | 'addon' | 'change_tariff' | 'topup'
export interface OrderRequest {
    kind: OrderKind
    item_kind?: 'subscription' | 'addon'
    item_id?: number
    tariff_id?: number
    months?: number
    days?: number
    amount?: number
}
export interface OrderQuote {
    title: string
    price: number
    from_balance: number
    to_pay: number
    methods: ('manual' | 'balance')[]
}
export interface Order {
    id: number
    kind: OrderKind
    title: string
    price: number
    from_balance: number
    to_pay: number
    paid: number
    method: 'manual' | 'balance'
    status: 'new' | 'awaiting_receipt' | 'pending_review' | 'approving' | 'approved' | 'credited' | 'failed' | 'rejected' | 'expired' | 'cancelled'
    reason?: string
    until?: string
    created_at: string
}
```

- [ ] **Step 2: API** (`api.ts`): generalise `get` into `call<T>(method, path, body?)` (same headers + `Content-Type: application/json` when body; 204 → `undefined as T`), keep `get` as a wrapper. Add:

```ts
export const useOrders = () => useQuery({ queryKey: ['orders'], queryFn: () => get<{ orders: Order[]; total: number }>('orders'), retry })
export const useOrderQuote = (r: OrderRequest | null) =>
    useQuery({ queryKey: ['quote', r], queryFn: () => call<OrderQuote>('POST', 'orders/quote', r), enabled: !!r, retry })
export const createOrder = (r: OrderRequest) => call<{ order: Order; details: string }>('POST', 'orders', r)
export const requestReceipt = (id: number) => call<void>('POST', `orders/${id}/receipt`)
export const payFromBalance = (id: number) => call<Order>('POST', `orders/${id}/pay-balance`)
export const cancelOrder = (id: number) => call<void>('POST', `orders/${id}/cancel`)
```

Add `close(): void` to the `WebApp` interface in `telegram.ts`.
- [ ] **Step 3: Checkout page** (`pages/Checkout.tsx`) — route `/checkout?kind=…&item_kind=…&item_id=…&tariff_id=…`:
  - Reads params with `useSearchParams`; local state `months` (default first tariff period or 1), `days`, `amount` (topup), `tariffId` (change_tariff picker, subscribe/addon fixed from params), `subId` (addon: `Select` of the profile's subscriptions when `item_id` missing).
  - Term chips: for renew/subscribe/addon — `SegmentedControl` of `[1,3,6,12]` months merged with the tariff's `periods` (via `useTariffs`), for change_tariff no term; for topup a `NumberInput` (min 1).
  - `useOrderQuote(request)` → shows title, price, from balance, **to pay** (reuse `RenewalSplit` styling or `Row`s), error `bad_request` → `t('checkout.unavailable')`.
  - Buttons: if `methods` includes `balance` → `CtaButton` "Оплатить с баланса" → `createOrder` then `payFromBalance` → `haptic()`, invalidate `['me']`, `['orders']`, navigate to `/balance`. If `manual` → `CtaButton` "Оплатить переводом" → `createOrder` then show details card (`Card` with `details` text, `white-space: pre-wrap`, copy button for the text) and button "📎 Отправить квитанцию в чат" → `requestReceipt(id)` → `tg.close()`. If `methods` is empty → text `t('checkout.manual_off')`.
  - `ApiError` 409 `manual_off` → same text; 503 → `ErrorScreen` retry.
  Write the component following the structure of `pages/SubscriptionDetails.tsx` (Mantine `Stack`, `Title order={3}`, `CtaButton`, i18n `t('checkout.*')`, `tg.BackButton` handled by `AppShell`).
- [ ] **Step 4: Orders list** (`ui/OrdersList.tsx`): `useOrders()`; renders up to 20 rows: status badge (color map: pending_review/approving/awaiting_receipt → yellow, approved → teal, credited → blue, failed → orange, rejected/expired/cancelled → gray), `#id · date`, title, `fmtMoney(paid)`, reason for rejected; `awaiting_receipt` rows get a "Отправить квитанцию" small button (`requestReceipt` → `tg.close()`) and "Отменить" (`cancelOrder` → invalidate). Empty → nothing (component returns null).
- [ ] **Step 5: Wire entry points.**
  - `renew.ts`: delete `pay()`; add `export const checkoutRenew = (subId: number) => \`/checkout?kind=renew&item_kind=subscription&item_id=${subId}\``.
  - `SubscriptionDetails.tsx:50` → `onClick={() => navigate(checkoutRenew(s.id))}`; add a secondary `Button` "Сменить тариф" → `/checkout?kind=change_tariff&item_kind=subscription&item_id=${s.id}`.
  - `BalanceCard.tsx:48` → `navigate(checkoutRenew(urgent.id))`.
  - `Tariffs.tsx:81` → base tariff: `/checkout?kind=subscribe&tariff_id=${tf.id}`; addon tariff: `/checkout?kind=addon&tariff_id=${tf.id}` (Checkout asks which subscription).
  - `Balance.tsx:44` → `/checkout?kind=topup`; render `<OrdersList />` above the ledger with a section title `t('orders.title')`.
  - `App.tsx`: `<Route element={<Checkout p={p} />} path="/checkout" />`.
- [ ] **Step 6: Locales** (`public/locales/ru/bot.json`) — add:

```json
"checkout": {
    "title": "Оплата",
    "term": "Срок",
    "price": "Цена",
    "from_balance": "С баланса",
    "to_pay": "К оплате",
    "amount": "Сумма пополнения",
    "subscription": "Подписка",
    "tariff": "Новый тариф",
    "pay_balance": "Оплатить с баланса",
    "pay_manual": "Оплатить переводом",
    "details": "Реквизиты",
    "send_receipt": "📎 Отправить квитанцию в чат",
    "receipt_hint": "Переведите сумму по реквизитам, затем пришлите фото или PDF квитанции в чат с ботом.",
    "manual_off": "Оплата переводом не настроена — пополнить баланс можно у администратора.",
    "unavailable": "Этот вариант сейчас недоступен",
    "done": "Готово!"
},
"orders": {
    "title": "Заявки на оплату",
    "send_receipt": "Отправить квитанцию",
    "cancel": "Отменить",
    "status": {
        "new": "Создан", "awaiting_receipt": "Ждёт квитанцию", "pending_review": "На проверке", "approving": "Проводится",
        "approved": "Оплачен", "credited": "Зачислен на баланс", "failed": "Ошибка", "rejected": "Отклонён",
        "expired": "Истёк", "cancelled": "Отменён"
    }
}
```

and `"sub": { …, "change_tariff": "Сменить тариф" }`; remove `tariffs.pay_soon` if nothing references it.
- [ ] **Step 7: Verify** — `cd web && bun run typecheck && bun run build` → no errors; then `go build ./...` (embeds `web/dist`).
- [ ] **Step 8: Manual check** in the running app (`task`/compose as in README): renew from a subscription card → manual pay → "Отправить квитанцию" closes the WebApp and the chat shows the pay screen; send a photo → admin chat gets the card; approve → customer message, order "Оплачен" in Balance.
- [ ] **Step 9: Commit** `git add web && git commit -m "feat(webapp): checkout for renewals, new subscriptions, add-ons, tariff changes and top-ups; payment requests list"`

---

## Self-review notes

- Spec coverage: preset (T1); config (T2); panel calls (T3); storage (T4); model/pricing/receipt/expiry (T5); Fulfill, corrections, reject, retry, balance (T6); admin card, prompts, notices, history, renew-in-bot, group gate (T7); WebApp API (T8); wiring, sweep, docs (T9); WebApp UI (T10).
- Deviation from spec, deliberate: admin prompts live in `admin_prompts` (keyed by message) instead of `chat_state.prompt_msg_id`.
- Balance-covered subscribe/addon/change_tariff go straight to the panel second step with `allow_debt=false` (no `customers:pay` because amount = 0) — covered by `fulfill` (`amount > 0` guard).
