# Tariff change for customers: surcharge or term exchange

Status: approved by the owner (icons: calendar/hourglass for "Remaining", refresh arrow for "Limit reset", from Tabler). Touches two repos: this bot and the panel (`/Users/victor/Projects/Go/vpn-control`, "remna-control"), which owns pricing and applies the change.

## Why

A customer can already open a subscription in the WebApp and press "change tariff", but the only way to switch is to pay the prorated surcharge. Two real situations are not served:

- The customer paid several months ahead and the target tariff costs about as much as a standard month. Paying more feels wrong; they would rather give up part of the paid term.
- The customer is looking at the tariff storefront (or ran out of traffic) and does not realise the current subscription can be upgraded. The only path there is "buy a new subscription".

Also, nowhere in the WebApp does a customer see when a traffic limit resets, and the subscription details page is hard to read.

## Goals

1. On the subscription details page, "change tariff" offers two ways to switch:
   - **Pay the difference** (exists today): the prorated surcharge, term unchanged.
   - **Exchange the remaining term**: no payment; the value of the remaining days at the old price becomes days at the new tariff's price, so the term shrinks (or grows, on a downgrade).
2. When a customer who already has a subscription picks a base tariff in the storefront/checkout, they choose: **new subscription** or **update an existing one** (and which one, if several).
3. Everywhere a traffic limit is shown in the WebApp, show when it resets.
4. Redesign the subscription details page (layout B below).

## Non-goals

- No change to the chat bot's menus; the chat keeps pointing to the WebApp.
- No new payment providers or payment flow: a surcharge goes through the existing `change_tariff` order.
- Add-on tariff switching UX is not redesigned; it keeps working as today.
- Unlimited ("forever") subscriptions stay without renew/change buttons.

## Anti-goals

- The exchange must never extend a term beyond the value the subscription's effective monthly price (same basis as `ProrateSurcharge`) gives, and must never silently give the customer less than the quote they saw.
- The bot must not re-implement panel pricing; it displays what the panel quotes.

## Term exchange (panel + bot)

Math, owned by the panel next to `ProrateSurcharge`:

    credit    = oldMonthly * daysLeft / 30
    newDays   = floor(credit / newMonthly * 30)      (newMonthly > 0)
    newExpire = now + newDays

The result is rounded down to whole days, so exchanging there and straight back never gains term.

Examples: 60 days left at 100 ₽/mo and a 200 ₽/mo target gives 30 days; the same 60 days to a 50 ₽/mo target gives 120 days.

- A new quote field (`exchange_expire_at`, `exchange_days`) is returned next to the existing surcharge in `QuoteTariffChange`; `ChangeTariff` gets a `mode` (`surcharge` default, `exchange`). In `exchange` mode it sets the new `expire_at` on the Remnawave user and records a `tariff_change` extension with surcharge 0.
- Included add-ons: the exchange applies the same included-add-on handling as a normal change; their credit is not folded into the term math. Included add-ons live and end with the subscription, so they follow the exchanged expiry; a paid add-on that becomes included keeps the later of its own expiry and the new one.
- Traffic counter behaviour is unchanged from the current change (used traffic is not reset; new limit applies).
- Free target tariff (`newMonthly <= 0`) is not offered an exchange.

Bot side: `OrderRequest` gets `mode`; `orders.price` returns 0 for exchange; fulfilment passes the mode to the panel. The order shows the new expiry date in the checkout before the customer confirms.

## Checkout choices

- From details page: tariff picker, then a two-option choice (pay / exchange) showing, for each, the exact outcome: "pay X ₽, term stays until D" or "pay nothing, term becomes until D2".
- From storefront: when the customer has at least one eligible subscription, a "new subscription / update existing" switch is shown before checkout; "update" leads into the same change-tariff flow with the chosen tariff preselected.

## Traffic reset visibility

The panel already provides `next_traffic_reset_at` and the strategy; the bot's profile model must carry it to the WebApp. Show "resets in N days, date" on the subscription list rows, the details page, and the change-tariff checkout (old and new tariff). For `NO_RESET` show that the limit refreshes on renewal (same wording as the traffic notice).

## Subscription list

Each row becomes a card with: title and status badge; two small facts, "Remaining" (days and end date, or "forever") and "Limit reset" (in N days, "after renewal" for `NO_RESET`, "no limit" when unlimited); a line with used/limit traffic and the tariff name plus add-on count; and a thin traffic bar (amber above 80%, red when exhausted). Unlimited subscriptions drop the bar.

## Subscription details page (layout B)

Header with name and status badge. Two tiles: "Remaining" (days, end date) and "Limit reset" (days, date). Below, a card with the traffic bar, device limit and tariff with price. Then "Renew" (primary), "Change tariff" (secondary, with an icon), the connection link, add-ons. Use icons, no emoji. A device limit of 0 is shown as unlimited or hidden (today it renders "up to 0"). Unlimited subscriptions show "forever" in the remaining tile and an explanatory line instead of the buttons.

Add-ons get their own "Add-ons" section under the connection link, split into two labelled groups so it is clear where each one comes from: "Included in your tariff" (no price, lives and ends with the subscription) and "Connected separately" (own price per month and own end date). Each card shows a human-readable name, not a raw code like `lte`: the add-on tariff's display name plus its short description from the panel, falling back to the add-on name only when those are empty. Then the status badge, end date or "forever", and its own traffic bar with reset date when the add-on has a limit. A separately connected add-on has a "Renew add-on" button (the existing `renew` order for an add-on item). A separately connected add-on that expires sooner than the subscription is flagged. The same grouping and names are used in the change-tariff checkout when included add-ons change ("will become included", "will be removed"). If there are none, a short "connect an add-on" entry leads to the storefront add-ons.

## Constraints

- Frontend: React + Mantine in `web/`, strings through i18n (`ru` and `en` both).
- Repo files in English.
- CI checks from `CLAUDE.md` apply to the bot; the panel has its own.
- The panel change must ship before (or together with) the bot change; the bot must tolerate an older panel that lacks the exchange fields by hiding the exchange option.

## Decided by the owner

- Two switching modes (surcharge and exchange) on the details page.
- Choice "new subscription / update current" during tariff checkout.
- Reset date shown wherever a limit is shown.
- Details page: layout B, icons instead of emoji.

## My calls (owner to check)

- Exchange math is linear by monthly price, rounded down to whole days.
- Exchange is available for downgrades too (term grows), not only upgrades.
- Add-ons in an exchange: included add-ons live and end with the subscription and follow the exchanged expiry; a paid add-on turned included keeps max(own expiry, new expiry), and its credit is not shown or applied in exchange mode.
- Hide the exchange option when the panel does not return the new quote fields.
- Downgrade surcharge stays clamped to 0 as today (no balance credit) for the pay mode; the exchange mode is the way to get value back from a downgrade.

- Price basis: `oldMonthly` is the subscription's effective price (price override if set, else the old tariff's current monthly price) and `newMonthly` is the target tariff's list monthly price, exactly as `QuoteTariffChange` uses today. Term discounts and the price actually paid are not tracked; the panel stores no paid price.
- Stable quote: the bot sends the quoted `exchange_days` with the order; the panel recomputes at apply time and rejects the change if the result is more than 1 day shorter than quoted. The checkout then asks the customer to re-confirm with a fresh quote.
- Edge rules for exchange: not offered for expired subscriptions, unlimited ones, the current tariff, free targets, or when the result would be under 1 day; the result is capped at 2 years. Whether current usage fits the new limits is not checked (same as the surcharge mode today).
- Zero-price order: created with the balance method and fulfilled immediately through the existing run path, no provider. If the panel call fails the order ends `failed` and nothing was charged. When the panel definitely refused (stale or unavailable exchange) the customer retries with a new order; when the result is unknown the customer is told not to repeat it and support checks. If the panel accepted the new expiry but the local switch failed, the panel puts the old expiry back (best effort).
- Storefront "update existing": eligible are the customer's non-unlimited, non-expired base subscriptions whose tariff differs from the chosen one; applies to base tariffs only. With exactly one eligible subscription the switch still shows, preselected to "update".

## Left to the builder

Exact component split, API field names beyond those above, i18n wording, test layout, and the order of work between the two repos.
