# Payment providers — design
> **Superseded in part:** admin decisions and all `*_`-prefixed provider/payment env settings moved to the admin web app — see `2026-10-09-admin-webapp-settings-design.md`.

Date: 2026-10-07. Status: approved in brainstorming. Builds on `2026-10-07-payments-manual-design.md` (order core, manual payment).

## Goal

A customer pays any order the bot can sell (renew, new subscription, add-on, tariff change, top-up) through an
online provider — no admin in the loop. The payment is confirmed automatically, the panel records it and
applies the purchase exactly as it does for a manual payment, and the customer is notified.

First iteration: the provider framework plus **Telegram Stars, YooKassa, CryptoBot, Heleket**. The other
providers of the reference project (remnawave-bedolaga-telegram-bot: ~24 more — CloudPayments, Freekassa,
Platega, WATA, Pal24, MulenPay, …) are added later, one adapter file each, with no change to the core.

Reference for protocols: Bedolaga's `app/external/*.py` and `app/services/payment/*.py` (signatures, request
shapes, status names). It is read-only guidance; nothing is copied or run.

## Decisions

- **Providers only confirm that money arrived.** `internal/orders` stays the only code that moves money in the
  panel; adapters never touch `remnactl`.
- **Order method = provider id** (`yookassa`, `cryptobot`, `heleket`, `stars`), next to `manual` and `balance`.
  The panel payment carries `method = <provider id>` and `note = "bot order #<uuid>"` (unchanged).
- **What is paid is `ToPay`** (price minus what the balance covers, as for manual); a top-up pays its amount.
  Prices are fixed at order creation and never recomputed.
- **A provider enabled by its keys:** no keys → the method is not offered. Nothing else switches it on or off.
- **Roubles everywhere.** Invoices are issued in RUB; the provider converts (YooKassa: RUB natively; CryptoBot:
  fiat invoice `RUB`; Heleket: `currency=RUB`, optional `to_currency`). Stars: `stars = ceil(ToPay / STARS_RUB_RATE)`.
- **No fee is added** to the customer's price.
- **Confirmation never trusts the request alone** (see Security): either a signature, or a fresh API read.
- **Late money is never dropped:** a confirmed payment is accepted for orders that expired or were cancelled
  after the invoice was issued.
- **No migrations** (nothing is in production): new columns go into the schema; recreate the dev database.

## Order changes (`internal/orders`, `internal/store`)

- New status `awaiting_payment` ("ждёт оплату"): an invoice is issued and the order waits for the provider.
  (`awaiting_receipt` would show "ждёт квитанцию" in the history for a card payment.) Not final.
- New `orders` columns: `external_id TEXT` (provider's payment/invoice id), `pay_url TEXT`, `pay_amount REAL` and
  `pay_currency TEXT` (what was invoiced: `1500 RUB`, `334 XTR`). The provider is the order's `method`; no extra column
  or index (lookups are by order id).
- `Method` gains the provider ids; `Quote.Methods` lists every enabled provider (and `manual`) when `ToPay > 0`.
- `Service` gets `Providers *payments.Registry`.

### New service methods

```go
// Pay issues an invoice for the customer's own order (new or waiting for a receipt) with the chosen
// provider and moves it to awaiting_payment. Returns the order with PayURL set; repeating it is idempotent.
func (s *Service) Pay(ctx, customerID int, id, provider string) (*Order, error)

// Paid applies a confirmed provider payment. Idempotent: a second call returns ErrStateChanged.
func (s *Service) Paid(ctx, ev payments.Event) (*Order, error)

// SweepProviders asks providers about waiting orders (lost webhooks) and expires stale ones.
func (s *Service) SweepProviders(ctx) error
```

`CreateWith(…, method)` (`Create` passes the default method) with a provider `method` creates the order as `new` and
issues the invoice in one step (`POST /api/orders` takes `method` and returns `pay_url`); if the provider fails the
order is cancelled (no dangling order). The default method is the balance when it covers the price, else manual when
it is on, else an undecided `new` order that the customer pays with `Pay` (bot flow). A balance-covered order
(`ToPay == 0`) keeps the existing balance path.

### `Paid` rules

1. Find the order by the id in the event (`order_id`/payload carries our UUID); a missing order → `ErrNotFound`
   (the HTTP handler answers 200 and logs, so the provider stops retrying; the alert is the log line).
2. The event's provider must equal `order.provider`, `external_id` must match.
3. Accepted from `awaiting_payment`, `expired`, `cancelled` → atomically to `approving`
   (`UPDATE … WHERE id=? AND status IN (…)`; a lost race returns `ErrStateChanged`), `admin = "provider:<id>"`,
   `decision = full`, then the existing `run` (record payment, apply, notify customer). A failure ends in
   `failed` with the existing admin card ("Повторить"), as for balance orders.
4. **Amount check:** `ev.Amount` (in RUB for fiat providers, in stars for Stars) must be ≥ the invoiced amount
   (`pay_amount`). Less → the order goes to `pending_review` with `received` set (RUB) and the admin card shows
   the provider, so the admin can credit/correct it with the existing buttons. More (Heleket `paid_over`) → treated
   as paid in full; the surplus stays with the provider and is logged.
5. A status the provider reports as failed/cancelled/expired (`payment.canceled`, `cancel`, `fail`) does nothing
   to the order except a log line; the sweep expires it by TTL.

### TTL and sweep

`SweepProviders` runs in its own one-minute loop in `cmd/bot/main.go` (started when any provider is enabled;
`notify.Runner`'s interval is an hour, too slow for lost webhooks). For every order in `awaiting_payment` older than
2 minutes:
- providers implementing `Poller` are asked `Status(external_id)`; a paid answer goes through `Paid`; an
  unreachable provider changes nothing (neither applied nor expired);
- an order older than `ORDER_TTL` that is still unpaid at that last look is expired — `SweepProviders` does the
  expiry itself, `ExpireStale` is unchanged and does not touch `awaiting_payment`.

## Provider framework (`internal/payments`)

```go
type Provider interface {
    ID() string                                  // "yookassa"
    Title() string                               // button label shown to the customer
    Invoice(ctx, in InvoiceRequest) (Invoice, error)
}
type InvoiceRequest struct{ OrderID, Title string; Amount float64 /* RUB */; ExpiresIn time.Duration; ReturnURL string }
type Invoice struct{ ExternalID, URL, Amount string /* "1500.00 RUB" */ }

type WebhookProvider interface {                  // YooKassa, CryptoBot, Heleket
    Webhook(ctx, headers http.Header, body []byte) (Event, bool /* relevant */, error)
}
type Poller interface{ Status(ctx, externalID string) (Event, error) }

type Event struct {
    Provider, OrderID, ExternalID string
    Paid     bool    // money arrived
    Amount   float64 // in the invoice's own unit (RUB, XTR)
    Review   bool    // provider flags a wrong amount: admin decides
}

type Registry struct{ … }  // Register(p), Get(id), Enabled() []Provider
```

HTTP: one Fiber route `POST /pay/:provider` (outside the WebApp auth, next to the existing public routes) looks the
provider up, reads the raw body (kept intact for signature checks), calls `Webhook`, and on a relevant event calls
`orders.Paid`. Answers: 200 for handled, duplicate, unknown or irrelevant events (so providers stop retrying);
400 for an unparsable body; 401 for a bad signature; 500 only when applying failed in a way worth retrying
(panel/database down — the order is then still `awaiting_payment`, so a retry or the sweep finishes it).

Stars has no webhook: it implements only `Provider`, and the bot handles the two Telegram updates itself.

## Providers

### YooKassa (`yookassa.go`)
- Invoice: `POST https://api.yookassa.ru/v3/payments`, Basic auth `shop_id:secret_key`, `Idempotence-Key = order uuid`,
  body: `amount{value,"RUB"}`, `capture:true`, `confirmation{type:"redirect", return_url}`, `description`,
  `metadata{order_id}`. If `YOOKASSA_RECEIPT_EMAIL` is set: `receipt{customer{email}, items[{description≤128,
  quantity "1.00", amount, vat_code, payment_mode "full_payment", payment_subject "service"}]}`.
  `URL = confirmation.confirmation_url`, `ExternalID = id`.
- Webhook `payment.succeeded` (others ignored): **the body is never trusted** — the handler fetches
  `GET /v3/payments/{id}` and requires `status == "succeeded"`, `paid == true`, and the amount/currency of that
  response; `metadata.order_id` from the response (not the request) selects the order. An API error → 500 (retry).
  No IP allow-list (brittle behind proxies; the API read is the authority).
- `Poller.Status`: the same `GET`.

### CryptoBot (`cryptobot.go`)
- Base `https://pay.crypt.bot/api` (`https://testnet-pay.crypt.bot/api` with `CRYPTOBOT_TESTNET=true`), header
  `Crypto-Pay-API-Token`.
- Invoice: `POST createInvoice {currency_type:"fiat", fiat:"RUB", amount, accepted_assets?, description,
  payload:<order uuid>, expires_in:<seconds>}`; `URL = bot_invoice_url`, `ExternalID = invoice_id`.
- Webhook: header `crypto-pay-api-signature` = hex HMAC-SHA256 of the **raw body** with key `SHA256(token)`,
  compared in constant time. `update_type == "invoice_paid"` → `Paid`, `OrderID = payload.payload`,
  `Amount = payload.amount` in the fiat currency (check `fiat == RUB`).
- `Poller.Status`: `getInvoices?invoice_ids=<id>` → `status == "paid"`.

### Heleket (`heleket.go`)
- Base `https://api.heleket.com/v1`; headers `merchant: <HELEKET_MERCHANT_ID>`, `sign`.
  `sign = hex(md5(base64(body) + api_key))` where `body` is the compact JSON with `/` escaped as `\/`.
- Invoice: `POST /payment {amount:"1500.00", currency:"RUB", order_id:<uuid>, lifetime, url_callback, to_currency?, network?}`;
  response `{state:0, result:{uuid,url,…}}`; `URL = result.url`, `ExternalID = result.uuid`.
- Webhook: JSON body with `sign` inside. Verification: remove `sign`, re-serialise the rest compactly (key order as
  received — decode into an ordered structure, not a Go map) with `/` escaped, compute `md5(base64(body)+key)`, compare in
  constant time. `status ∈ {paid, paid_over}` → `Paid`; `wrong_amount`, `wrong_amount_waiting` → `Review`;
  `cancel`, `fail`, `system_fail` → ignored.
- `Poller.Status`: `POST /payment/info {uuid}`.

### Telegram Stars (`stars.go`, plus bot handlers)
- Invoice: `createInvoiceLink` with `currency:"XTR"`, empty `provider_token`, one `LabeledPrice{amount: stars}`,
  `payload = <order uuid>`, `title/description` from the order; `ExternalID = ""` until paid, `pay_amount = "<n> XTR"`.
- `pre_checkout_query`: answer OK only if the payload is one of our order ids, the order is `awaiting_payment`
  with `provider = stars`, the sender is the order's Telegram user and `total_amount` equals the invoiced stars;
  otherwise answer with an error message (within Telegram's 10 s).
- `successful_payment`: `Paid` with `Amount = total_amount` (XTR) and `ExternalID = telegram_payment_charge_id`;
  idempotent — a repeated update returns `ErrStateChanged` and is ignored.
- No poller (the event is the only signal).

## Config (`internal/config`)

| Variable | Meaning |
|---|---|
| `YOOKASSA_SHOP_ID`, `YOOKASSA_SECRET_KEY` | enable YooKassa (both required) |
| `YOOKASSA_RETURN_URL` | where the customer lands after paying (default: `PUBLIC_URL` + `WEBAPP_PATH`) |
| `YOOKASSA_RECEIPT_EMAIL`, `YOOKASSA_VAT_CODE` | fiscal receipt customer e-mail (receipt only when set), VAT code (default `1`) |
| `CRYPTOBOT_TOKEN` | enable CryptoBot |
| `CRYPTOBOT_TESTNET`, `CRYPTOBOT_ASSETS` | testnet switch; accepted assets, e.g. `USDT,TON` (default: all) |
| `HELEKET_MERCHANT_ID`, `HELEKET_API_KEY` | enable Heleket (both required) |
| `HELEKET_TO_CURRENCY`, `HELEKET_NETWORK` | optional fixed asset/network |
| `STARS_RUB_RATE` | roubles per star (> 0 enables Stars) |

Webhook URL for providers: `{PUBLIC_URL}/pay/<provider>`. `Config.Providers()` returns the configured set;
`PUBLIC_URL` must be reachable from the internet for the three webhook providers (startup logs a warning when
a provider is enabled with a non-https `PUBLIC_URL`). `.env.example` and the README are updated.

## WebApp and bot

- `POST /api/orders/quote` already returns `methods`; it now also lists provider ids in `methods` and a `titles` map (`{"yookassa": "Карта / СБП"}`) for their labels.
  Checkout shows one button per method; a provider click calls `POST /api/orders {…, method}` and gets
  `{order, pay_url}`; Stars opens `Telegram.WebApp.openInvoice(pay_url)`, the others `openLink(pay_url)`.
- After paying, the customer returns to the app; the history/list shows `awaiting_payment` → result. The bot sends
  its existing decision notice (`Notifier.Customer`) when the order completes.
- The bot's own renew flow (`rp:` callbacks) offers the same methods; a provider choice replies with a message
  carrying a "Оплатить" URL button (Stars: the invoice link).
- `awaiting_payment` orders show "Оплатить" (re-open `pay_url`) and "Отмена" in the history. Cancelling a provider
  order is allowed (money arriving later is still applied, see Decisions).
- Admin card: only for provider orders that need a decision (`Review`/underpaid) or failed to apply. It shows the
  provider and `external_id` instead of the receipt.

## Security

- Webhook authenticity: CryptoBot and Heleket by constant-time signature comparison over the raw body; YooKassa by a
  fresh authenticated API read (the request body only supplies the payment id); Stars by Telegram's own updates plus
  the `pre_checkout_query` checks.
- Our order id is a UUID; the event's order must exist, belong to the same provider and match `external_id`.
- No secret is logged; request/response logging redacts `Authorization`, `sign`, tokens.
- Only `Paid` can move money, and only through the one atomic transition; replay of any webhook is harmless.
- The raw body size is capped (64 KiB); the route does no work for an unconfigured provider (404).
- HTTP clients have timeouts (connect 5 s, total 30 s) so a slow provider cannot hold the order lock.

## Testing

- `payments`: for each provider — invoice request shape and signature/headers against an `httptest` server using
  examples from the provider docs and Bedolaga's code paths (CryptoBot HMAC, Heleket md5 incl. `\/` escaping,
  YooKassa Basic auth + idempotence key); webhook accept / bad signature / irrelevant event / malformed body;
  `Poller.Status` mapping.
- `orders`: `Pay` creates an invoice and moves to `awaiting_payment`; `Paid` applies a renewal exactly once
  (double webhook → one panel payment; the `Fake` counts `Payments`); late payment after `expired`/`cancelled`;
  underpaid → `pending_review`; foreign provider/external id rejected; sweep expires after a last status check
  and picks up a payment whose webhook was lost.
- `store`: new columns round-trip.
- `webapp`: `POST /api/orders` with a provider returns `pay_url`; ownership checks unchanged; `/pay/:provider`
  status codes (200 duplicate/unknown, 401 bad signature, 404 unconfigured).
- `telegram`: `pre_checkout_query` accept/reject cases; `successful_payment` applied once.
- Real provider sandboxes are not available to the author: **live payments are untested** until the operator tries
  each provider with test credentials (YooKassa test shop, CryptoBot testnet, Heleket test mode, Stars). The
  README lists a per-provider smoke test.

## Adding the next provider

An adapter file implementing `Provider` (+ `WebhookProvider`/`Poller`), its config variables, one registration line
in `cmd/bot/main.go`, and its tests. Source of request shapes: Bedolaga `app/services/payment/<name>.py`
and `app/external/<name>.py`. Planned later (each its own small plan): CloudPayments, Freekassa, Kassa AI, Pal24,
Platega, WATA, MulenPay, RioPay, SeverPay, RollyPay, AuraPay, Overpay, Antilopay, Etoplatezhi, Jupiter, Donut,
Lava, CisPay, Cashera, TabPay, ParityPay, Tribute, PayPear. Apple IAP is not applicable (no iOS app).

## Out of scope

Recurring payments and saved cards, refunds, provider fees added to the price, partial payments across several
providers, per-customer provider preferences, a fiscalisation service beyond the optional YooKassa receipt,
admin UI for provider settings (env only).

## Update: base currency

Amounts are no longer fixed to RUB: they are in the panel's base currency, read from `GET /settings`
(`base_currency`), cached for 10 minutes, with `BASE_CURRENCY` (default `RUB`) as the fallback; the bot token needs
the scope `stats:settings`. `InvoiceRequest` carries `Currency`, and each adapter invoices in it. Stars convert with
`STARS_RATE` (base-currency units per Star; replaces `STARS_RUB_RATE`). `received` is recorded only when the event's
currency equals the base currency. A currency a provider does not support is rejected by the provider and the
customer sees the generic invoice error; providers are not hidden by currency.

## Update: PayPalych and Platega

Two more adapters, `Pal24` (PayPalych) and `Platega`. Pal24 postbacks carry an MD5 signature over
`OutSum:InvId:token` (the signature token, defaulting to the API token), but it does not cover `Status`, `bill_id` or
`CurrencyIn`, so it is only a pre-filter: the authority is `bill/status`. A SUCCESS/OVERPAID/UNDERPAID postback with a
`bill_id` is re-read through the bill and the event is built from the API's answer (external id = the bill id asked
about, order id from the bill, falling back to `InvId`); repeated `InvId`/`OutSum`/`SignatureValue`/`Status`/`bill_id`
keys are refused. A postback without `bill_id` is ignored and the one-minute `SweepProviders` loop confirms the payment
through the stored bill id, at most about a minute later. Platega webhooks carry the merchant id and
secret in the `X-MerchantId`/`X-Secret` headers, and the bot re-reads the transaction through the API before trusting
the event. `Event.Fixed` marks a paid event that reports neither amount nor currency: the provider vouches for the
invoice as issued, so the order's own amount is recorded; it may be set only for a paid event with both fields absent.
`InvoiceRequest.CustomerID` carries the bot's customer id for providers that need a payer identity (Platega).
Config: `PAL24_API_TOKEN`, `PAL24_SHOP_ID`, `PAL24_SIGNATURE_TOKEN`, `PLATEGA_MERCHANT_ID`, `PLATEGA_SECRET`,
`PLATEGA_PAYMENT_METHOD`, `PLATEGA_API_VERSION`, `PLATEGA_RETURN_URL`, `PLATEGA_FAILED_URL`.
