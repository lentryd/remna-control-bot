# Payments: order core + manual payment — design
> **Superseded in part:** admin decisions and all `*_`-prefixed provider/payment env settings moved to the admin web app — see `2026-10-09-admin-webapp-settings-design.md`.

Date: 2026-10-07. Status: approved in brainstorming. Follow-up: payment providers (separate spec, reuses this core).

## Goal

Let customers pay for anything the bot can sell — renewing a subscription or add-on, a new subscription,
connecting an add-on, changing a tariff, topping up the balance — without any payment provider: the customer
sends a receipt, an admin approves it with a button in a Telegram chat, the panel records the payment and
applies it, and the customer is notified and sees the history. Aimed at operators who sell among friends and
do not want to set up providers. Providers later plug into the same order core.

## Decisions

- **The panel needs no new endpoints.** Everything exists in vpn-control `/api/v1`; only the `bot` token preset grows.
- **Orders live in the bot's SQLite** — the order lifecycle is the bot's concern. The panel only receives the
  final payment via `customers:pay`, with `note = "bot order #N"`.
- **Admins:** one `ADMIN_CHAT_ID` (private chat or group) receives cards; only Telegram ids in `ADMIN_IDS` may press their buttons.
- **Payment details:** one free text `MANUAL_PAY_DETAILS`. A receipt (photo, document or text) is mandatory.
- **Where:** orders are built in the WebApp (and "Renew" in the bot); the receipt is sent in the bot chat.
- **Reject** asks for a reason (or "no reason"). **Admin may correct the received amount.**
- **Price is fixed at order creation**, computed server-side; never taken from the client, not recomputed on approval.

## Panel (vpn-control) change

`web/src/components/api-tokens/modals/create-api-token-modal.tsx` — `BOT_PRESET_KEYS` adds:
`items:quote`, `items:extend`, `items:tariff_quote`, `items:tariff`, `customers:payment_preview`, `customers:pay`,
`subscriptions:provision`, `subscriptions:connect_addon`. Update the comment above it.

Endpoints used (all amounts in major units):

| Scope | Call | Use |
|---|---|---|
| `items:quote` | `GET /items/{kind}/{id}/quote?months=&days=` | renewal price, from/to, periods, balance |
| `items:extend` | `POST /items/{kind}/{id}/extend {months,days,amount,allow_debt}` | renew paid fully from the balance |
| `items:tariff_quote` | `GET /items/{kind}/{id}/tariff-quote?tariff_id=` | tariff change surcharge |
| `items:tariff` | `POST /items/{kind}/{id}/tariff` | apply tariff change |
| `customers:pay` | `POST /customers/{id}/payments {amount,date,method,note,allocations[]}` | record payment (+ledger, +referral) and extend allocations; failed extension refunds to balance, reported in `extensions[].ok/error` |
| `subscriptions:provision` | `POST /subscriptions/provision {customer_id,tariff_id,username,label,months,days,amount,allow_debt}` | new subscription |
| `subscriptions:connect_addon` | `POST /subscriptions/{id}/addons {tariff_id,months,days,amount,allow_debt}` | connect add-on |

`customers:payment_preview` is granted for future use (not required by this spec's flows).

## Order model — `internal/orders`

```
Order {
  ID, CustomerID, ChatID
  Kind        renew | subscribe | addon | change_tariff | topup
  ItemKind    subscription | addon          (renew, change_tariff)
  ItemID      int                           (renew, change_tariff; subscription id for addon)
  TariffID    int                           (subscribe, addon, change_tariff)
  Months, Days
  Title       human label snapshot ("Дом · 3 мес.")
  Price       full price of the purchase (topup: requested amount)
  FromBalance part covered by the balance (billing.Split with BALANCE_PAY_MODE)
  ToPay       Price - FromBalance
  Received    amount the admin confirmed (nil until corrected; defaults to ToPay)
  Method      manual | balance              (providers later)
  Status      see below
  ReceiptKind photo | document | text; ReceiptFileID; Comment
  RejectReason; AdminMsgID; Admin (who decided, @username/id)
  PanelPaymentID; Error
  CreatedAt, UpdatedAt
}
```

Statuses: `awaiting_receipt → pending_review → approving → approved | credited | failed`;
`pending_review → rejected`; `awaiting_receipt → expired | cancelled`. Balance method: `created → approving → approved | failed`.
`failed` means the panel payment may have been recorded but the second step failed (`PanelPaymentID` set → only
the second step is retried).

Transitions are done with `UPDATE orders SET status=? WHERE id=? AND status=?` — a lost race returns
`ErrStateChanged`, so double clicks and (later) repeated webhooks never pay twice.

Pricing (`orders.Quote`), server-side:
- renew: `items:quote` amount for months/days.
- subscribe: the tariff period price (`Tariff.Periods` match, else `MonthlyPrice*months`), via panel tariffs.
- addon: add-on tariff period price.
- change_tariff: `tariff_quote.surcharge` (months/days unused).
- topup: requested amount (> 0), no balance use.
`billing.Split(Price, own, referral, mode)` gives FromBalance/ToPay (topup: FromBalance = 0).

### Fulfill — the only code that changes money in the panel

`Fulfill(ctx, orderID, received)`; `amount = received` (manual) or `0` (balance method — nothing new paid):

- renew → `customers:pay{amount, method, note, allocations:[{kind,id,months,days,amount=Price}]}`; result `to` date taken from `extensions[0]`; `ok=false` → `failed` with the error (money stays on balance).
- topup → `customers:pay{amount}` with no allocations → `approved`.
- subscribe → `customers:pay{amount}` (skipped if amount=0) then `provision{amount=Price, allow_debt=false}`.
- addon → `customers:pay` then `connect_addon{amount=Price, allow_debt=false}`.
- change_tariff → `customers:pay` then `items:tariff` for the target tariff.
- Balance method with `customers:pay` skipped: renew uses `items:extend{amount=Price, allow_debt=false}`.

Amount correction (manual only), `received < ToPay`:
- "Extend on debt" (renew only): `customers:pay{amount=received}` with full allocation; balance goes negative → `approved`.
- "Credit to balance only": `customers:pay{amount=received}`, no allocations → `credited`.
`received ≥ ToPay` → normal Fulfill; overpay stays on balance automatically.

Replaces the `billing.Billing`/`NotImplemented` stub; `billing.Split` stays.

## Storage

New SQLite tables in `internal/store` (migrations in the existing style):
- `orders` — the model above.
- `chat_state(chat_id PK, kind, order_id, created_at)` — what the next message in a chat means:
  `receipt` (customer chat waits for a receipt), `reject_reason`, `amount` (admin chat waits for a reply).
  Admin prompts are matched by `reply_to_message_id` stored alongside (`prompt_msg_id`), so several pending prompts in a group don't collide.

## Bot flows

### Customer
1. Order from the WebApp, or bot "Renew": subscription → period buttons (tariff `Periods`) → amount screen.
   If the balance covers everything, the only method is "Pay from balance" → Fulfill without an admin.
2. Pay screen: "Заказ №N: <title>. Цена X, с баланса Y, **к оплате Z**" + `MANUAL_PAY_DETAILS` +
   "📎 Отправить квитанцию", "Отмена". Status `awaiting_receipt`, `chat_state=receipt`.
3. Photo / document / text → stored (`file_id`, caption → comment), status `pending_review`, reply
   "Квитанция отправлена, ждите подтверждения", card posted to the admin chat. Other content → hint.
4. Unpaid `awaiting_receipt` older than `ORDER_TTL` → `expired` (swept by the notify runner tick).

### Admin card (`ADMIN_CHAT_ID`)
Receipt re-sent by `file_id` (`sendPhoto` / `sendDocument` / text message) with caption: customer (name,
@username, panel id), title, period, price, from balance, to pay, comment. Buttons:
"✅ Подтвердить", "✏️ Изменить сумму", "❌ Отклонить". Callback from a user not in `ADMIN_IDS` → alert "Нет прав".

- ✅ → Fulfill → caption "✅ Подтвердил @admin, продлено до DD.MM", buttons removed; error → "⚠️ Ошибка: …" + "🔁 Повторить".
- ✏️ → ForceReply "Фактически получено по №N?" → number → `Received` set, card re-rendered with
  "Сумма исправлена @admin: 700 → 500 ₽"; if `received < ToPay` buttons become "✅ Продлить в долг" (renew only) / "💰 Только на баланс" / "❌ Отклонить".
- ❌ → ForceReply "Причина отклонения №N?" + inline "Без причины" → `rejected`, caption "❌ Отклонил @admin: причина".

### Customer notifications
- approved: "✅ Оплата №N подтверждена: <title>, действует до DD.MM.YYYY"
- credited: "Получено X из Z, зачислено на баланс — доплатите разницу"
- rejected: "❌ Платёж №N отклонён: <причина>"
- failed: "⚠️ Оплата №N принята, но применить не удалось — деньги на балансе, администратор разберётся"
Amount corrections are shown in the message.

### History in the bot
Menu button "🧾 Платежи": last 10 orders with status icons (⏳ на проверке, ✅, 💰, ❌, ⌛, ⚠️), paging like `ps:`/`pt:`.

## WebApp

API (auth `tma`, as now):
- `POST /api/orders/quote {kind, item_kind?, item_id?, tariff_id?, months, days, amount?}` → `{price, from_balance, to_pay, methods}`; methods: `manual` (if enabled), `balance` (if to_pay = 0).
- `POST /api/orders` (same body) → `{id, to_pay, details}`.
- `POST /api/orders/{id}/receipt-request` → sets `chat_state=receipt`, sends the pay screen into the chat; frontend calls `Telegram.WebApp.close()`.
- `POST /api/orders/{id}/pay-balance`, `POST /api/orders/{id}/cancel`.
- `GET /api/orders` → history.
Ownership: every `{id}` must belong to the caller's customer (else 404).

UI: subscription card "Продлить" → period → pay screen; tariffs "Подключить" (new subscription, or add-on to a chosen subscription); "Сменить тариф" on a subscription; balance "Пополнить"; Balance tab gets a "Заявки на оплату" block with statuses (ledger unchanged). Texts in `web/public/locales/ru/bot.json`.

## Config

- `ADMIN_CHAT_ID` (int64), `ADMIN_IDS` (comma-separated int64), `MANUAL_PAY_DETAILS` (text), `ORDER_TTL` (default `48h`).
- Manual payment is enabled only when `ADMIN_CHAT_ID`, `ADMIN_IDS` and `MANUAL_PAY_DETAILS` are set; otherwise the current "Оплата скоро" stays (balance-only payments still work).

## Errors

- Panel unavailable on Fulfill → status reverts `approving → pending_review`, admin sees "⚠️ Панель недоступна, попробуйте позже".
- Panel recorded payment but extension/provision failed → `failed`, `PanelPaymentID` kept; "🔁 Повторить" redoes only the second step.
- Receipt for an order no longer `awaiting_receipt` → "Заказ уже обработан".
- Archived/missing customer at Fulfill → `failed` with message.

## Testing

- `orders`: transition table, `ErrStateChanged` on races, Fulfill per Kind on `vpnctl.Fake`, partial failure + retry, amount correction both branches, quote per Kind incl. Split modes.
- `store`: orders and chat_state CRUD, expiry query.
- `vpnctl`: httptest for the new methods (DTO mapping, major units).
- `telegram/view`: golden tests for pay screen, admin card variants, notifications, history.
- `telegram`: non-admin callback rejected; receipt routing by `chat_state`.
- `webapp`: `/api/orders*` handlers incl. ownership checks.

## Out of scope

Payment providers and their webhooks (next spec), refunds, multiple payment-details variants, receipt OCR.
