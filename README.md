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

**Panel token:** vpn-control → Settings → API tokens → preset **Telegram bot**
(`customers:lookup`, `customers:get`, `customers:create`, `customers:update`, `tariffs:list`).

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
the panel's docker network; its Traefik labels route `Host(APP_DOMAIN) && PathPrefix(WEBAPP_PATH)` to the bot
with a higher priority than the panel's router. With another balancer, route that path to port 8090.
