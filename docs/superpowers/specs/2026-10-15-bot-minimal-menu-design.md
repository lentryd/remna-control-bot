# Bot reduced to a launcher, the WebApp does the rest

## Goal

Keep only the basics in the bot. Browsing, choosing and paying move to the WebApp. The buttons of the
main menu stay, but each opens the WebApp on the matching page instead of drawing another screen in the chat.

## Decisions

- **Stays in the bot (variant A):** onboarding (`/start`, referral, entering a subscription link), the
  payment-details screen of a manual order with its cancel button, receipt intake (photo, file, text),
  Telegram Stars (`pre_checkout`, `successful_payment`), admin order cards, notifications.
- **Moves to the WebApp:** every list/detail/choice screen of the bot (see "Removed").
- Old inline keyboards in existing chats are not supported: the project is still in development, an unknown
  callback is simply ignored as today.

## Main menu (`view.Home`)

The text is unchanged (name, balance, subscriptions summary, renewal warning). Every button is a `web_app`
button built with `Links.Page(hash)`:

| Button | WebApp page |
|---|---|
| Open app | `/` |
| Subscriptions | `/subscriptions` |
| Balance | `/balance` |
| Friends | `/referrals` |
| Tariffs | `/tariffs` |
| Payments | `/orders` (new) |
| Renew (only when the first subscription ends soon or has ended) | `/checkout?kind=renew&item_kind=subscription&item_id=<id>` (`renew.ts` `checkoutRenew`) |

`/orders` is a new customer route that renders the existing `OrdersList` as a page of its own; `AppShell`
tabs are unchanged.

## Removed from the bot

- Screens: Subscriptions, Subscription, Balance, Referrals, Tariffs, RenewPeriods, Confirm, ProviderPay,
  Orders (history), with paging.
- Callbacks `m:` `s:` `ps:` `pt:` `r:` `po:` `rp:` `pr:` `pb:` `pp:` and their routing in `route.go` /
  `onCallback` / `onOrderCallback`. `pc:` (cancel) stays for the payment-details screen.
- Their golden files, unit tests and i18n keys (`subs.*`, `sub.*`, `balance.*`, `refs.*`, `tariffs.*`,
  `ledger.*`, `orders.*` and the `btn.*` only they used), in both `en.json` and `ru.json`. Keys used
  dynamically (`status.*`, `payment.*`, `time.*`, `unit.*`) are kept unless proven unused.
- Dead already: `app.name`, `msg.panel_down`.

## Notifications and order messages

- Traffic alert: buttons are `web_app` links to `/subscriptions/<id>` and `/tariffs` (the `s:<id>` callback goes).
- Payment reminder: `web_app` links to the renew checkout and to `/subscriptions/<id>` (the `r:<id>` callback goes).
- Order decision message to the customer: its `m:pay` button becomes a `web_app` link to `/orders`.
- Payment-details screen (manual order) keeps its cancel button; receipt intake is unchanged.
- Admin cards, `/admin`, `NotifyAdmins` are unchanged.

## Tests

- Update the golden files of the home screen and the notices; delete the ones of removed screens.
- Rewrite `buttons_test`, `renew_test`, `paging_test` and `route_test` for what remains (every Home button
  is `web_app` with the table above; the renew row targets the checkout).
- Add: `/orders` route renders for a customer; the Home keyboard fits Telegram's limits.
- The locale parity and jargon tests must pass after the key pruning.

## Docs

Update `README.md` (what the bot shows) and the "Bot UX" section of `2026-10-06-vpn-control-bot-design.md`.

## Out of scope

Moving link entry or receipt upload to the WebApp, removing Stars from the bot, changing the notification schedule.
