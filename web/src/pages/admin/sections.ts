export type Field = {
    name: string
    label: string
    kind: 'text' | 'secret' | 'number' | 'bool' | 'select' | 'textarea' | 'days'
    options?: string[]
    hint?: string
    decimal?: boolean // number fields are integers unless set
}

export type SectionDef = { key: string; title: string; fields: Field[]; webhook?: string }

const enabled: Field = { name: 'enabled', label: 'Включено', kind: 'bool' }

export const GENERAL: SectionDef = {
    key: 'general',
    title: 'Общие настройки',
    fields: [
        { name: 'mode', label: 'Режим бота', kind: 'select', options: ['closed', 'open'], hint: 'closed — только клиенты панели, open — любой' },
        { name: 'base_currency', label: 'Базовая валюта', kind: 'text', hint: 'Код из 3 заглавных букв. Пусто — берём из панели' },
        { name: 'pay_mode', label: 'Оплата с баланса', kind: 'select', options: ['mixed', 'referral_full'], hint: 'referral_full — реферальные деньги только если покрывают всю цену' },
        { name: 'notify_minutes', label: 'Проверка уведомлений, мин', kind: 'number', hint: '0 — выключить' },
        { name: 'traffic_alert_pct', label: 'Порог трафика, %', kind: 'number' },
        { name: 'remind_days', label: 'Напоминать за, дней', kind: 'days', hint: 'Через запятую: 3,1' },
        { name: 'order_ttl_hours', label: 'Срок жизни неоплаченного заказа, ч', kind: 'number' }
    ]
}

export const METHODS: SectionDef[] = [
    {
        key: 'pay.manual',
        title: 'Перевод с квитанцией',
        fields: [enabled, { name: 'details', label: 'Реквизиты', kind: 'textarea', hint: 'Показываются клиенту рядом с суммой' }]
    },
    {
        key: 'pay.yookassa',
        title: 'ЮKassa',
        webhook: 'yookassa',
        fields: [
            enabled,
            { name: 'shop_id', label: 'Shop ID', kind: 'text' },
            { name: 'secret_key', label: 'Секретный ключ', kind: 'secret' },
            { name: 'return_url', label: 'Куда вернуть клиента', kind: 'text', hint: 'Пусто — в приложение' },
            { name: 'receipt_email', label: 'Email для чека', kind: 'text', hint: 'Пусто — без чека' },
            { name: 'vat_code', label: 'Код НДС (1–6)', kind: 'number' }
        ]
    },
    {
        key: 'pay.cryptobot',
        title: 'CryptoBot',
        webhook: 'cryptobot',
        fields: [
            enabled,
            { name: 'token', label: 'API-токен', kind: 'secret' },
            { name: 'testnet', label: 'Тестовая сеть', kind: 'bool' },
            { name: 'assets', label: 'Принимаемые активы', kind: 'text', hint: 'USDT,TON — пусто: все' }
        ]
    },
    {
        key: 'pay.heleket',
        title: 'Heleket',
        webhook: 'heleket',
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text' },
            { name: 'api_key', label: 'API-ключ', kind: 'secret' },
            { name: 'to_currency', label: 'Актив (необязательно)', kind: 'text' },
            { name: 'network', label: 'Сеть (необязательно)', kind: 'text' }
        ]
    },
    {
        key: 'pay.pal24',
        title: 'PayPalych',
        webhook: 'pal24',
        fields: [
            enabled,
            { name: 'api_token', label: 'API-токен', kind: 'secret' },
            { name: 'shop_id', label: 'Shop ID', kind: 'text' },
            { name: 'signature_token', label: 'Токен подписи postback', kind: 'secret', hint: 'Пусто — используется API-токен' }
        ]
    },
    {
        key: 'pay.platega',
        title: 'Platega',
        webhook: 'platega',
        fields: [
            enabled,
            { name: 'merchant_id', label: 'Merchant ID', kind: 'text' },
            { name: 'secret', label: 'Секрет', kind: 'secret' },
            { name: 'payment_method', label: 'Метод', kind: 'number', hint: '2 СБП QR, 11 карты, 12 зарубежные карты, 13 крипта' },
            { name: 'api_version', label: 'Версия API', kind: 'select', options: ['v1', 'v2'] },
            { name: 'return_url', label: 'URL успеха', kind: 'text', hint: 'Пусто — в приложение' },
            { name: 'failed_url', label: 'URL ошибки', kind: 'text' }
        ]
    },
    {
        key: 'pay.stars',
        title: 'Telegram Stars',
        fields: [enabled, { name: 'rate', label: 'Базовой валюты за 1 ⭐', kind: 'number', decimal: true }]
    }
]
