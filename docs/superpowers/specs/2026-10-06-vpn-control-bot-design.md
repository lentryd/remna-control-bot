# vpn-control-bot — design

Date: 2026-10-06

## Goal

Telegram bot + Telegram WebApp for customers of the vpn-control panel
(`../vpn-control`). In this iteration: access/onboarding (including referral access)
and viewing the customer's current information. Payments and checkout are only
stubbed out behind interfaces.

## Requirements

- Go, a single process and a single docker container (bot + WebApp HTTP server).
  In the first commits there are no releases: everything is built from Dockerfile/compose files.
- Two modes, `BOT_MODE`:
  - `open` — anyone can start the bot; a new user automatically becomes a Customer.
  - `closed` — access only for: a customer with `telegram_id` set in the panel;
    someone who sent their subscription link (found in the panel); someone who came via a referral link.
- Features: subscriptions and their add-ons, balance and transactions, invitees and how much
  each one brings in, available tariffs.
- Secondary/inconvenient features go to the WebApp; the bot has deep-link buttons to the needed section.
- The WebApp lives at `<panel domain>` + `WEBAPP_PATH` (default `/bot`).
- Local development on a local domain via traefik + mkcert, Telegram opens the WebApp
  at a URL like `https://local.<domain>/bot` (the space-rocket-next approach: hosts + trusted certificate).
- Compose files are named `compose.yml` / `compose.dev.yml` (the current standard, without the `docker-` prefix).

## Decisions

- **Source of truth is vpn-control.** The bot does not read the panel DB; it works only through `/api/v1`
  with a service token (the "bot" preset of scopes). A new user is created as a Customer immediately.
- The bot has its own SQLite only for service data: dialog state (waiting for a subscription link).
- vpn-control is not modified, except for the "bot" scope preset in the token form.

## Architecture

```
cmd/bot/main.go         config → store → vpnctl → access → telegram + webapp; graceful shutdown
internal/config         ENV
internal/vpnctl         typed client for /api/v1 (interface Client, fake for tests)
internal/access         access policy and onboarding (pure logic on top of vpnctl.Client)
internal/refcode        customer_id ⇄ short referral code (see "Referral code")
internal/sublink        parsing short_uuid out of a subscription link
internal/telegram       go-telegram/bot: router, handlers, keyboards, texts
internal/webapp         Fiber: initData validation, JSON API for the frontend, static files under WEBAPP_PATH
internal/store          SQLite (modernc, no CGO): dialog state (waiting for a subscription link)
internal/profile        customer view model (shared by bot screens and WebApp API)
internal/billing        interface Billing{Quote, Pay} + NotImplemented implementation
web/                    WebApp frontend: the vpn-control/web stack (Bun, React, Mantine), hash routing
```

### Configuration (ENV)

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BOT_TOKEN` | yes | | Telegram bot token |
| `BOT_MODE` | no | `closed` | `open` / `closed` |
| `VPNCTL_URL` | yes | | base URL of the panel, e.g. `http://vpn-control:8080` |
| `VPNCTL_TOKEN` | yes | | API token created with the "bot" preset |
| `PUBLIC_URL` | yes | | public origin, e.g. `https://vpn.example.com` |
| `WEBAPP_PATH` | no | `/bot` | WebApp path prefix |
| `HTTP_ADDR` | no | `:8090` | listen address |
| `DB_PATH` | no | `/data/bot.db` | bot's SQLite |
| `LOG_LEVEL` | no | `info` | slog |
| `REF_SECRET` | yes | | secret for referral codes (changing it invalidates all issued links) |

WebApp URL = `PUBLIC_URL + WEBAPP_PATH`. The bot username is taken from `getMe` for referral links.

## Access and onboarding

`/start [payload]`:

1. `lookup?telegram_id=<id>` found the customer (not archived) → main menu.
2. Not found, payload `ref_<code>` → `refcode.Decode(code)` → `customer_id` → `GET /api/v1/customers/{id}`;
   if the referrer exists and is not archived —
   `POST /api/v1/customers` with `telegram_id`, `referrer_id`, name from Telegram → menu. Works in both modes.
   Invalid code: in `open` — create without a referrer; in `closed` — "link is invalid".
3. Not found, no payload:
   - `open` → create a Customer → welcome screen + tariffs.
   - `closed` → "Send your subscription link". The session waits for text.
4. Text while waiting for a link → `sublink.Parse` (`short_uuid` = last path segment of the URL
   or the bare uuid) → `lookup?short_uuid=` →
   - found, `telegram_id` empty → GET + `PUT /api/v1/customers/{id}` with `telegram_id` → menu;
   - found, `telegram_id` is a different one → refusal ("the subscription is already linked to another account, contact support");
   - not found → "subscription not found", stay waiting.

Every update (except /start and link input) goes through a middleware that resolves the customer
by `telegram_id`; no customer → the onboarding screen. Archived customer → "access suspended".

## Bot UX

One "main" message that is edited in place (`editMessageText`); the bot only sends new messages
in response to text input. Callback data is short: `m:home`, `m:subs`, `s:<id>`, `m:bal`, `m:ref`, `m:tar`.

- **Home**: name, balance, the nearest expiration with status (🟢 active / 🟡 < 3 days / 🔴 expired).
  Buttons: 📦 Subscriptions · 💰 Balance · 👥 Referrals · 🏷 Tariffs · 🌐 Open app (WebApp).
- **Subscriptions**: list (label/tariff, until when) → card: tariff, until, traffic/HWID limits,
  add-ons with their dates, subscription link (copied in `<code>`), "Details in the app" (`#/subscriptions/<id>`).
- **Balance**: current balance, last 5 operations, "Full history" (`#/balance`).
- **Referrals**: personal link `t.me/<bot>?start=ref_<code>`, "Share" button (`switch_inline_query`/share URL),
  number of invitees, total accruals, top 5 by income, "Everything in the app" (`#/referrals`).
- **Tariffs**: list with prices; the "Pay/Pick" button → `billing` → "payment will be available soon".
- Commands: `/start`, `/menu`, `/help`. The menu button (`setChatMenuButton`) opens the WebApp.

## WebApp

- Fiber, all routes under `WEBAPP_PATH`: `GET {path}/` — SPA static files (embed), `GET {path}/api/*` — JSON.
- Auth: the header `Authorization: tma <initData>`; HMAC-SHA256 validation with the key
  `HMAC("WebAppData", BOT_TOKEN)`, `auth_date` no older than 24h → telegram_id → customer.
  The panel token never reaches the frontend.
- API: `GET /api/me` — the customer's aggregated profile (`internal/profile`: balance, subscriptions with add-ons,
  ledger, invitees with accrual amounts, referral link), `GET /api/tariffs` — active tariffs.
  The data is built only from the customer resolved by initData, so someone else's objects are unreachable;
  admin fields (`notes`, `contact`, `referral_percent`) are not exposed.
- Screens: Overview, Subscriptions (+details with add-ons), Balance/history, Referrals, Tariffs.

### Frontend: stack and design

Main criterion: beautiful and clear to the user. Visually it is part of the vpn-control family.

- Stack is the same as in `vpn-control/web`: Bun (build via `scripts/build.ts`), React 19, TypeScript,
  Mantine 9 (+ postcss-preset-mantine), TanStack Query, react-router (HashRouter), i18next,
  `@tabler/icons-react`, Geist fonts, dayjs. Versions are pinned the same as in the panel.
- Reused from the panel by **copying into `web/src/shared`** (not a cross-repo import: the
  docker build context of the bot must be self-contained): Mantine theme/global.css/vendor.css,
  `shared/ui` (`section-card`, `stat-strip`, `info-field`, `copyable-field`, `copyable-code-block`,
  `shimmer-skeleton`, `loading-screen`, `page-header`), `components/badges`, `format.ts`, `pricing.ts`,
  `SubscriptionCard` and `referral-flow` as a base for the customer screens (adapted: read-only, no admin actions).
  Copied files are marked with a source comment.
- Mobile-first for the Telegram WebView: single column, large touch targets, bottom navigation
  (Overview · Subscriptions · Balance · Referrals), skeletons instead of spinners, empty states with a hint
  of what to do ("No subscriptions — pick a tariff").
- Integration with Telegram: `colorScheme` and accent colors from `themeParams` (light/dark follows
  the client), `BackButton` for navigation back, `HapticFeedback` on copy, `expand()` on startup,
  the subscription link copied with one tap.
- Texts via i18next, `ru` locale (key structure ready for `en`).

## Referral code

Computed in the bot and not stored anywhere; vpn-control is not modified for it.

- `code = base62(id XOR k) + base62(HMAC-SHA256(REF_SECRET, id))[:4]`, where `k` is derived from
  `REF_SECRET` (masks the sequential id). Result: ~6–9 characters, `[0-9A-Za-z]` (valid for `start`).
- `Decode`: split off the last 4 characters, recover `id`, recompute the HMAC and compare in constant time;
  mismatch → invalid code. Enumeration requires guessing the HMAC (62⁴ ≈ 14.7M per id).
- The code is unique because it is a bijection on `id`. Tests: round-trip, tamper, wrong secret.

## vpn-control

The panel's public API (`/api/v1`, by token) already covers everything required; no backend changes.
Used scopes:

| Scope | Purpose in the bot |
|---|---|
| `customers:lookup` | by `telegram_id` / `short_uuid` |
| `customers:get` | customer card: subscriptions, add-ons, balance, ledger, `subscription_url` |
| `customers:create` | new customer with `telegram_id`, `referrer_id` |
| `customers:update` | link `telegram_id`. **Full PUT**: missing fields are cleared, so the bot always does GET → modifies `telegram_id` → PUT of the whole object |
| `tariffs:list` | tariff catalog |

Invitees and accruals are already in `customers:get`: `referrals` — direct invitees with
`total_paid`/`monthly`, `accruals` — accruals where the customer is the referrer or referee
(the bot takes `referrer_id == id` and groups them by `referee_id`).

The only change: a **"bot" preset** in the API token creation form
(`web/src/components/api-tokens/modals/create-api-token-modal.tsx`, next to `SUBPAGE_PRESET_KEYS`)
with the keys listed above. Payments later add `items:quote|extend`, `customers:payment_preview|pay`.

## Payments (groundwork)

`internal/billing.Billing` with methods `Quote(ctx, customerID, item) ` and `Pay(...)`; the current implementation
returns `ErrNotImplemented`, handlers show "coming soon". Later the implementation goes through
`items/*/quote|extend` and `payments` of the panel.

## Local development

`compose.dev.yml`:
- `traefik` (v3): ports 80/443, file provider with mkcert certificates from `./dev/certs`, http→https redirect.
- `vpn-control`: image `ghcr.io/lentryd/vpn-control:latest` (the panel repo has no Dockerfile; the image comes from its release workflow), host `${LOCAL_DOMAIN}` (required in `.env`, no default).
- `bot`: `build: .`, rule `Host(${LOCAL_DOMAIN}) && PathPrefix(${WEBAPP_PATH})`.

Like in space-rocket-next: `LOCAL_DOMAIN` is a subdomain of your own domain (e.g. `local.<domain>`),
entry `127.0.0.1 local.<domain>` in `/etc/hosts`, certificate via `mkcert` (CA installed with `mkcert -install`)
— Telegram Desktop on the same machine opens the WebApp at that URL.
`taskfile.yaml` (as in vpn-control): `certs` (mkcert), `hosts` (check), `panel:db` (local panel DB snapshot), `up`, `down`, `logs`, `test`.

## Production

`Dockerfile` multi-stage: `oven/bun` (build web) → `golang` (build, embed web/dist) → `distroless/static`.
`compose.yml` example: the bot service next to the panel, volume `/data`; routing by path `/bot`
is configured on the external balancer.

## Errors

- Panel unavailable/5xx → "Service temporarily unavailable, try again later" + log; retry with no automatic repeats.
- 404 on lookup → onboarding scenario; 409 when linking → message about another account.
- Expired/invalid initData → 401, the frontend shows "Open the app from the bot".

## Testing

- `access`: table-driven tests over a fake `vpnctl.Client` covering all onboarding branches × both modes.
- `sublink`: URL formats/bare uuid/garbage.
- `webapp`: initData validation (valid/tampered/expired), ownership checks.
- `vpnctl`: `httptest` server, DTO mapping and errors.
- `telegram`: rendering texts/keyboards from fixtures (golden), without the network.

## Addendum 2026-10-07: balance pay mode and notifications

- `BALANCE_PAY_MODE` (`mixed` | `referral_full`): `billing.Split` decides how a renewal is covered —
  mixed spends referral money first, then own funds; referral_full spends referral money only when it
  covers the whole price, otherwise own funds only. Every subscription in the profile carries `renewal`
  (a month of the subscription with its paid add-ons) shown by the bot and the WebApp.
- Notifications (`internal/notify`): every `NOTIFY_INTERVAL` the bot reads `customers:list`,
  `subscriptions:list` and `tariffs:list`, plans notices and sends each once (keys in the bot's SQLite):
  traffic ≥ `TRAFFIC_ALERT_PCT` — once per reset period, with the reset date and the cheapest active paid
  tariff of the same kind with more traffic; payment reminders at `REMIND_DAYS` before the end and once
  within 3 days after it. Only non-archived customers with a Telegram id.
- Tariffs are picked only in the WebApp; free (0 ₽) tariffs are not offered.

## Out of scope for this iteration

Payment and checkout, notifications/reminders, admin commands in the bot, multi-language support (Russian texts only, collected in one place).
