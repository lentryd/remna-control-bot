// term: a tariff period as text, e.g. "3 mo" or "1 mo 15 d".
import type { TFunction } from 'i18next'

import type { Period } from './types'

export function term(p: Pick<Period, 'months' | 'days'>, t: TFunction) {
    return [p.months && t('tariffs.months', { count: p.months }), p.days && t('tariffs.days', { count: p.days })].filter(Boolean).join(' ')
}
