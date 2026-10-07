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
    tariff_id: number | null
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
    renewal: Payment
}
export interface Payment {
    price: number
    from_own: number
    from_referral: number
    to_pay: number
}
export interface LedgerEntry {
    type: string
    amount: number
    date: string
}
export interface Referral {
    name: string
    active: boolean
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
export type OrderKind = 'renew' | 'subscribe' | 'addon' | 'change_tariff' | 'topup'
export interface OrderRequest {
    kind: OrderKind
    item_kind?: 'subscription' | 'addon'
    item_id?: number
    tariff_id?: number
    months?: number
    days?: number
    amount?: number
    method?: string
}
export interface OrderQuote {
    title: string
    price: number
    from_balance: number
    to_pay: number
    methods: string[]
    titles?: Record<string, string>
}
export interface Order {
    id: string
    number: string // short form to show people
    kind: OrderKind
    title: string
    price: number
    from_balance: number
    to_pay: number
    paid: number
    method: string
    pay_url?: string
    status: 'new' | 'awaiting_payment' | 'awaiting_receipt' | 'pending_review' | 'approving' | 'approved' | 'credited' | 'failed' | 'rejected' | 'expired' | 'cancelled'
    reason?: string
    until?: string
    created_at: string
}
