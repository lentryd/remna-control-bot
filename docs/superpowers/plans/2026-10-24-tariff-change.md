# Tariff change (term exchange, reset dates, redesigned subscription pages) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let customers switch a subscription's tariff by paying the difference or by exchanging the remaining term, choose "new subscription / update existing" in the storefront, and see remaining time and limit-reset dates (with clear add-on grouping) everywhere limits are shown.

**Architecture:** The panel (`/Users/victor/Projects/Go/vpn-control`) owns the exchange math and applies it; the bot (`/Users/victor/Projects/Go/vpn-control-bot`) forwards the panel's quote through its order flow and the WebApp renders it. Panel tasks (P1, P2) ship first; the bot hides the exchange option when the panel's quote lacks the exchange fields.

**Tech Stack:** Go 1.26 (fiber, ent), React + Mantine + react-i18next in `web/` (bun), Tabler icons.

**Spec:** `docs/superpowers/specs/2026-10-24-tariff-change-design.md`

## Global Constraints

- Repo files, comments and commit messages in English; chat in Russian.
- No emoji in the UI: Tabler icons (calendar/hourglass for "Remaining", refresh arrow for "Limit reset").
- Strings go through i18n in both `web/public/locales/ru/bot.json` and `web/public/locales/en/bot.json`; bot notices in `internal/i18n/locales/{ru,en}.json`.
- Exchange math: `credit = oldMonthly * daysLeft / 30`, `newDays = round(credit / newMonthly * 30)`, `newExpire = now + newDays`; `oldMonthly` is the effective price (override, else tariff price), `newMonthly` the target's list price.
- Exchange rules: not offered for expired, unlimited, same-tariff, free-target (`newMonthly <= 0`) or result < 1 day; result capped at 2 years (730 days). Apply-time check: reject if recomputed days are more than 1 day below the quoted `exchange_days`.
- Zero-price exchange order uses the balance method and runs immediately; on panel failure the order ends `failed`, nothing charged.
- Storefront "update existing": non-unlimited, non-expired base subscriptions whose tariff differs from the chosen one; base tariffs only; the switch still shows with exactly one candidate, preselected to "update".
- Pay-mode downgrade surcharge stays clamped to 0 (`math.Max(tq.Surcharge, 0)`).
- Before every commit that changes bot code: `gofmt -l $(git ls-files '*.go')` empty, `go vet ./...`, `go test ./...`, `golangci-lint run` (separately), and for `web/`: `cd web && bun run typecheck && bun run build`. Commit messages carry no Claude attribution line. Do not push unless asked.

## Review Focus

- Exchange on a subscription with 0 or negative days left, or `expire_at` nil: must return no exchange option, never a negative or huge date.
- Target tariff priced 0, or very cheap (huge result): option hidden for free, result clamped to 730 days.
- Quote goes stale between display and confirm (time passes): apply rejects when shorter by more than 1 day, bot shows a re-quote prompt instead of a generic error.
- Panel older than this change (no `exchange_*` fields): bot/UI show only "pay the difference", nothing breaks.
- Unlimited subscription, `NO_RESET` strategy, or `limit_bytes == 0`: list/details show "forever", "after renewal", "no limit" respectively, never "NaN days" or a 1970 date.
- Device limit 0: shown as unlimited or hidden, never "up to 0".
- Add-on with empty tariff name/description: falls back to add-on name, never a blank card.

---

## Part 1: Panel (`/Users/victor/Projects/Go/vpn-control`)

### Task P1: Exchange quote

**Files:**
- Modify: `internal/billing/pricing.go` (next to `ProrateSurcharge`), `internal/billing/provision.go` (`TariffQuote`, `QuoteTariffChange`), `internal/api/handlers/subscriptions.go` (`QuoteTariff` JSON)
- Test: `internal/billing/pricing_test.go`, `internal/billing/provision` tests following the existing pattern in `traffic_flow_test.go`

**Interfaces:**
- Produces: `func ExchangeDays(oldMonthly, newMonthly int64, now time.Time, expireAt *time.Time) int` (0 means not offered); `TariffQuote` gains `ExchangeDays int` and `ExchangeExpireAt *time.Time`; JSON keys `exchange_days`, `exchange_expire_at` (both omitted/null when not offered).

- [ ] **Step 1: Failing tests in `pricing_test.go`:** `TestExchangeDays` table: (100, 200, 60 days left) → 30; (100, 50, 60) → 120; expired → 0; unlimited (year ≥ 2090) → 0; `newMonthly` 0 → 0; result under 1 day → 0; (100, 1, 300 days left) → 730 (cap).
- [ ] **Step 2: Run** `go test ./internal/billing -run TestExchangeDays` — expect FAIL (undefined).
- [ ] **Step 3: Implement `ExchangeDays`** in `pricing.go` using the formula from Global Constraints; round to whole days, apply the 1-day floor and 730-day cap.
- [ ] **Step 4: Failing quote test:** quoting a subscription change in `provision` tests returns `ExchangeDays == 30` and `ExchangeExpireAt == now+30d` for the 60-day/100→200 case, and none when the item is unlimited or the tariff is unchanged.
- [ ] **Step 5: Implement** the fields in `QuoteTariffChange` (compute only for `KindSubscription`; skip when `t.ID` equals the current tariff) and add them to the `QuoteTariff` handler JSON (`expire_at` style: RFC3339 or null).
- [ ] **Step 6: Run** `go test ./internal/billing ./internal/api/...` — expect PASS; run the panel's own lint/test task (`taskfile.yaml`).
- [ ] **Step 7: Commit** `feat(billing): quote a term exchange for tariff changes`.

### Task P2: Apply exchange in `ChangeTariff`

**Files:**
- Modify: `internal/billing/provision.go` (`ChangeTariffInput`, `ChangeTariff`), `internal/api/handlers/subscriptions.go` (`changeTariffRequest`)
- Test: provision tests (same pattern as P1)

**Interfaces:**
- Consumes: `ExchangeDays` from P1.
- Produces: `ChangeTariffInput` gains `Mode string` (`""`/`"surcharge"` default, `"exchange"`) and `ExpectedDays int`; request JSON keys `mode`, `expected_days`. Exchange errors use `apperr.New("billing.exchange_unavailable", …)` and `apperr.New("billing.exchange_stale", …)`.

- [ ] **Step 1: Failing tests:** (a) exchange mode sets the Remnawave user's `expire_at` to now+30d, charges nothing, records a `tariff_change` extension with surcharge 0; (b) `ExpectedDays` 30 but recomputed 28 → `billing.exchange_stale`, nothing changed; (c) recomputed 29 vs expected 30 passes; (d) exchange on an unlimited subscription → `billing.exchange_unavailable`; (e) default mode behaves exactly as before.
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement** in `ChangeTariff`: when `Mode == "exchange"`, recompute `ExchangeDays` against the current state, validate, and set `cur.expireAt` to the new date before `DesiredParams(...).Apply(&upd)` so one Remnawave update carries limit and expiry; force `Surcharge = 0`; the included-add-ons step is unchanged.
- [ ] **Step 4: Run** panel tests and lint — expect PASS.
- [ ] **Step 5: Commit** `feat(billing): exchange the remaining term when changing tariff`.

---

## Part 2: Bot backend (`/Users/victor/Projects/Go/vpn-control-bot`)

### Task B1: Panel client, profile fields

**Files:**
- Modify: `internal/remnactl/billing.go` (`TariffQuote`, `ChangeTariffInput`), `internal/remnactl/fake.go`, `internal/profile/profile.go` (`Subscription`, `Addon`), `web/src/types.ts`
- Test: `internal/remnactl/client_test.go`, `internal/profile/profile_test.go`

**Interfaces:**
- Produces:
  - `remnactl.TariffQuote` gains `ExchangeDays int \`json:"exchange_days"\``, `ExchangeExpireAt *time.Time \`json:"exchange_expire_at"\``, `Addons []AddonChange \`json:"addons"\`` (fields: `AddonName`, `TariffName`, `Action`, `Credit float64`, matching the panel's JSON).
  - `remnactl.ChangeTariffInput` gains `Mode string \`json:"mode,omitempty"\``, `ExpectedDays int \`json:"expected_days,omitempty"\``.
  - `profile.Subscription` and `profile.Addon` gain `ResetAt *time.Time \`json:"reset_at"\`` and `ResetStrategy string \`json:"reset_strategy"\``; `profile.Addon` gains `TariffID *int \`json:"tariff_id"\``.
  - TS: matching fields on `Subscription` and `Addon` in `web/src/types.ts`.

- [ ] **Step 1: Failing tests:** `client_test.go` decodes a tariff-quote payload with `exchange_days`/`exchange_expire_at`/`addons`; `profile_test.go` asserts `ResetAt`/`ResetStrategy` copied from `RwUser.NextTrafficResetAt`/`TrafficLimitStrategy` for subscription and add-on, and `TariffID` for add-on.
- [ ] **Step 2: Run** `go test ./internal/remnactl ./internal/profile` — FAIL.
- [ ] **Step 3: Implement** the fields; extend the fake client's `QuoteTariff`/`ChangeTariff` (`fake.go:251/264`) to return an exchange quote and to record `Mode`.
- [ ] **Step 4: Run** tests — PASS. **Step 5: Commit** `feat(remnactl): carry exchange quotes and reset dates`.

### Task B2: Orders: exchange mode

**Files:**
- Modify: `internal/orders/order.go` (`Request`, `Quote`), `internal/orders/service.go` (`KindChangeTariff` case at ~`:141-154`), `internal/orders/fulfill.go` (`:306`), `internal/i18n/locales/{ru,en}.json`
- Test: `internal/orders/service_test.go`, `internal/orders/fulfill_test.go` (follow existing tests)

**Interfaces:**
- Consumes: B1 types.
- Produces: `Request` gains `Mode string \`json:"mode,omitempty"\`` (`""`/`"surcharge"`/`"exchange"`) and `ExpectedDays int \`json:"expected_days,omitempty"\``; `Quote` gains `Change *ChangeQuote \`json:"change,omitempty"\`` with `ChangeQuote{Surcharge float64, ExpireAt *time.Time, ExchangeDays int, ExchangeExpireAt *time.Time, Addons []remnactl.AddonChange}` so the UI can show both outcomes from one quote call.

- [ ] **Step 1: Failing tests:** (a) quote for `change_tariff` returns `Change` with both options and `Price == max(surcharge, 0)` in surcharge mode; (b) `Mode: "exchange"` with `ExchangeDays > 0` returns `Price == 0`; (c) `Mode: "exchange"` against a panel quote with `ExchangeDays == 0` → `bad("exchange")` error; (d) fulfil calls `ChangeTariff` with `Mode: "exchange"`, `ExpectedDays: r.ExpectedDays`, `Surcharge: 0`; (e) a panel `billing.exchange_stale` error ends the order `failed` with a retry-able message key.
- [ ] **Step 2: Run** `go test ./internal/orders` — FAIL.
- [ ] **Step 3: Implement** `price()`/quote and fulfil changes; a zero-price exchange order must take the existing balance path (`FromBalance == 0`, `ToPay == 0`) and be fulfilled via `PayFromBalance`.
- [ ] **Step 4: Run** `go test ./...` and `golangci-lint run` — PASS.
- [ ] **Step 5: Commit** `feat(orders): change tariff by exchanging the remaining term`.

---

## Part 3: WebApp (`web/`)

No unit-test runner exists in `web/`; each task is verified by `bun run typecheck && bun run build` and a manual pass with the dev server. Add a component only where a task says so.

### Task F1: Shared display pieces

**Files:**
- Create: `web/src/ui/ResetInfo.tsx`, `web/src/ui/AddonGroups.tsx`
- Modify: `web/src/shared/format.ts` (add `daysUntil(date: string | null): number | null`), locales (ru/en `bot.json`)

**Interfaces:**
- Produces:
  - `ResetInfo({ expireAt, resetAt, strategy, limitBytes, unlimited, variant })` where `variant: 'compact' | 'tile'`; renders "in N days" / "after renewal" (strategy `NO_RESET`) / "no limit" (`limitBytes === 0` or unlimited), with the Tabler refresh icon.
  - `RemainingInfo({ expireAt, unlimited, variant })` in the same file or beside it: days left plus end date, or "forever", with the calendar icon.
  - `AddonGroups({ addons, tariffs, subscriptionExpireAt })`: two labelled groups ("Included in your tariff", "Connected separately"); card shows name from the add-on tariff's name + description (found via `addon.tariff_id` in `tariffs`), falling back to `addon.name`; status badge, end date, price (separate only), own traffic bar and reset info, flag when a separate add-on ends before the subscription, "Renew add-on" button (`/checkout?kind=renew&item_kind=addon&item_id=…`), "Connect an add-on" entry (to `/tariffs`).

- [ ] **Step 1:** Add i18n keys (ru + en) for all new strings.
- [ ] **Step 2:** Implement the three components and `daysUntil` (returns null for null/unlimited; never negative text: expired shows the existing expired wording).
- [ ] **Step 3:** `cd web && bun run typecheck && bun run build` — expect success.
- [ ] **Step 4: Commit** `feat(web): reset and remaining info, grouped add-on cards`.

### Task F2: Subscription list and details page

**Files:**
- Modify: `web/src/ui/SubscriptionRow.tsx`, `web/src/pages/SubscriptionDetails.tsx`, `web/src/pages/Subscriptions.tsx` (pass nothing new unless needed)

**Interfaces:**
- Consumes: `ResetInfo`, `RemainingInfo`, `AddonGroups` from F1; `useTariffs()` for add-on names.

- [ ] **Step 1: List row** per the approved mockup (spec section "Subscription list"): title + `StateBadge`, two facts (Remaining, Limit reset), line "used / limit" and "tariff · N add-ons", thin traffic `Progress` (amber ≥ 80%, red at 100%, omitted when unlimited).
- [ ] **Step 2: Details page** layout B: header, two tiles (Remaining, Limit reset), traffic/devices/tariff card (device limit `0` hidden, `null` hidden), "Renew" (primary), "Change tariff" (secondary, Tabler icon), connection link, `AddonGroups`. Unlimited subscriptions: "forever" tile, no buttons, short explanatory line.
- [ ] **Step 3:** `bun run typecheck && bun run build`; open the dev server and check an active, an expiring, an expired and an unlimited subscription.
- [ ] **Step 4: Commit** `feat(web): redesigned subscription list and details`.

### Task F3: Change-tariff checkout with mode choice

**Files:**
- Modify: `web/src/pages/Checkout.tsx` (`change_tariff` branch ~`:124`, `:285-293`), `web/src/types.ts` (`OrderRequest`: `mode?: 'surcharge' | 'exchange'`, `expected_days?: number`; `OrderQuote.change`), locales

**Interfaces:**
- Consumes: B2 `Quote.change`.

- [ ] **Step 1:** In `change_tariff`, request the quote without a mode, then show two selectable options built from `quote.change`: "Pay {surcharge}, term stays until {expire_at}" and "Pay nothing, term becomes until {exchange_expire_at}"; hide the exchange option when `exchange_days` is absent or 0. Default selection: surcharge. Selecting exchange sets `mode: 'exchange'` and `expected_days: change.exchange_days` on the request used for create/pay.
- [ ] **Step 2:** Show old and new tariff reset info and the included-add-on changes from `change.addons` ("will become included" / "will be removed"), using the grouped names.
- [ ] **Step 3:** When the order fails with the stale-quote message key, refetch the quote and show the re-confirm hint.
- [ ] **Step 4:** `bun run typecheck && bun run build`; manual pass against a panel with P1/P2 and against a mocked old-panel quote (no exchange fields).
- [ ] **Step 5: Commit** `feat(web): choose surcharge or term exchange when changing tariff`.

### Task F4: Storefront "new / update existing"

**Files:**
- Modify: `web/src/pages/Tariffs.tsx` (CTA at ~`:94`), possibly `web/src/pages/Checkout.tsx` for the preselected tariff

**Interfaces:**
- Produces: for a base tariff, when `eligible(profile.subscriptions, tariff)` is non-empty (rules in Global Constraints; put the predicate in `web/src/renew.ts` as `changeCandidates(subs, tariffId)`), the CTA opens a choice (Mantine `Modal` or `SegmentedControl` + subscription `Select`): "New subscription" → `/checkout?kind=subscribe&tariff_id=…` (existing), "Update existing" (preselected) → `/checkout?kind=change_tariff&item_kind=subscription&item_id=<picked>&tariff_id=<tariff>`. Add-on tariffs keep the current CTA.

- [ ] **Step 1:** Implement `changeCandidates` and the choice UI; with several candidates show a subscription picker listing title and tariff.
- [ ] **Step 2:** Make `Checkout` accept `tariff_id` for `change_tariff` as the preselected target (today it is picked on the page).
- [ ] **Step 3:** `bun run typecheck && bun run build`; manual pass: no subscriptions (old CTA), one candidate, several, only unlimited/expired ones (old CTA).
- [ ] **Step 4: Commit** `feat(web): update an existing subscription from the storefront`.

### Task F5: Final checks

- [ ] Run the full pre-push checklist from `CLAUDE.md` in the bot repo (gofmt, vet, test, `golangci-lint run`, `bun run typecheck && bun run build`) and the panel's checks in its repo; fix anything red.
- [ ] Manual end-to-end against a dev panel: pay-mode change, exchange-mode change (expiry moves to the quoted date), stale-quote rejection, old-panel fallback.
- [ ] Do not push; report results and wait for the owner.
