// Admin screens are for the operator and Russian-only; kept apart from the customer locale files.
export const A = {
    title: 'Админка',
    tabs: { orders: 'Заказы', methods: 'Оплата', general: 'Общее' },
    orders: {
        filters: { review: 'Ждут решения', all: 'Все', failed: 'Ошибки' },
        empty: 'Заказов нет',
        loadFailed: 'Не удалось загрузить заказы',
        retry: 'Повторить',
        prev: 'Назад',
        next: 'Вперёд',
        page: (n: number, of: number) => `Страница ${n} из ${of}`,
        unreachable: (ids: number[]) => `Уведомления не доходят до админов: ${ids.join(', ')}. Пусть нажмут /start в боте.`
    },
    order: {
        back: '← К заказам',
        customer: 'Клиент',
        method: 'Способ',
        comment: 'Комментарий клиента',
        receipt: 'Квитанция',
        receiptOpen: 'Открыть файл',
        amount: 'Получено',
        approve: 'Подтвердить',
        debt: 'Продлить в долг',
        credit: 'Только на баланс',
        reject: 'Отклонить',
        reason: 'Причина отказа (необязательно)',
        retry: 'Повторить',
        failed: 'Ошибка',
        decidedBy: (a: string) => `Решил: ${a}`,
        done: 'Заказ уже обработан',
        amountInvalid: 'Введите сумму больше нуля',
        loadFailed: 'Не удалось загрузить заказ',
        receiptFailed: 'Не удалось загрузить квитанцию',
        receiptAlt: 'Квитанция',
        errors: {
            underpaid: 'Получено меньше суммы заказа',
            state_changed: 'Заказ уже обработан',
            unavailable: 'Сервис временно недоступен, попробуйте ещё раз',
            not_found: 'Заказ не найден',
            bad_request: 'Некорректный запрос'
        } as Record<string, string>
    },
    status: {
        new: 'Черновик',
        awaiting_receipt: 'Ждёт квитанцию',
        awaiting_payment: 'Ждёт оплату',
        pending_review: 'На проверке',
        approving: 'Проводится',
        approved: 'Оплачен',
        credited: 'На балансе',
        failed: 'Ошибка',
        rejected: 'Отклонён',
        expired: 'Истёк',
        cancelled: 'Отменён'
    } as Record<string, string>,
    settings: {
        save: 'Сохранить',
        saved: 'Сохранено',
        test: 'Проверить',
        lost: 'Сохранённые ключи не расшифровались (изменился REF_SECRET). Введите их заново для:',
        webhook: 'Webhook URL',
        noCheck: 'Для этого способа проверка недоступна',
        notActive: 'Сначала включите способ и сохраните',
        loadFailed: 'Не удалось загрузить настройки',
        checkFailed: 'Проверка не прошла',
        badNumber: (label: string) => `Поле «${label}»: введите число`,
        badDays: (label: string) => `Поле «${label}»: целые положительные числа через запятую`
    }
}

// Readable text for an API error code; falls back to the server message or the code itself.
export const errMsg = (code: string, detail?: string) => A.order.errors[code] ?? detail ?? code
