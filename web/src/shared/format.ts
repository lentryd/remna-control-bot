// Copied from vpn-control/web/src/components/format.ts (trimmed to what the WebApp uses).
import dayjs from 'dayjs'

import i18n from '../i18n'

// Money is shown in the base currency with the UI locale; main.tsx sets the
// locale and App sets the base currency from the profile before it renders pages.
let base = 'RUB'
let locale = 'ru-RU'
let money0: Intl.NumberFormat
let money2: Intl.NumberFormat

function build() {
    const opts = { style: 'currency', currency: base, currencyDisplay: 'narrowSymbol' } as const
    money0 = new Intl.NumberFormat(locale, { ...opts, maximumFractionDigits: 0 })
    money2 = new Intl.NumberFormat(locale, { ...opts, maximumFractionDigits: 2, minimumFractionDigits: 2 })
}
build()

export function setFormat(opts: { base?: string; locale?: string }) {
    const b = opts.base || base
    const l = opts.locale || locale
    if (b === base && l === locale) return
    base = b
    locale = l
    build()
}

// The narrow symbol of the current base currency ($, ₽, €), or its ISO code
// when the locale has no symbol for it.
export function currencySymbol(): string {
    const part = money0.formatToParts(0).find((p) => p.type === 'currency')
    return part?.value || base
}

// fmtMoney with digits 0 drops the cents of whole amounts only: 120 → 120 ₽,
// 119.8 → 119,80 ₽ (never a lone tenth digit).
export function fmtMoney(v: number | null | undefined, digits: 0 | 2 = 0) {
    if (v === null || v === undefined) return '—'
    const cents = Math.round(v * 100)
    return (digits || cents % 100 !== 0 ? money2 : money0).format(cents / 100)
}

const fmtNum = (v: number, digits = 2) => new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(v)

export function fmtBytes(b: number | null | undefined): string {
    if (!b) return '0'
    const units = (['b', 'kb', 'mb', 'gb', 'tb'] as const).map((u) => i18n.t(`format.units.${u}`))
    let i = 0
    let v = b
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
    }
    return `${fmtNum(v, v < 10 ? 2 : 1)} ${units[i]}`
}

// The panel's "forever" is an expiry in 2099; anything from 2090 on is unlimited.
export const UNLIMITED_YEAR = 2090
export const isUnlimited = (d: string | null | undefined) => !!d && dayjs(d).year() >= UNLIMITED_YEAR

export const fmtDate = (d: string | null | undefined) => (!d ? '—' : isUnlimited(d) ? '∞' : dayjs(d).format(i18n.t('format.date')))

export function daysLeft(d: string | null | undefined): number | null {
    if (!d) return null
    if (isUnlimited(d)) return Infinity
    return Math.floor(dayjs(d).diff(dayjs(), 'hour') / 24)
}
