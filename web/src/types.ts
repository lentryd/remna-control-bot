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
    tariff_id: number | null
    reset_at: string | null
    reset_strategy: string
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
    reset_at: string | null
    reset_strategy: string
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
    currency: string
    is_admin: boolean
}
export interface Period {
    months: number
    days: number
    price: number
}
export interface IncludedAddon {
    name: string
    traffic_limit_bytes: number
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
    includes: IncludedAddon[]
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
    mode?: 'surcharge' | 'exchange' // change_tariff only; surcharge is the default and omitted
    expected_days?: number // exchange only: the quoted term the panel re-checks
}
export interface AddonChange {
    subscription_addon_id?: number | null
    addon_id: number
    addon_name: string
    tariff_name: string
    action: string // connect | include | remove, anything else is shown by name only
    credit: number
}
export interface TariffChange {
    surcharge: number
    expire_at?: string
    exchange_days: number // < 1 or absent: exchange is not offered
    exchange_expire_at?: string
    addons?: AddonChange[]
}
export interface OrderQuote {
    change?: TariffChange
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
    // a definite exchange failure: nothing changed and the plan change can be started again
    fail_reason?: 'exchange_stale' | 'exchange_unavailable'
    until?: string
    created_at: string
}

export type Decision = 'full' | 'debt' | 'credit'

export interface AdminOrder extends Order {
    customer_id: number
    customer_name?: string
    telegram_id: number
    comment?: string
    receipt_kind?: 'photo' | 'document' | 'text' | ''
    received: number | null
    admin?: string
    decision?: string
    error?: string
    external_id?: string
    pay_amount?: number
    pay_currency?: string
}

export interface AdminSettings {
    sections: Record<string, Record<string, unknown>>
    lost: string[]
    webhook_base: string
    profile_defaults?: Record<string, ProfileTexts> // the built-in bot profile texts, by language
}

export interface ProfileTexts {
    name: string
    description: string
    about: string
}

export interface ProfileApply {
    failed: { lang: string; field: 'name' | 'description' | 'about'; message: string }[]
    retry_after: number
}

export interface ProfileSaved {
    texts: Record<string, ProfileTexts>
    apply?: ProfileApply
}

export interface CheckResult {
    ok: boolean
    supported: boolean
    message?: string
}
