// RenewalSplit — what a month of renewal costs and how the balance covers
// it (BALANCE_PAY_MODE is applied on the server).
import { Group, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import type { Payment } from '../types'

function Row({ label, value, strong, color }: { label: string; value: number; strong?: boolean; color?: string }) {
    return (
        <Group justify="space-between">
            <Text c={strong ? undefined : 'dimmed'} fw={strong ? 600 : undefined} size="sm">{label}</Text>
            <Text c={color} fw={strong ? 700 : 500} size="sm">{fmtMoney(value, 2)}</Text>
        </Group>
    )
}

export function RenewalSplit({ r }: { r: Payment }) {
    const { t } = useTranslation()
    return (
        <SectionCard.Root gap="xs">
            <Stack gap={6}>
                <Row label={t('renew.month_price')} value={r.price} />
                {r.from_referral > 0 && <Row color="teal" label={t('renew.from_referral')} value={-r.from_referral} />}
                {r.from_own > 0 && <Row color="teal" label={t('renew.from_own')} value={-r.from_own} />}
            </Stack>
            {r.to_pay > 0 ? (
                <Row label={t('renew.to_pay')} strong value={r.to_pay} />
            ) : (
                <Text c="teal" fw={600} size="sm">{t('renew.covered')}</Text>
            )}
        </SectionCard.Root>
    )
}
