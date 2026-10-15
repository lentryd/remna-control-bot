# Admin web app and DB-backed settings — design

Date: 2026-10-09. Status: approved in brainstorming. Supersedes the admin-in-Telegram parts of
`2026-10-07-payments-manual-design.md`. No backward compatibility: the project is still in development.

## Goal

Admins stop working inside Telegram chats. Telegram becomes a notification channel only: a new order
arrives as a direct message with a button that opens the admin part of the bot's Mini App. Order decisions
(approve, reject, retry, edit amount) and all operational settings (payment methods, credentials, timings)
move to that admin part and are stored in the database. The environment keeps only what is needed to start
the process and to identify admins.

## Decisions

- **Notifications go to each admin in `ADMIN_IDS` as a private message.** `ADMIN_CHAT_ID` is removed. The
  button is always a `web_app` button (it works in private chats), so no chat-type handling is needed.
- **Admin access is checked on every request:** valid Telegram `initData` (existing `initdata.Validate`)
  and `telegramID ∈ ADMIN_IDS`. No panel customer lookup is required for admin routes.
- **Settings live in SQLite**, one JSON document per section in `settings(key, value, updated_at)`. Secret
  fields are encrypted (AES-GCM) with a key derived from `REF_SECRET` via HKDF; no new env variable.
  Rotating `REF_SECRET` makes stored secrets unreadable and they must be re-entered (stated in the UI).
- **Settings apply without a restart:** the service keeps an immutable in-memory snapshot, replaced on save,
  and notifies subscribers.
- **Telegram callbacks for admin actions and the reply-prompt flow are deleted**, not kept in parallel.

## Environment after the change

Kept: `ADMIN_IDS`, `BOT_TOKEN`, `REMNACTL_URL`, `REMNACTL_TOKEN`, `PUBLIC_URL`, `WEBAPP_PATH`, `HTTP_ADDR`,
`DB_PATH`, `REF_SECRET`, `LOG_LEVEL` (plus dev-only `LOCAL_DOMAIN`).

Removed from env, moved to settings: `ADMIN_CHAT_ID`, `BOT_MODE`, `BASE_CURRENCY`, `BALANCE_PAY_MODE`,
`NOTIFY_INTERVAL`, `TRAFFIC_ALERT_PCT`, `REMIND_DAYS`, `ORDER_TTL`, `MANUAL_PAY_DETAILS`, every
`YOOKASSA_*`, `CRYPTOBOT_*`, `HELEKET_*`, `PAL24_*`, `PLATEGA_*`, `STARS_RATE`.

`.env.example` shrinks to the kept variables. `internal/config` keeps only them; `PayConfig` and the
`*On()` predicates are replaced by reads from the settings snapshot.

First start: the database is empty, so every payment method (manual included) is off, `BOT_MODE` is
`closed`, other values take the defaults currently in `config.go`. On start, if no payment method is
configured, the bot DMs each admin a reminder with a button to the admin settings.

## Settings (`internal/settings`)

- Sections: `general` (bot mode, base currency, balance pay mode, notify interval, traffic alert %, remind
  days, order TTL), `pay.manual` (enabled, details text), `pay.yookassa`, `pay.cryptobot`, `pay.heleket`,
  `pay.pal24`, `pay.platega`, `pay.stars`. Field sets mirror the current env variables one to one.
- Each section is a typed struct with `Validate()`. Paired credentials (shop id + secret, merchant id + key)
  are both set or both empty, as in current `PayConfig`. An enabled section must pass validation.
- Store API: `Get(section)`, `Put(section, value)` (validate, encrypt secrets, write, swap snapshot, fire
  `OnChange`), `Snapshot()`. Reads never touch the database after startup.
- Secret fields are marked in the struct; the API returns them masked (`••••1234`). A `PUT` that sends the
  mask or an empty secret keeps the stored value; sending a new value replaces it.
- A confirmed base currency still requires either a panel answer or an explicit `general.base_currency`, as
  today (`svc.CurrencyKnown`).

## Admin API (`internal/webapp`)

All under `{WEBAPP_PATH}/api/admin/*`, behind an admin middleware (initData, then `ADMIN_IDS`, otherwise 403).
`/me` gains `isAdmin` so the SPA can show the admin entry point.

- `GET /admin/orders?status=&limit=&offset=`, `GET /admin/orders/:id`.
- `POST /admin/orders/:id/approve` — body `decision` (`full|debt|credit`), optional `amount`.
- `POST /admin/orders/:id/reject` — body `reason` (optional).
- `POST /admin/orders/:id/retry`.
- `GET /admin/orders/:id/receipt` — streams the receipt through the bot; Telegram `file_id` and bot token
  never reach the browser.
- `GET /admin/settings`, `PUT /admin/settings/:section`, `POST /admin/settings/:section/test`.

Handlers are thin wrappers over `orders.Service.Approve/Reject/Retry/SetReceived`; the admin string passed
is the Telegram id. A second decision on an already-resolved order returns 409 from the existing
status-guarded `move`.

## Notifications (`internal/telegram`, `internal/orders`)

- `Notifier.PostCard` sends a short card to every admin: order kind, customer, amount, method and one
  `web_app` button opening `/admin/orders/:id`. Receipts are viewed in the web UI, not attached.
- New table `order_notices(order_id, admin_id, msg_id)` replaces the single `orders.admin_msg_id`.
- `UpdateCard` edits every stored card when the order changes state ("approved by …", "rejected by …");
  resolved cards keep only the open button.
- Send failures (typically 403 for an admin who never started the bot) are logged and surfaced as a warning
  in the admin Orders screen ("admin X is not receiving notifications"). They never fail order creation.
- Deleted: callbacks `oa od oc oe ox on oy`, `onAdmin`, `onAdminReply`, table `admin_prompts` and its
  `Store` methods, the admin-chat gate in `private.go`, admin keyboards in `view/orders.go`.

## Hot reload

- Provider construction moves from `main.go` into `payments.Build(snapshot)`. `payments.Registry` becomes
  safe for concurrent replacement (`RWMutex` + `Swap`), and `orders.Service` reads providers through it on
  each call.
- A subscriber on `OnChange` rebuilds the registry. The webhook route `/pay/:provider` stays static and
  resolves the provider from the current registry, so the router is never rebuilt.
- Background loops (`providerLoop`, `notify.Runner`, `currencyLoop`, `ExpireStale`) read intervals and TTL from
  the snapshot on every tick.
- `POST /admin/settings/:section/test` performs a harmless authenticated call where the provider offers one
  (balance, method list). Where none exists the response says "no check available" rather than reporting success.

## Web UI (`web/src`)

New route group `/admin`, shown only when `/me.isAdmin`. Screens: **Orders** (filter by status, notification
warnings), **Order** (details, receipt, approve with decision and amount, reject with reason, retry),
**Payment methods** (one section per provider with enable switch, masked secrets, Test and Save, the webhook
URL `PUBLIC_URL/pay/<provider>` shown for copying), **General** (the `general` fields). Strings go through the
existing `i18n.ts`.

## Testing

- settings: encrypt/decrypt round trip, masking and keep-on-mask update, pair validation, snapshot swap and
  `OnChange`, wrong key yields an explicit error.
- admin middleware: admin, non-admin, expired/invalid `initData`.
- orders: two admins resolving the same order (second gets 409), card fan-out with one failing admin,
  `UpdateCard` on every stored card.
- hot reload: enabling/disabling a provider changes `Registry` and webhook resolution without restart;
  loops pick up new intervals.
- webapp handlers over a fake `orders.Service`, receipt streaming never exposes `file_id`.

## Delivery order

Each step leaves the bot working.

1. `internal/settings` and its table; move `general` values and loops onto the snapshot.
2. Payment sections and `payments.Build` with hot-swappable registry; remove `PayConfig`.
3. Admin middleware and admin API.
4. Admin screens in the SPA.
5. DM notifications with `order_notices`; delete Telegram admin callbacks, reply prompts, `admin_prompts`,
   `ADMIN_CHAT_ID`; trim `.env.example` and docs.

## Out of scope

Roles or per-admin permissions, an audit log beyond the existing `orders.admin` field, a settings change
history, and login to the admin part outside Telegram.

## Implementation notes

Where the implementation differs from or extends the text above:

- The orders API paginates with `page=` (20 per page) instead of limit/offset.
- Admin UI strings are Russian-only in `web/src/pages/admin/text.ts`, not in the i18n locale files.
- The registry is swapped through `orders.Live` (`SetLive`) rather than a `Registry` with an RWMutex and `Swap`.
- A switched-off provider with complete credentials is "retired": it still resolves for webhooks and polling but never
  issues new invoices.
- Extras: `GET /api/admin/me` and the `/admin` bot command.
- `ADMIN_IDS` is required: the bot refuses to start without it.
