// Everything the operator reads here is a key of the `admin` locale namespace; screens call t().
export type Option = { value: string; labelKey: string; hintKey?: string }

export type Field = {
    name: string
    labelKey: string
    kind: 'text' | 'secret' | 'number' | 'bool' | 'select' | 'segmented' | 'textarea' | 'tags'
    options?: Option[]
    hintKey?: string
    unitKey?: string // shown inside the input: minutes, percent, ...
    group?: string // key of the block of the screen the field belongs to
    input?: 'url' | 'email' // the on-screen keyboard to ask for
    maxLength?: number
    decimal?: boolean // number fields are integers unless set
}

export type SectionDef = {
    key: string // the settings key, e.g. "pay.stars"
    titleKey: string
    hintKey?: string // what the payment service needs, one sentence
    fields: Field[]
    required?: string[] // fields an enabled section can't work without (as the server checks)
    webhook?: string
}

const ACCESS = 'groups.access'
const PARAMS = 'groups.params'

type FieldOpts = Partial<Omit<Field, 'name' | 'labelKey' | 'hintKey' | 'unitKey' | 'options'>> & { hint?: boolean; unit?: boolean }

// A field of `sec` (the locale id of the section): its texts live under sections.<sec>.<name>.
const fld = (sec: string, name: string, opts: FieldOpts & Pick<Field, 'kind'>): Field => {
    const { hint, unit, ...rest } = opts
    const base = `sections.${sec}.${name}`
    return { name, labelKey: `${base}.label`, ...(hint ? { hintKey: `${base}.hint` } : {}), ...(unit ? { unitKey: `${base}.unit` } : {}), ...rest }
}

const opt = (sec: string, name: string, value: string, hint = false): Option => ({
    value,
    labelKey: `sections.${sec}.${name}.${value}.label`,
    ...(hint ? { hintKey: `sections.${sec}.${name}.${value}.hint` } : {})
})

const section = (key: string, fields: (sec: string) => Field[], rest: Omit<SectionDef, 'key' | 'titleKey' | 'fields'> & { hint?: boolean } = {}): SectionDef => {
    const sec = key.replace('.', '_')
    const { hint, ...tail } = rest
    return { key, titleKey: `sections.${sec}.title`, ...(hint ? { hintKey: `sections.${sec}.hint` } : {}), fields: fields(sec), ...tail }
}

const enabled: Field = { name: 'enabled', labelKey: 'settings.enabled', kind: 'bool' }

export const GENERAL: SectionDef = section('general', (s) => [
    { ...fld(s, 'mode', { kind: 'segmented', group: 'groups.bot' }), options: [opt(s, 'mode', 'closed', true), opt(s, 'mode', 'open', true)] },
    fld(s, 'base_currency', { kind: 'text', group: 'groups.bot', maxLength: 3, hint: true }),
    { ...fld(s, 'pay_mode', { kind: 'segmented', group: 'groups.balance' }), options: [opt(s, 'pay_mode', 'mixed', true), opt(s, 'pay_mode', 'referral_full', true)] },
    fld(s, 'notify_minutes', { kind: 'number', unit: true, group: 'groups.notifications', hint: true }),
    fld(s, 'traffic_alert_pct', { kind: 'number', unit: true, group: 'groups.notifications', hint: true }),
    fld(s, 'remind_days', { kind: 'tags', unit: true, group: 'groups.notifications', hint: true }),
    fld(s, 'order_ttl_hours', { kind: 'number', unit: true, group: 'groups.orders', hint: true })
])

export const METHODS: SectionDef[] = [
    section('pay.manual', (s) => [enabled, fld(s, 'details', { kind: 'textarea', group: PARAMS, hint: true })], { hint: true, required: ['details'] }),
    section(
        'pay.yookassa',
        (s) => [
            enabled,
            fld(s, 'shop_id', { kind: 'text', group: ACCESS }),
            fld(s, 'secret_key', { kind: 'secret', group: ACCESS }),
            fld(s, 'return_url', { kind: 'text', group: PARAMS, input: 'url', hint: true }),
            fld(s, 'receipt_email', { kind: 'text', group: PARAMS, input: 'email', hint: true }),
            fld(s, 'vat_code', { kind: 'number', group: PARAMS })
        ],
        { hint: true, webhook: 'yookassa', required: ['shop_id', 'secret_key'] }
    ),
    section(
        'pay.cryptobot',
        (s) => [
            enabled,
            fld(s, 'token', { kind: 'secret', group: ACCESS }),
            fld(s, 'testnet', { kind: 'bool', group: PARAMS }),
            fld(s, 'assets', { kind: 'text', group: PARAMS, hint: true })
        ],
        { hint: true, webhook: 'cryptobot', required: ['token'] }
    ),
    section(
        'pay.heleket',
        (s) => [
            enabled,
            fld(s, 'merchant_id', { kind: 'text', group: ACCESS }),
            fld(s, 'api_key', { kind: 'secret', group: ACCESS }),
            fld(s, 'to_currency', { kind: 'text', group: PARAMS, hint: true }),
            fld(s, 'network', { kind: 'text', group: PARAMS, hint: true })
        ],
        { hint: true, webhook: 'heleket', required: ['merchant_id', 'api_key'] }
    ),
    section(
        'pay.pal24',
        (s) => [
            enabled,
            fld(s, 'api_token', { kind: 'secret', group: ACCESS }),
            fld(s, 'shop_id', { kind: 'text', group: ACCESS }),
            fld(s, 'signature_token', { kind: 'secret', group: ACCESS, hint: true })
        ],
        { hint: true, webhook: 'pal24', required: ['api_token', 'shop_id'] }
    ),
    section(
        'pay.platega',
        (s) => [
            enabled,
            fld(s, 'merchant_id', { kind: 'text', group: ACCESS }),
            fld(s, 'secret', { kind: 'secret', group: ACCESS }),
            fld(s, 'payment_method', { kind: 'number', group: PARAMS, hint: true }),
            { ...fld(s, 'api_version', { kind: 'segmented', group: PARAMS }), options: [opt(s, 'api_version', 'v1'), opt(s, 'api_version', 'v2')] },
            fld(s, 'return_url', { kind: 'text', group: PARAMS, input: 'url', hint: true }),
            fld(s, 'failed_url', { kind: 'text', group: PARAMS, input: 'url' })
        ],
        { hint: true, webhook: 'platega', required: ['merchant_id', 'secret'] }
    ),
    section('pay.stars', (s) => [enabled, fld(s, 'rate', { kind: 'number', group: PARAMS, decimal: true, hint: true })], { hint: true, required: ['rate'] })
]

/** Title keys of the sections by key; an unknown key is shown as is (not translated). */
export function sectionTitleKeys(keys: string[]): { key: string; titleKey?: string }[] {
    return keys.map((k) => ({ key: k, titleKey: [GENERAL, ...METHODS].find((d) => d.key === k)?.titleKey }))
}

/** The fields of a section in blocks, in the order the blocks first appear; `key` is the group's locale key. */
export function fieldGroups(fields: Field[]): { key: string; fields: Field[] }[] {
    const out: { key: string; fields: Field[] }[] = []
    for (const f of fields) {
        const key = f.group ?? ''
        const g = out.find((x) => x.key === key) ?? (out.push({ key, fields: [] }), out[out.length - 1])
        g.fields.push(f)
    }
    return out
}
