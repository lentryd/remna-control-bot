// pricing.ts — what a term of a tariff costs, as the server prices it
// (keep in sync with internal/orders/pricing.go).
import type { Period, Tariff } from './types'

const kop = (v: number) => Math.round(v * 100)

export const termCost = (monthly: number, periods: Period[], months: number, days: number) => {
    if (days > 0) {
        const exact = periods.find((p) => p.months === months && p.days === days)
        if (exact) return exact.price
    }
    const m = kop(monthly)
    const best = [0]
    for (let i = 1; i <= months; i++) {
        best[i] = best[i - 1] + m
        for (const p of periods) {
            if (p.days === 0 && p.months > 0 && p.months <= i) best[i] = Math.min(best[i], best[i - p.months] + kop(p.price))
        }
    }
    return (best[months] + Math.round((m * days) / 30)) / 100
}

// the price of the same term paid month by month
export const regularCost = (monthly: number, months: number, days: number) => monthly * (months + days / 30)

// saving of a price against the regular one, in whole percent
export const savingPct = (regular: number, price: number) => (regular > 0 ? Math.max(0, Math.round((1 - price / regular) * 100)) : 0)

// the period with the biggest saving and its price per month; null without any saving
export function bestPeriod(tf: Tariff) {
    let best: { period: Period; pct: number; perMonth: number; saved: number } | null = null
    for (const p of tf.periods ?? []) {
        const len = p.months + p.days / 30
        if (len <= 0) continue
        const regular = regularCost(tf.monthly_price, p.months, p.days)
        const pct = savingPct(regular, p.price)
        if (pct > (best?.pct ?? 0)) best = { period: p, pct, perMonth: p.price / len, saved: regular - p.price }
    }
    return best
}

export const termKey = (p: Pick<Period, 'months' | 'days'>) => `${p.months}:${p.days}`
