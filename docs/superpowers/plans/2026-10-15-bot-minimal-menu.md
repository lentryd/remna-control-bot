# Bot reduced to a launcher — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The bot's main menu keeps its buttons but each one opens the WebApp on the matching page; every other list/choice screen and its callbacks leave the bot.

**Architecture:** `view.Home`, the notices and the order notice build `web_app` buttons with `Links.Page(hash)`. The bot keeps one callback family for navigation (`m:home`, used by `Notice`) and `pc:` (cancel on the payment-details screen). A new customer route `/orders` renders the existing `OrdersList` as a page. Dead screens, routes, tests, golden files and i18n keys are deleted.

**Tech Stack:** Go (go-telegram/bot, golden tests in `internal/telegram/view`), React + Mantine + i18next in `web/` (checks: `bun run typecheck`, `bun run check:locales`).

**Spec:** `docs/superpowers/specs/2026-10-15-bot-minimal-menu-design.md`

## Global Constraints

- Every Home button is a `web_app` button; `Links.Page(hash)` = `WebApp + "/#" + hash` (`WebApp` = `PUBLIC_URL + WEBAPP_PATH`, e.g. `https://x.dev/bot`).
- Pages: `/`, `/subscriptions`, `/balance`, `/referrals`, `/tariffs`, `/orders` (new), renew = `/checkout?kind=renew&item_kind=subscription&item_id=<id>` (same string as `web/src/renew.ts` `checkoutRenew`).
- Stays in the bot: `/start`, `/menu`, `/help`, `/admin`, onboarding and link entry, `PayScreen` + `pc:` cancel, receipt intake, Stars (`pre_checkout`, `successful_payment`), admin cards, notifications.
- No support for old inline keyboards in existing chats (project is in development).
- `en.json` and `ru.json` of `internal/i18n/locales` and the `web/public/locales/{en,ru}` catalogs keep key parity (tests `TestCatalogParity`, `bun run check:locales`); no jargon (`TestNoJargon`).
- Commit messages: no `Co-Authored-By` or other Claude attribution (user's global rule).
- Run Go commands from the repo root, frontend commands from `web/`.

## Review Focus

- `WEBAPP_PATH=/bot`: every URL carries the path (`https://x.dev/bot/#/orders`), pinned in Task 2.
- A profile with no subscriptions: Home still shows all six buttons, all `web_app`, no renew row (Task 2).
- Renew button of a subscription whose id is not 1: the id lands in the checkout URL (Task 2).
- `/orders` for a customer with no orders shows an empty state, not a blank page (Task 1).
- An old/unknown callback (`m:subs`, `s:3`, `rp:3:1:0`) must not panic or act: `parseRoute` rejects it (Task 3).
- `pc:` still cancels an order from the payment-details screen after the callback cleanup (Task 3: kept handler, `parseOrderID` test).
- All remaining screens still fit Telegram's limits (existing `TestScreensFitTelegramLimit`, adjusted in Task 3).

---

### Task 1: `/orders` page in the WebApp

**Files:**
- Create: `web/src/pages/Orders.tsx`
- Modify: `web/src/ui/OrdersList.tsx` (heading prop), `web/src/App.tsx` (route), `web/public/locales/en/bot.json`, `web/public/locales/ru/bot.json` (`empty.orders`)

**Interfaces:**
- Consumes: `useOrders()` from `../api`, `Empty` from `../ui/Empty`, existing `orders.title` key.
- Produces: route `#/orders` (Task 2 links to it); `OrdersList({ heading?: boolean })`, default `true`.

There is no frontend test runner; the checks are the typecheck, the locale parity script and the build.

- [ ] **Step 1: Add the empty-state key to both catalogs**

In `web/public/locales/en/bot.json`, inside `"empty"`, after `"ledger"`:

```json
        "orders": "No payments yet",
```

In `web/public/locales/ru/bot.json`, inside `"empty"`, after `"ledger"`:

```json
        "orders": "Платежей пока не было",
```

- [ ] **Step 2: Let `OrdersList` hide its own heading**

In `web/src/ui/OrdersList.tsx` change the signature and the heading line:

```tsx
export function OrdersList({ heading = true }: { heading?: boolean }) {
```

```tsx
            {heading && <Text fw={600}>{t('orders.title')}</Text>}
```

- [ ] **Step 3: Create the page**

`web/src/pages/Orders.tsx`:

```tsx
// Orders.tsx — the customer's payment requests as a page of their own (the bot's "Payments" button).
import { Stack, Title } from '@mantine/core'
import { IconReceipt } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { useOrders } from '../api'
import { Empty } from '../ui/Empty'
import { OrdersList } from '../ui/OrdersList'

export function Orders() {
    const { t } = useTranslation()
    const q = useOrders()
    const none = !q.isPending && (q.data?.orders ?? []).length === 0
    return (
        <Stack gap="lg">
            <Title order={3}>{t('orders.title')}</Title>
            {none ? <Empty icon={<IconReceipt />} title={t('empty.orders')} /> : <OrdersList heading={false} />}
        </Stack>
    )
}
```

- [ ] **Step 4: Register the route**

In `web/src/App.tsx` add `import { Orders } from './pages/Orders'` (alphabetical, after `Overview`'s neighbours) and, in `CustomerApp`'s `<Routes>`, before the `*` route:

```tsx
                <Route element={<Orders />} path="/orders" />
```

- [ ] **Step 5: Verify**

Run (from `web/`): `bun run typecheck && bun run check:locales && bun run build`
Expected: all three exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/Orders.tsx web/src/ui/OrdersList.tsx web/src/App.tsx web/public/locales
git commit -m "feat(web): /orders page for the payments list"
```

---

### Task 2: Home, notices and the order notice open WebApp pages

**Files:**
- Modify: `internal/telegram/view/screens.go` (`Home`), `internal/telegram/view/notices.go`, `internal/telegram/view/orders.go` (`OrderNotice`), `internal/telegram/orders.go` (`Customer`), `internal/telegram/view/buttons_test.go`, `internal/telegram/view/renew_test.go`, `internal/telegram/view/orders_test.go` (call sites)
- Regenerate: `internal/telegram/view/testdata/{en,ru}/home*.golden`, `notice_*.golden`

**Interfaces:**
- Consumes: `Links.Page(hash string) *models.WebAppInfo` (existing).
- Produces: `view.OrderNotice(loc i18n.Localizer, o orders.Order, l Links) Screen` (new third parameter); `func renewPage(subID int) string` in package `view` returning `/checkout?kind=renew&item_kind=subscription&item_id=<id>`.

- [ ] **Step 1: Write the failing tests**

Replace the body of `internal/telegram/view/buttons_test.go` below the package/imports with (keep `TestSubscriptionLinkOpens` only until Task 3 deletes it; leave it untouched in this task):

```go
package view

import (
	"testing"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/profile"
)

// pages maps a button's text to the WebApp URL it opens; any button that is not a web_app fails the test.
func pages(t *testing.T, s Screen) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if b.WebApp == nil || b.CallbackData != "" {
				t.Fatalf("button %q is not a web_app button: %+v", b.Text, b)
			}
			out[b.Text] = b.WebApp.URL
		}
	}
	return out
}

func TestHomeOpensAppFirst(t *testing.T) {
	for _, s := range []Screen{Home(ru, fixture(), links, now), Home(ru, fixture(), links, now.AddDate(1, 0, 0))} {
		first := s.Keyboard.InlineKeyboard[0][0]
		if first.WebApp == nil || first.Text != ru.T("btn.app") {
			t.Fatalf("first button = %+v", first)
		}
	}
}

func TestHomeButtonsOpenWebAppPages(t *testing.T) {
	got := pages(t, Home(ru, fixture(), links, now)) // the fixture's subscription ends in 2 days: the renew row is there
	want := map[string]string{
		ru.T("btn.app"):     "https://x.dev/bot/#/",
		ru.T("btn.subs"):    "https://x.dev/bot/#/subscriptions",
		ru.T("btn.balance"): "https://x.dev/bot/#/balance",
		ru.T("btn.refs"):    "https://x.dev/bot/#/referrals",
		ru.T("btn.tariffs"): "https://x.dev/bot/#/tariffs",
		ru.T("btn.orders"):  "https://x.dev/bot/#/orders",
		clip(ru.T("btn.renew_sub", "Основная"), maxTitle): "https://x.dev/bot/#/checkout?kind=renew&item_kind=subscription&item_id=3",
	}
	if len(got) != len(want) {
		t.Fatalf("buttons = %v", got)
	}
	for text, url := range want {
		if got[text] != url {
			t.Errorf("%q opens %q, want %q", text, got[text], url)
		}
	}
}

func TestHomeWithoutSubscriptionsHasNoRenewRow(t *testing.T) {
	got := pages(t, Home(ru, profile.Profile{Name: "Ann"}, links, now))
	if len(got) != 6 {
		t.Fatalf("want the 6 menu buttons, got %v", got)
	}
}

func TestOrderNoticeOpensOrdersPage(t *testing.T) {
	s := OrderNotice(ru, orders.Order{Status: orders.StatusApproved}, links)
	if got := pages(t, s)[ru.T("btn.orders")]; got != "https://x.dev/bot/#/orders" {
		t.Fatalf("opens %q", got)
	}
}

func TestNoticesOpenWebAppPages(t *testing.T) {
	n := fixtureNotice()
	if got := pages(t, TrafficAlert(ru, n, links, now)); got[ru.T("btn.open_sub")] != "https://x.dev/bot/#/subscriptions/3" || got[ru.T("btn.tariffs_app")] != "https://x.dev/bot/#/tariffs" {
		t.Fatalf("traffic: %v", got)
	}
	if got := pages(t, PaymentReminder(ru, n, links, now)); got[ru.T("btn.renew")] != "https://x.dev/bot/#/checkout?kind=renew&item_kind=subscription&item_id=3" || got[ru.T("btn.details")] != "https://x.dev/bot/#/subscriptions/3" {
		t.Fatalf("reminder: %v", got)
	}
}
```

`fixtureNotice()` must return the `notify.Notice` that `notices_test.go` already builds for its first case with `SubID: 3`. Read `internal/telegram/view/notices_test.go`: if it builds the notice inline, move that literal into `func fixtureNotice() notify.Notice` in the same file and make the existing cases call it; the literal must have `SubID: 3`, a non-nil `Upgrade` and `ResetAt` so `TrafficAlert` renders both buttons.

Keep the existing `TestSubscriptionLinkOpens` from the old file below these tests (it is removed in Task 3).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/telegram/view -run 'TestHome|TestOrderNotice|TestNotices' -v`
Expected: build FAIL (`OrderNotice` takes 2 arguments) — then, after the Step 3 signature change but before the rest, FAIL on "is not a web_app button".

- [ ] **Step 3: Implement**

`internal/telegram/view/screens.go` — in `Home`, replace the renew row append and the final keyboard:

```go
		rows = append(rows, []btn{{Text: clip(loc.T("btn.renew_sub", s.Title), maxTitle), WebApp: l.Page(renewPage(s.ID))}})
```

```go
	return Screen{Text: b.String(), Keyboard: kb(append(rows,
		[]btn{{Text: loc.T("btn.subs"), WebApp: l.Page("/subscriptions")}, {Text: loc.T("btn.balance"), WebApp: l.Page("/balance")}},
		[]btn{{Text: loc.T("btn.refs"), WebApp: l.Page("/referrals")}, {Text: loc.T("btn.tariffs"), WebApp: l.Page("/tariffs")}},
		[]btn{{Text: loc.T("btn.orders"), WebApp: l.Page("/orders")}},
	)...)}
```

Add to `screens.go`:

```go
// renewPage is the WebApp checkout that renews a subscription (web/src/renew.ts checkoutRenew).
func renewPage(subID int) string {
	return "/checkout?kind=renew&item_kind=subscription&item_id=" + strconv.Itoa(subID)
}
```

`internal/telegram/view/notices.go` — `TrafficAlert` second row and `PaymentReminder` first row:

```go
		[]btn{{Text: loc.T("btn.open_sub"), WebApp: l.Page("/subscriptions/" + strconv.Itoa(n.SubID))}},
```

```go
		[]btn{{Text: loc.T("btn.renew"), WebApp: l.Page(renewPage(n.SubID))}},
```

`internal/telegram/view/orders.go` — `OrderNotice`:

```go
func OrderNotice(loc i18n.Localizer, o orders.Order, l Links) Screen {
```

and its last line:

```go
	return Screen{Text: text, Keyboard: kb([]btn{{Text: loc.T("btn.orders"), WebApp: l.Page("/orders")}})}
```

`internal/telegram/orders.go` `Customer`:

```go
	s := view.OrderNotice(b.locFor(ctx, o.ChatID), o, b.links)
```

Update the other `OrderNotice(` call sites in `view/orders_test.go` to pass `links`.

- [ ] **Step 4: Update the renew tests that look for the old callback**

Run `go test ./internal/telegram/view` and fix each failure in `renew_test.go` that asserts `CbRenew`/callback data on the Home or reminder keyboards: assert instead that the button has `WebApp.URL == "https://x.dev/bot/#"+renewPage(3)`. (`TestSubscriptionCardHasRenew` and `TestSubscriptionCardShowsHowRenewalIsPaid` test the `Subscription` card, which Task 3 deletes — leave them alone here.)

- [ ] **Step 5: Regenerate goldens and review the diff**

Run: `go test ./internal/telegram/view -update && git diff --stat internal/telegram/view/testdata`
Expected: only `home*.golden` and `notice_*.golden` changed (en and ru); the diff shows `→ app …` replacing the old plain buttons.

- [ ] **Step 6: Run the package**

Run: `go test ./internal/telegram/... ./cmd/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/telegram
git commit -m "feat(bot): menu, notices and order notice open WebApp pages"
```

---

### Task 3: Remove the screens and callbacks that moved to the WebApp

**Files:**
- Modify: `internal/telegram/view/screen.go`, `screens.go`, `orders.go`, `paging.go`, `internal/telegram/route.go`, `bot.go`, `orders.go`, and their tests (`view/*_test.go`, `route_test.go`)
- Delete golden files: `internal/telegram/view/testdata/{en,ru}/{subscriptions,subscriptions_empty,subscription,balance,referrals,referrals_empty,tariffs}.golden`

**Interfaces:**
- Consumes: `view.CbHome`, `view.CbOrderCancel`, `parseOrderID` (kept).
- Produces: `parseRoute` accepts only `m:home`; `view` exports no `Subscriptions`, `Subscription`, `Balance`, `Referrals`, `Tariffs`, `RenewPeriods`, `Confirm`, `ProviderPay`, `Orders`, `ProviderButton`.

- [ ] **Step 1: Write the failing route test**

Replace `TestParseRoute` in `internal/telegram/route_test.go`:

```go
func TestParseRoute(t *testing.T) {
	got, ok := parseRoute("m:home")
	if !ok || got != (route{kind: "m:home"}) {
		t.Fatalf("parseRoute(m:home) = %+v,%v", got, ok)
	}
	// callbacks of screens that moved to the WebApp are not routes any more
	for _, in := range []string{"", "x", "m:subs", "m:bal", "m:ref", "m:tar", "m:pay", "s:12", "ps:0", "pt:1", "r:3", "po:0", "rp:3:1:0", "pr:x", "pb:x", "pp:a:b"} {
		if _, ok := parseRoute(in); ok {
			t.Errorf("parseRoute(%q) accepted", in)
		}
	}
}

func TestParseOrderID(t *testing.T) {
	const id = "0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c"
	if got, ok := parseOrderID("pc:"+id, "pc:"); !ok || got != id {
		t.Fatalf("%q %v", got, ok)
	}
	for _, in := range []string{"pc:", "pc:1", "pc:" + id + "x", "pr:" + id} {
		if _, ok := parseOrderID(in, "pc:"); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
```

Delete `TestParseIDs` and `TestParseProviderCallback` from the same file (their helpers go away in Step 3). Keep `TestRefLink`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/telegram -run 'TestParseRoute|TestParseOrderID' -v`
Expected: FAIL (`parseRoute("m:subs")` is still accepted).

- [ ] **Step 3: Cut the production code**

`internal/telegram/route.go` — keep `route`, `RefLink`, `parseOrderID`; replace `parseRoute` and delete `parseIDs` and `parseProviderCallback` (and now-unused imports `strconv`, `strings` only if nothing else uses them; `strings` is still used by `parseOrderID`):

```go
func parseRoute(data string) (route, bool) {
	if data == view.CbHome {
		return route{kind: data}, true
	}
	return route{}, false
}
```

`internal/telegram/view/screen.go` — the constants block becomes:

```go
const (
	CbHome        = "m:home"
	CbOrderCancel = "pc:" // + order id
)
```

`internal/telegram/bot.go`:
- Registration: replace both `for` loops with

```go
	tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, view.CbHome, tgbot.MatchTypeExact, b.onCallback)
	tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, view.CbOrderCancel, tgbot.MatchTypePrefix, b.onOrderCallback)
```

- `onCallback`: after the `parseRoute` check and `Access.Start`, the whole `switch` becomes just the home case. Replace from `p := b.profile(r.Customer)` to the end of the function with:

```go
	b.show(ctx, chatID, msgID, view.Home(loc, b.profile(r.Customer), b.links, time.Now()))
```

`internal/telegram/orders.go`:
- Delete `renewPeriods`, `ordersPage`, `showOrders`, `providerButtons`.
- `onOrderCallback`: delete the `parseIDs` (rp:) block and the `parseProviderCallback` (pp:) block; in the `map[string]func(string) error` keep only the `view.CbOrderCancel` entry. Update the comment to `// onOrderCallback: pc: (cancel) from the customer.`
- Remove imports that become unused (`payments` stays — `onStarsPaid` uses it; `remnactl` goes if unused).

`internal/telegram/view/screens.go` — delete `Subscriptions`, `Subscription`, `Balance`, `Referrals`, `Tariffs`, `stateIcon`, `until` (and imports `net/url`, `models` if unused). Keep `Home`, `left`, `Notice`, `Plain`.

`internal/telegram/view/orders.go` — delete `ProviderButton`, `Confirm`, `providerTitle`, `ProviderPay`, `RenewOption`, `RenewPeriods`, `termText`, `Orders`, `ordersPerPage`, and the helper `id` if unreferenced. Keep `summary`, `PayScreen`, `AdminCard`, `AdminEntry`, `OrderNotice`, `statusIcon`, `statusText`, `methodTitle`, `plain`.

`internal/telegram/view/paging.go` — delete `subsPerPage`, `tariffsPerPage`, `maxAddons`, `page`, `pager`, `pageTitle`; keep `maxTitle`, `maxDescription` only if still referenced (grep; delete `maxDescription` if not), and `clip`.

Use the compiler to find the rest: `go build ./... && go vet ./...`; remove anything still unreferenced (`grep -rn '<name>' internal` before deleting).

- [ ] **Step 4: Cut the tests and goldens**

- `git rm internal/telegram/view/testdata/{en,ru}/{subscriptions,subscriptions_empty,subscription,balance,referrals,referrals_empty,tariffs}.golden`
- `screens_test.go`: in `TestScreens` and `TestEmptyStates` remove the cases of deleted screens (keep home, home_empty); delete tests that call deleted functions (`TestLeft` keeps only if it tests `left`; `TestHomeEscapesName` stays).
- `buttons_test.go`: delete `TestSubscriptionLinkOpens`.
- `paging_test.go`: delete `TestSubscriptionsPaging`, `TestTariffsPaging`, `TestTariffsHaveNoPickButtons`, `TestSubscriptionBackGoesToItsPage`; keep `TestScreensFitTelegramLimit` but reduce it to `Home` with a profile of 100 long-titled subscriptions (renew row uses `profile.Subscriptions[0]` only) and the notices.
- `renew_test.go`: delete `TestSubscriptionCardHasRenew`, `TestSubscriptionCardShowsHowRenewalIsPaid`; keep the Home/renewal-window tests (updated in Task 2).
- `orders_test.go`: delete `TestRenewPeriods`, `TestConfirmWithProviders`, `TestProviderPay`, `TestOrdersHistory`, `TestProviderTitlesFromCatalog` and any helper only they use; keep `TestPayScreen`, `TestAdminCard*`, `TestOrderNotice*`, `TestStoredOrderTextsPerViewer`.
- `newcomer_test.go`: if it renders a deleted screen, switch it to `Home`.

- [ ] **Step 5: Run everything**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. Then `go test ./internal/telegram/view -update && git status --short internal/telegram/view/testdata` — expected: no modified goldens (only the deletions).

- [ ] **Step 6: Commit**

```bash
git add -A internal/telegram
git commit -m "refactor(bot): drop the screens and callbacks that moved to the WebApp"
```

---

### Task 4: Prune unused bot i18n keys

**Files:**
- Modify: `internal/i18n/locales/en.json`, `internal/i18n/locales/ru.json`

**Interfaces:**
- Consumes: the state of the code after Task 3.
- Produces: catalogs without keys no code can reach.

- [ ] **Step 1: List candidates the code no longer references**

Run:

```bash
for k in $(python3 -c "import json;[print(k) for k in json.load(open('internal/i18n/locales/en.json'))]"); do
  grep -rqF "\"$k\"" --include='*.go' internal cmd || echo "$k"
done
```

Expected output: the keys with no literal reference — including `app.name`, `msg.panel_down`, the removed screens' keys (`subs.*`, `sub.*`, `balance.*`, `refs.*`, `tariffs.*`, `orders.*`, `fmt.page_of`, `msg.share`, `msg.pick_period`, `msg.soon` if unused, `btn.subs`... ) and some false positives.

- [ ] **Step 2: Remove false positives from the list**

Keys used through a prefix must stay even if the loop lists them: anything starting `status.`, `ledger.`, `payment.`, `time.`, `unit.`, `provider.`, `method.`, `term.` only if a `loc.T("<prefix>"+…)` or `Plural` call still builds them: check with `grep -rn '"status\.\|"payment\.\|"time\.\|"unit\.\|"provider\.\|"method\.\|"ledger\.\|"term\.' internal --include='*.go'`. `ledger.*` and `term.*` were used only by deleted screens — delete them; `status.*` (OrderNotice, AdminCard), `time.*` (`left`), `unit.*` (`Bytes`), `provider.*`/`method.*` (AdminCard) stay.

- [ ] **Step 3: Delete the remaining keys from both files**

Delete each confirmed key from `en.json` and `ru.json` (same set; nested objects are flat dotted keys in these files — keep the files' existing formatting and key order). Do not touch `msg.welcome`, `msg.need_link`, `msg.bad_*`, `msg.linked`, `msg.down`, `msg.help`, `msg.receipt_*`, `msg.order_*`, `order.*` used by `summary`/`OrderNotice`, `admin.*`, `alert.*`, `review.*`, `notice.*`, `cmd.*`, `menu.open`, `invoice.*`, `btn.app`, `btn.back`, `btn.home`, `btn.cancel`, `btn.open_admin`, `btn.orders`, `btn.subs`, `btn.balance`, `btn.refs`, `btn.tariffs`, `btn.renew`, `btn.renew_sub`, `btn.details`, `btn.open_sub`, `btn.tariffs_app`, `home.*`, `msg.provider_down`/`msg.pay_manual_off` only if still referenced by Go.

- [ ] **Step 4: Verify**

Run: `python3 -c "import json;[json.load(open(f'internal/i18n/locales/{l}.json')) for l in ('en','ru')]" && go test ./...`
Expected: PASS (`TestCatalogParity`, `TestNoJargon`, golden tests unchanged — a golden diff means a still-used key was deleted: restore it).

- [ ] **Step 5: Commit**

```bash
git add internal/i18n/locales
git commit -m "chore(i18n): drop bot texts of screens that moved to the WebApp"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md`, `docs/superpowers/specs/2026-10-06-remna-control-bot-design.md` (section "Bot UX", around lines 90-103), `docs/i18n.md` if it names removed screens

**Interfaces:** none.

- [ ] **Step 1: README**

Edit the paragraphs that say what the bot shows (lines ~3-5 and 56) so they say: the bot's menu opens the WebApp on the matching page; the bot itself only onboards (`/start`, link entry), takes receipts, handles Telegram Stars, sends notifications and admin order cards.

- [ ] **Step 2: Design spec "Bot UX"**

Replace the Home/Subscriptions/Balance/Referrals/Tariffs bullet list with the button→page table from `docs/superpowers/specs/2026-10-15-bot-minimal-menu-design.md`, and add one line pointing to that spec.

- [ ] **Step 3: Verify nothing references removed screens**

Run: `grep -rn "CbSubs\|CbBalance\|CbRefs\|CbTariffs\|CbRenewPick\|RenewPeriods\|view.Subscriptions\|view.Tariffs" --include='*.go' --include='*.md' . | grep -v "docs/superpowers/plans\|docs/superpowers/specs/2026-10-06"`
Expected: no output.

- [ ] **Step 4: Full verification and commit**

Run: `go test ./... && (cd web && bun run typecheck && bun run check:locales && bun run build)`
Expected: all pass.

```bash
git add README.md docs
git commit -m "docs: the bot is a launcher; screens live in the WebApp"
```
