export interface Addon {
    id: number
    name: string
    tariff: string
    price: number
    included: boolean
    status: string
    expire_at: string | null
    unlimited: boolean
    used_bytes: number
    limit_bytes: number
}
export interface Subscription {
    id: number
    title: string
    tariff: string
    price: number
    status: string
    expire_at: string | null
    unlimited: boolean
    used_bytes: number
    limit_bytes: number
    device_limit: number | null
    url: string
    addons: Addon[]
}
export interface LedgerEntry {
    type: string
    amount: number
    date: string
}
export interface Referral {
    name: string
    active: boolean
    monthly: number
    total_paid: number
    earned: number
}
export interface Profile {
    id: number
    name: string
    balance: number
    balance_own: number
    balance_referral: number
    monthly: number
    nearest_expire_at: string | null
    subscriptions: Subscription[]
    ledger: LedgerEntry[]
    referrals: Referral[]
    referrals_earned: number
    ref_link: string
}
export interface Period {
    months: number
    days: number
    price: number
}
export interface Tariff {
    id: number
    kind: 'base' | 'addon'
    name: string
    description: string
    addon_name: string
    monthly_price: number
    traffic_limit_bytes: number
    hwid_limit: number | null
    periods: Period[] | null
}
