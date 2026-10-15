<div align="center">

<img src="web/public/favicon.svg" width="72" alt="">

# Remna Control Bot

**A Telegram bot and WebApp for the customers of [Remna Control](https://github.com/lentryd/remna-control)**

Subscriptions and add-ons, balance and history, referrals and tariffs, and payments (Telegram Stars, YooKassa, CryptoBot, Heleket, PayPalych, Platega or manual transfer) in one Mini App.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](web/package.json)
[![Mantine](https://img.shields.io/badge/Mantine-9-339AF0?logo=mantine&logoColor=white)](web/package.json)
[![SQLite](https://img.shields.io/badge/SQLite-no%20CGO-003B57?logo=sqlite&logoColor=white)](internal/store)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/lentryd/remna-control-bot)](https://github.com/lentryd/remna-control-bot/releases)
[![Test](https://github.com/lentryd/remna-control-bot/actions/workflows/test.yaml/badge.svg)](https://github.com/lentryd/remna-control-bot/actions/workflows/test.yaml)

**English** · [Русский](README.ru.md)

</div>

## Why

Customers of a VPN service should not have to write to support to learn when their subscription ends or to pay for it. The bot is a launcher: every button of its menu (Subscriptions, Balance, Friends, Tariffs, Payments, Renew) opens the WebApp on the matching page. What stays in the bot is onboarding (`/start`, entering a subscription link), receipts for manual payments, Telegram Stars, notifications and the order cards of the admins.

One Go process: the bot (long polling) plus an HTTP server for the WebApp under `WEBAPP_PATH`. All customer data comes from the remna-control public API; the bot's own SQLite only remembers who is expected to send a subscription link, orders and settings.

## Features

- **Subscriptions and add-ons.** Status, term and traffic from the panel, with the tariffs available for renewal.
- **Balance and history.** Top-ups and payments, applied exactly once.
- **Referrals.** Invite links and what each invitee brings in.
- **Payments.** Telegram Stars, YooKassa, CryptoBot, Heleket, PayPalych, Platega, manual transfer with a receipt, and the balance. See [Payment providers](#payment-providers).
- **Admin app.** Orders, payment methods and general settings, configured from the WebApp without restarts. See [Admin](#admin).
- **Notifications.** Traffic alerts and payment reminders.
- **English and Russian UI,** following Telegram's language. See [docs/i18n.md](docs/i18n.md).

## Deployment

Put `compose.yml` and `.env` next to the panel and run `docker compose up -d --build`. The container joins
the panel's docker network; its Traefik labels route `Host(APP_DOMAIN)` with `PathPrefix(WEBAPP_PATH)` or `PathPrefix(/pay/)` (provider webhooks) to the bot
with a higher priority than the panel's router. With another balancer, route both paths to port 8090.

## Access modes

The mode is a setting in the admin web app (see [Admin](#admin)); the default is `closed`.

- `open` — anyone can start the bot; a new user becomes a remna-control customer.
- `closed` — only customers whose `telegram_id` is set in the panel, users who send the link
  to their subscription (the bot links their Telegram id), and users who come via a referral link.

Referral links look like `https://t.me/<bot>?start=ref_<code>`; the code is derived from the customer id
and `REF_SECRET`, nothing is stored.

## Configuration

Environment variables hold only what the process needs to start. Everything operational (bot mode, payment
methods and their credentials, notifications) lives in the admin web app and is stored in the bot's SQLite.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BOT_TOKEN` | yes | | Telegram bot token |
| `REMNACTL_URL` | yes | | remna-control base URL, e.g. `http://vpn-control:8080` |
| `REMNACTL_TOKEN` | yes | | remna-control API token (see below) |
| `PUBLIC_URL` | yes | | public origin, e.g. `https://control.example.com`; provider webhooks go to `PUBLIC_URL/pay/<provider>`, so it must be https and reachable |
| `WEBAPP_PATH` | no | `/bot` | WebApp path; WebApp URL = `PUBLIC_URL + WEBAPP_PATH` |
| `REF_SECRET` | yes | | referral code secret; also the source of the key that encrypts stored payment credentials. Changing it invalidates every issued link and the credentials must be entered again |
| `ADMIN_IDS` | yes | | comma-separated Telegram ids of the admins (e.g. `12345,67890`) |
| `HTTP_ADDR` | no | `:8090` | listen address |
| `DB_PATH` | no | `/data/bot.db` | bot's SQLite (customers' link expectations, orders, settings) |
| `LOG_LEVEL` | no | `info` | `debug` / `info` / `warn` / `error` |

**Panel token:** remna-control → Settings → API tokens → preset **Telegram bot**
(`customers:lookup`, `customers:get`, `customers:create`, `customers:update`, `customers:list`,
`subscriptions:list`, `tariffs:list`; the two lists are for notifications, plus the payment scopes and
`stats:settings`, read only, which gives the base currency).
Create the remna-control token with the *bot* preset (it includes the payment scopes) — re-issue an older token.
A token issued before `stats:settings` was added to the preset must be re-issued too; until then the bot uses the
base currency set in the admin app (if any).

## Admin

Admins are the Telegram ids in `ADMIN_IDS`. Admin rights are checked on every request.

- **First start:** every payment method is off, and the general settings have defaults: mode `closed`, balance
  payments `mixed`, notification check every 60 minutes, traffic alert at 80%, payment reminders 3 and 1 days
  before the end, unpaid orders expire after 48 hours, base currency taken from the panel. If no method is
  enabled at startup, admins get a one-time direct message saying that no payment method is configured.
- **Bot profile:** on the first start, if the bot has no description yet, it fills in its own name, descriptions
  (with a link to this repository), commands, menu button and photo. `/init` does the same again on demand, overwriting
  what was set in @BotFather, and replies with what failed, if anything. `/admin` and `/init` are shown only in admins'
  command menus.
- **Opening the admin app:** send `/admin` to the bot (or press the button on an order card). The app has screens for
  orders, payment methods and general settings.
- **Payment methods:** enable a method (transfer with a receipt, YooKassa, CryptoBot, Heleket, PayPalych, Platega,
  Telegram Stars), enter its credentials and save. The webhook URL to register in the provider's cabinet is shown next
  to each provider. "Проверить" tests the credentials where the provider supports it (CryptoBot, YooKassa). Changes apply
  immediately without a restart; a method that is switched off still finishes invoices already issued.
- **General settings:** bot mode, balance payment mode, base currency fallback, notification interval (`0` turns
  notifications off), traffic alert percentage, reminder days, order TTL.
- **Credentials are encrypted** in the bot's database with a key derived from `REF_SECRET`. If `REF_SECRET` changes,
  stored credentials can no longer be read (the bot logs a warning at startup) and must be entered again.
- **Order cards:** orders that need a decision (a receipt to check, a payment with a wrong amount) arrive as private
  messages to every admin in `ADMIN_IDS`, with a button that opens the order in the admin app. Each admin must press
  `/start` in the bot once, otherwise Telegram does not let the bot write to them (the Orders screen shows who is
  unreachable). Approve or reject in the app; the cards in all admin chats are updated with the decision.

## Payment providers

Customers can pay with **Telegram Stars**, **YooKassa** (cards, SBP), **CryptoBot** and **Heleket** (crypto), **PayPalych** and **Platega** next to
manual transfer and the balance. A provider appears in the checkout when an admin has enabled it and entered its keys. The payment is confirmed
automatically and applied exactly once; a payment that arrives for an expired or cancelled order is still applied, and
a wrong amount goes to the admins as a card.

Invoices are issued in the panel's base currency. A provider that does not support that currency rejects the invoice
and the customer sees "could not create an invoice". Stars are converted with the rate set in the admin app (base-currency units per Star).

Provider invoices need the base currency to be confirmed: either the panel answers (a token with `stats:settings`) or
the base currency is set explicitly in the general settings. Otherwise the bot logs an error and provider payments stay disabled — manual
payment and the balance are unaffected.

Webhooks: the bot serves `POST {PUBLIC_URL}/pay/yookassa`, `/pay/cryptobot`, `/pay/heleket`, `/pay/pal24`, `/pay/platega` (not under `WEBAPP_PATH`) —
route `/pay/*` to the bot's `HTTP_ADDR` in your reverse proxy, and register the URL in the provider's cabinet
(shown next to each provider in the admin app; YooKassa: HTTP notifications for `payment.succeeded`; CryptoBot: Crypto Pay → My Apps → Webhooks; Heleket sends to
the callback URL the bot passes with every invoice; PayPalych: set `{PUBLIC_URL}/pay/pal24` as the **Result URL** in
the PayPalych cabinet — its postback signature does not cover the status, so the postback is only a pre-filter and every
confirmation is checked against the bill (`bill/status`); a postback that carries no `bill_id` is ignored and the payment is
confirmed by the background check within about a minute; Platega: set `{PUBLIC_URL}/pay/platega` as the **callback URL** in the Platega merchant
settings — it authenticates with the `X-MerchantId`/`X-Secret` headers). Stars need no webhook. If a webhook is lost, the bot asks the
provider about waiting invoices every minute.

Smoke test per provider (use the provider's test mode): YooKassa — a test shop and test card; CryptoBot — the testnet option
with @CryptoTestnetBot; Heleket — a test-mode merchant; PayPalych — create a small bill, pay it and check *Оплачен*; Platega — the same with the configured payment method; Stars — pay a small order from a test account. Create a top-up
of the smallest amount, pay it, and check that the order turns *Оплачен*, the panel has a payment with the provider's
name as its method, and a second delivery of the webhook changes nothing.

PayPalych and Platega are written from Bedolaga's reference and have not been run against the live services.

## Languages

The bot and the web app speak English and Russian, following Telegram's language. See [docs/i18n.md](docs/i18n.md) for how to add one.

## Development

Tasks run with [Task](https://taskfile.dev) (`task --list`), like in remna-control.

1. `task init` — creates `.env`, installs WebApp deps and copies `../vpn-control/data/vpn-control.db`
   into `dev/panel-data/` (git-ignored; `task panel:db` refreshes it, `PANEL_DB` overrides the source).
   Fill in `.env`: `BOT_TOKEN`, `REF_SECRET`, `ADMIN_IDS`, `LOCAL_DOMAIN` — a subdomain of your own domain, e.g.
   `local.example.com`. The panel container reads `../vpn-control/.env` (override with `PANEL_ENV`).
2. `task up` — creates an mkcert certificate for `LOCAL_DOMAIN` (installs the mkcert CA), checks
   `/etc/hosts` (add `127.0.0.1 <LOCAL_DOMAIN>` if it asks) and starts traefik, the panel and the bot.
   - `https://<LOCAL_DOMAIN>/` — remna-control, `https://<LOCAL_DOMAIN>/bot/` — the WebApp,
     http://localhost:8088 — traefik dashboard.
3. In the panel create an API token (preset **Telegram bot**, or tick the scopes listed above if the
   panel image is older than the preset), put it into `REMNACTL_TOKEN` and run `task up` again.
4. In @BotFather set the bot's Mini App / domain to `LOCAL_DOMAIN`. Telegram Desktop on the same
   machine trusts the mkcert CA and opens the WebApp from the menu button.

`task logs` follows the bot, `task down` stops the stack, `task test` runs Go tests and the WebApp typecheck.
`task dev` — panel and traefik in Docker, the bot on the host (talks to the panel via `https://<LOCAL_DOMAIN>`); it stops the
bot container, and traefik then routes the WebApp path to the host bot. Nothing needs restarting or deleting while you work:
Go changes rebuild and restart the bot ([air](https://github.com/air-verse/air), `.air.toml`, built with `-tags dev` so it doesn't embed `web/dist`), frontend changes
rebuild `web/dist` (`web/scripts/watch.ts`), which the bot serves from disk (`WEB_DIR`), so just reload the WebApp. `task dev:web:server` is the
standalone browser dev server.

### Commands

Requirements: Go 1.26, [Bun](https://bun.sh), [Task](https://taskfile.dev), Docker; [golangci-lint](https://golangci-lint.run) for `task lint`.

```bash
task init       # .env, frontend deps, local panel DB
task up         # local stack: traefik + panel + bot on https://LOCAL_DOMAIN
task dev        # panel in Docker, bot on the host with hot reload
task build      # binary with the WebApp embedded -> bin/bot
task test       # Go tests, frontend typecheck, locale check
task lint       # tidy, fmt, vet, golangci-lint
task docker     # Docker image remna-control-bot:local
task snapshot   # local goreleaser snapshot build
```

Stack: Go, [go-telegram/bot](https://github.com/go-telegram/bot), Fiber, SQLite without CGO ([modernc.org/sqlite](https://modernc.org/sqlite)); the WebApp is React 19, Mantine 9, react-router, TanStack Query and i18next, built with Bun and embedded into the binary.

### Releases

CI runs on every push and pull request ([`test.yaml`](.github/workflows/test.yaml)): frontend typecheck and build, `gofmt`, `go vet`, tests, golangci-lint and a check of the release config. A tag starts the release ([`release.yaml`](.github/workflows/release.yaml)): goreleaser builds `linux/amd64` and `linux/arm64` binaries, signs the checksums with cosign, attaches SBOMs and publishes the GitHub release, and the Docker image goes to `ghcr.io/lentryd/remna-control-bot`.

```bash
git tag v1.0.0 && git push origin v1.0.0
```

## License

[AGPL-3.0](LICENSE).

Remna Control Bot is an independent project and is not affiliated with the Remnawave team.
