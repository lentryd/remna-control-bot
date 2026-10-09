export type Option = { value: string; label: string; hint?: string }

export type Field = {
    name: string
    label: string
    kind: 'text' | 'secret' | 'number' | 'bool' | 'select' | 'segmented' | 'textarea' | 'tags'
    options?: Option[]
    hint?: string
    unit?: string // shown inside the input: «мин», «%»
    group?: string // the block of the screen the field belongs to
    input?: 'url' | 'email' // the on-screen keyboard to ask for
    maxLength?: number
    decimal?: boolean // number fields are integers unless set
}

export type SectionDef = {
    key: string
    title: string
    hint?: string // what the provider needs, one sentence
    fields: Field[]
    required?: string[] // fields an enabled section can't work without (as the server checks)
    webhook?: string
}

const ACCESS = 'Доступ'
const PARAMS = 'Параметры'

const enabled: Field = { name: 'enabled', label: 'Включено', kind: 'bool' }

export const GENERAL: SectionDef = {
    key: 'general',
    title: 'Общие настройки',
    fields: [
        {
            name: 'mode',
            label: 'Режим бота',
            kind: 'segmented',
            group: 'Бот',
            options: [
                { value: 'closed', label: 'Закрытый', hint: 'Бот отвечает только клиентам панели' },
                { value: 'open', label: 'Открытый', hint: 'Бот отвечает любому пользователю Telegram' }
            ]
        },
        {
            name: 'base_currency',
            label: 'Базовая валюта',
            kind: 'text',
            group: 'Бот',
            maxLength: 3,
            hint: 'Код из 3 заглавных букв, например RUB. Пусто — берём из панели'
        },
        {
            name: 'pay_mode',
            label: 'Способ оплаты с баланса',
            kind: 'segmented',
            group: 'Оплата с баланса',
            options: [
                { value: 'mixed', label: 'Вместе', hint: 'Основной и реферальный баланс списываются вместе' },
                { value: 'referral_full', label: 'Только реферальный целиком', hint: 'Реферальные деньги тратятся, только если покрывают всю цену' }
            ]
        },
        {
            name: 'notify_minutes',
            label: 'Интервал проверки',
            kind: 'number',
            unit: 'мин',
            group: 'Уведомления',
            hint: 'Как часто проверять трафик и сроки. 0 — уведомления выключены'
        },
        { name: 'traffic_alert_pct', label: 'Порог трафика', kind: 'number', unit: '%', group: 'Уведомления', hint: 'Предупредить клиента, когда израсходовано столько трафика' },
        {
            name: 'remind_days',
            label: 'Напоминать за',
            kind: 'tags',
            unit: 'дн.',
            group: 'Уведомления',
            hint: 'Сколько дней до конца подписки. Введите число и нажмите Enter или запятую'
        },
        { name: 'order_ttl_hours', label: 'Срок жизни неоплаченного заказа', kind: 'number', unit: 'ч', group: 'Заказы', hint: 'Через сколько часов неоплаченный заказ истекает' }
    ]
}

export const METHODS: SectionDef[] = [
    {
        key: 'pay.manual',
        title: 'Перевод с квитанцией',
        hint: 'Клиент переводит деньги по вашим реквизитам и присылает квитанцию, вы подтверждаете оплату вручную.',
        required: ['details'],
        fields: [enabled, { name: 'details', label: 'Реквизиты', kind: 'textarea', group: PARAMS, hint: 'Показываются клиенту рядом с суммой' }]
    },
    {
        key: 'pay.yookassa',
        title: 'ЮKassa',
        hint: 'Нужны Shop ID и секретный ключ из кабинета ЮKassa. Webhook укажите в настройках уведомлений магазина.',
        webhook: 'yookassa',
        required: ['shop_id', 'secret_key'],
        fields: [
            enabled,
            { name: 'shop_id', label: 'Shop ID', kind: 'text', group: ACCESS },
            { name: 'secret_key', label: 'Секретный ключ', kind: 'secret', group: ACCESS },
            { name: 'return_url', label: 'Куда вернуть клиента', kind: 'text', group: PARAMS, input: 'url', hint: 'Пусто — в приложение' },
            { name: 'receipt_email', label: 'Email для чека', kind: 'text', group: PARAMS, input: 'email', hint: 'Пусто — без чека' },
            { name: 'vat_code', label: 'Код НДС (1–6)', kind: 'number', group: PARAMS }
        ]
    },
    {
        key: 'pay.cryptobot',
        title: 'CryptoBot',
        hint: 'Нужен API-токен приложения из @CryptoBot (Crypto Pay). Webhook укажите в настройках приложения.',
        webhook: 'cryptobot',
        required: ['token'],
        fields: [
            enabled,
            { name: 'token', label: 'API-токен', kind: 'secret', group: ACCESS },
            { name: 'testnet', label: 'Тестовая сеть', kind: 'bool', group: PARAMS },
            { name: 'assets', label: 'Принимаемые активы', kind: 'text', group: PARAMS, hint: 'USDT,TON — пусто: все' }
        ]
    },
    {
        key: 'pay.heleket',
        title: 'Heleket',
        hint: 'Нужны Merchant ID и API-ключ из кабинета Heleket. Webhook укажите в настройках магазина.',
        webhook: 'heleket',
        required: ['merchant_id', 'api_key'],
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text', group: ACCESS },
            { name: 'api_key', label: 'API-ключ', kind: 'secret', group: ACCESS },
            { name: 'to_currency', label: 'Актив', kind: 'text', group: PARAMS, hint: 'Необязательно' },
            { name: 'network', label: 'Сеть', kind: 'text', group: PARAMS, hint: 'Необязательно' }
        ]
    },
    {
        key: 'pay.pal24',
        title: 'PayPalych',
        hint: 'Нужны API-токен и Shop ID из кабинета PayPalych. Postback-адрес укажите в настройках магазина.',
        webhook: 'pal24',
        required: ['api_token', 'shop_id'],
        fields: [
            enabled,
            { name: 'api_token', label: 'API-токен', kind: 'secret', group: ACCESS },
            { name: 'shop_id', label: 'Shop ID', kind: 'text', group: ACCESS },
            { name: 'signature_token', label: 'Токен подписи postback', kind: 'secret', group: ACCESS, hint: 'Пусто — используется API-токен' }
        ]
    },
    {
        key: 'pay.platega',
        title: 'Platega',
        hint: 'Нужны Merchant ID и секрет из кабинета Platega. Webhook укажите в настройках мерчанта.',
        webhook: 'platega',
        required: ['merchant_id', 'secret'],
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text', group: ACCESS },
            { name: 'secret', label: 'Секрет', kind: 'secret', group: ACCESS },
            { name: 'payment_method', label: 'Метод оплаты', kind: 'number', group: PARAMS, hint: '2 СБП QR, 11 карты, 12 зарубежные карты, 13 крипта' },
            {
                name: 'api_version',
                label: 'Версия API',
                kind: 'segmented',
                group: PARAMS,
                options: [
                    { value: 'v1', label: 'v1' },
                    { value: 'v2', label: 'v2' }
                ]
            },
            { name: 'return_url', label: 'URL успеха', kind: 'text', group: PARAMS, input: 'url', hint: 'Пусто — в приложение' },
            { name: 'failed_url', label: 'URL ошибки', kind: 'text', group: PARAMS, input: 'url' }
        ]
    },
    {
        key: 'pay.stars',
        title: 'Telegram Stars',
        hint: 'Оплата звёздами Telegram без внешнего кабинета: достаточно указать курс.',
        required: ['rate'],
        fields: [enabled, { name: 'rate', label: 'Курс одной звезды', kind: 'number', group: PARAMS, decimal: true, hint: 'Сколько базовой валюты стоит 1 ⭐' }]
    }
]

/** Titles of the sections by key; an unknown key is shown as is. */
export function sectionTitles(keys: string[]): string[] {
    return keys.map((k) => [GENERAL, ...METHODS].find((d) => d.key === k)?.title ?? k)
}

/** The fields of a section in blocks, in the order the blocks first appear. */
export function fieldGroups(fields: Field[]): { title: string; fields: Field[] }[] {
    const out: { title: string; fields: Field[] }[] = []
    for (const f of fields) {
        const title = f.group ?? ''
        const g = out.find((x) => x.title === title) ?? (out.push({ title, fields: [] }), out[out.length - 1])
        g.fields.push(f)
    }
    return out
}
