// Renewal: which subscriptions to offer and what the button does until
// payments exist (mirrors internal/profile.NeedsRenewal: 7 days).
import i18n from './i18n'
import type { Subscription } from './types'

const WINDOW_MS = 7 * 24 * 3600 * 1000

export const needsRenewal = (s: Subscription) => !s.unlimited && !!s.expire_at && new Date(s.expire_at).getTime() - Date.now() < WINDOW_MS

export const isExpired = (s: Subscription) => !!s.expire_at && new Date(s.expire_at).getTime() <= Date.now()

// daysLeftText: "сегодня" / "через 2 дня"
export function daysLeftText(s: Subscription) {
    const days = Math.floor((new Date(s.expire_at!).getTime() - Date.now()) / (24 * 3600 * 1000))
    return days < 1 ? i18n.t('renew.today') : i18n.t('renew.in_days', { count: days })
}

export const checkoutRenew = (subId: number) => `/checkout?kind=renew&item_kind=subscription&item_id=${subId}`
