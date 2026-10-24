// Renewal: which subscriptions to offer and what the button does until
// payments exist (mirrors internal/profile.NeedsRenewal: 7 days).
import i18n from './i18n'
import type { Subscription } from './types'

const WINDOW_MS = 7 * 24 * 3600 * 1000

export const needsRenewal = (s: Subscription) => !s.unlimited && !!s.expire_at && new Date(s.expire_at).getTime() - Date.now() < WINDOW_MS

export const isExpired = (s: Pick<Subscription, 'expire_at'>) => !!s.expire_at && new Date(s.expire_at).getTime() <= Date.now()

// daysLeftText: "today" / "in 2 days"
export function daysLeftText(s: Subscription) {
    const days = Math.floor((new Date(s.expire_at!).getTime() - Date.now()) / (24 * 3600 * 1000))
    return days < 1 ? i18n.t('renew.today') : i18n.t('renew.in_days', { count: days })
}

export const checkoutRenew = (subId: number) => `/checkout?kind=renew&item_kind=subscription&item_id=${subId}`

// changeCandidates: subscriptions the storefront may offer to switch to the
// base tariff `tariffId` (live, limited, currently on another tariff).
export const changeCandidates = (subs: Subscription[], tariffId: number, tariffName?: string): Subscription[] =>
    subs.filter((s) => !s.unlimited && !isExpired(s) && (s.tariff_id != null ? s.tariff_id !== tariffId : s.tariff !== tariffName))

export const checkoutChange = (subId: number, tariffId: number) => `/checkout?kind=change_tariff&item_kind=subscription&item_id=${subId}&tariff_id=${tariffId}`
