# vpn-control-bot

Telegram bot and Telegram WebApp for customers of [vpn-control](../vpn-control). Customers see their
subscriptions and add-ons, balance and history, invitees and what each one brings in, and the
available tariffs. Payments are not implemented yet (the "Pick" buttons say "coming soon").

One Go process: the bot (long polling) plus an HTTP server for the WebApp under `WEBAPP_PATH`.
All customer data comes from the vpn-control public API; the bot's own SQLite only remembers
who is expected to send a subscription link.

## Access modes

- `BOT_MODE=open` — anyone can start the bot; a new user becomes a vpn-control customer.
- `BOT_MODE=closed` — only customers whose `telegram_id` is set in the panel, users who send the link
  to their subscription (the bot links their Telegram id), and users who come via a referral link.

Referral links look like `https://t.me/<bot>?start=ref_<code>`; the code is derived from the customer id
and `REF_SECRET`, nothing is stored.

## Configuration

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BOT_TOKEN` | yes | | Telegram bot token |
| `BOT_MODE` | no | `closed` | `open` / `closed` |
| `VPNCTL_URL` | yes | | vpn-control base URL, e.g. `http://vpn-control:8080` |
| `VPNCTL_TOKEN` | yes | | vpn-control API token (see below) |
| `PUBLIC_URL` | yes | | public origin, e.g. `https://control.example.com` |
| `WEBAPP_PATH` | no | `/bot` | WebApp path; WebApp URL = `PUBLIC_URL + WEBAPP_PATH` |
| `HTTP_ADDR` | no | `:8090` | listen address |
| `DB_PATH` | no | `/data/bot.db` | bot's SQLite |
| `LOG_LEVEL` | no | `info` | `debug` / `info` / `warn` / `error` |
| `REF_SECRET` | yes | | referral code secret; changing it invalidates every issued link |
| `BALANCE_PAY_MODE` | no | `mixed` | renewals from the balance: `mixed` — own + referral money; `referral_full` — referral money only if it covers the whole price |
| `ADMIN_CHAT_ID` | no | | chat (private or group) that receives payment receipts; add the bot to the group |
| `ADMIN_IDS` | no | | comma-separated Telegram ids allowed to approve/reject (e.g. `12345,67890`) |
| `MANUAL_PAY_DETAILS` | no | | how to pay, shown with the amount; `\n` for line breaks (e.g. `Карта 2200 0000 0000 0000\nСБП +7…`) |
| `ORDER_TTL` | no | `48h` | unpaid orders expire after this |
| `NOTIFY_INTERVAL` | no | `1h` | how often to check for notifications; `0` turns them off |
| `TRAFFIC_ALERT_PCT` | no | `80` | traffic share of the limit that triggers an alert (with the reset date and a tariff with more traffic) |
| `REMIND_DAYS` | no | `3,1` | days before the end to remind about payment (plus one reminder right after it ends) |
| `YOOKASSA_SHOP_ID` | no | | YooKassa shop id; YooKassa turns on when this and the secret key are set |
| `YOOKASSA_SECRET_KEY` | no | | YooKassa secret key |
| `YOOKASSA_RETURN_URL` | no | WebApp URL (`PUBLIC_URL + WEBAPP_PATH + /`) | where the customer lands after paying |
| `YOOKASSA_RECEIPT_EMAIL` | no | | customer e-mail for the fiscal receipt; no receipt when empty |
| `YOOKASSA_VAT_CODE` | no | `1` | VAT code of the receipt item, `1`..`6` |
| `CRYPTOBOT_TOKEN` | no | | Crypto Pay app token; turns CryptoBot on |
| `CRYPTOBOT_TESTNET` | no | `false` | use the CryptoBot testnet (@CryptoTestnetBot) |
| `CRYPTOBOT_ASSETS` | no | | accepted assets, e.g. `USDT,TON` (empty: all) |
| `HELEKET_MERCHANT_ID` | no | | Heleket merchant id; Heleket turns on when this and the API key are set |
| `HELEKET_API_KEY` | no | | Heleket payment API key |
| `HELEKET_TO_CURRENCY` | no | | fixed asset the customer pays in (empty: customer chooses) |
| `HELEKET_NETWORK` | no | | fixed network (empty: customer chooses) |
| `STARS_RUB_RATE` | no | | roubles per Telegram Star; `> 0` turns Stars on |

**Panel token:** vpn-control → Settings → API tokens → preset **Telegram bot**
(`customers:lookup`, `customers:get`, `customers:create`, `customers:update`, `customers:list`,
`subscriptions:list`, `tariffs:list`; the two lists are for notifications, plus the payment scopes).
Manual payment turns on only when `ADMIN_CHAT_ID`, `ADMIN_IDS` and `MANUAL_PAY_DETAILS` are all set.
Create the vpn-control token with the *bot* preset (it now includes payment scopes) — re-issue the existing token.

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

## Local development

Tasks run with [Task](https://taskfile.dev) (`task --list`), like in vpn-control.

1. `task init` — creates `.env`, installs WebApp deps and copies `../vpn-control/data/vpn-control.db`
   into `dev/panel-data/` (git-ignored; `task panel:db` refreshes it, `PANEL_DB` overrides the source).
   Fill in `.env`: `BOT_TOKEN`, `REF_SECRET`, `LOCAL_DOMAIN` — a subdomain of your own domain, e.g.
   `local.example.com`. The panel container reads `../vpn-control/.env` (override with `PANEL_ENV`).
2. `task up` — creates an mkcert certificate for `LOCAL_DOMAIN` (installs the mkcert CA), checks
   `/etc/hosts` (add `127.0.0.1 <LOCAL_DOMAIN>` if it asks) and starts traefik, the panel and the bot.
   - `https://<LOCAL_DOMAIN>/` — vpn-control, `https://<LOCAL_DOMAIN>/bot/` — the WebApp,
     http://localhost:8088 — traefik dashboard.
3. In the panel create an API token (preset **Telegram bot**, or tick the scopes listed above if the
   panel image is older than the preset), put it into `VPNCTL_TOKEN` and run `task up` again.
4. In @BotFather set the bot's Mini App / domain to `LOCAL_DOMAIN`. Telegram Desktop on the same
   machine trusts the mkcert CA and opens the WebApp from the menu button.

`task logs` follows the bot, `task down` stops the stack, `task test` runs Go tests and the WebApp typecheck.
`task dev` — panel and traefik in Docker, the bot on the host (`go run`, talks to the panel via `https://<LOCAL_DOMAIN>`)
plus the WebApp dev server; it stops the bot container, and traefik then routes the WebApp path to the host bot.

## Production

Put `compose.yml` and `.env` next to the panel and run `docker compose up -d --build`. The container joins
the panel's docker network; its Traefik labels route `Host(APP_DOMAIN)` with `PathPrefix(WEBAPP_PATH)` or `PathPrefix(/pay/)` (provider webhooks) to the bot
with a higher priority than the panel's router. With another balancer, route both paths to port 8090.
