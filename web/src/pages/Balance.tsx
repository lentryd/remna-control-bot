// Balance.tsx — totals and full history grouped by month.
import { Group, Stack, Text, Title } from '@mantine/core'
import { IconReceipt } from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'

import { fmtDate, fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { StatStrip } from '@shared/ui/stat-strip'
import type { LedgerEntry, Profile } from '../types'
import { Empty } from '../ui/Empty'

export function Balance({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const months = new Map<string, LedgerEntry[]>()
    for (const e of p.ledger) {
        const key = dayjs(e.date).format('MMMM YYYY')
        months.set(key, [...(months.get(key) ?? []), e])
    }
    return (
        <Stack gap="lg">
            <Title order={3}>{t('balance.title')}</Title>
            <StatStrip
                items={[
                    { label: t('balance.title'), value: fmtMoney(p.balance, 2) },
                    { label: t('balance.own'), value: fmtMoney(p.balance_own, 2) },
                    { label: t('balance.referral'), value: fmtMoney(p.balance_referral, 2) }
                ]}
            />
            <Text fw={600}>{t('balance.history')}</Text>
            {p.ledger.length === 0 ? (
                <Empty icon={<IconReceipt />} title={t('empty.ledger')} />
            ) : (
                [...months].map(([month, items]) => (
                    <Stack gap="xs" key={month}>
                        <Text c="dimmed" size="sm" tt="capitalize">{month}</Text>
                        <SectionCard.Root gap="sm">
                            {items.map((e, i) => (
                                <Group justify="space-between" key={i} wrap="nowrap">
                                    <Stack gap={0}>
                                        <Text size="sm">{t(`balance.types.${e.type}`, { defaultValue: e.type })}</Text>
                                        <Text c="dimmed" size="xs">{fmtDate(e.date)}</Text>
                                    </Stack>
                                    <Text c={e.amount >= 0 ? 'teal' : undefined} fw={600}>
                                        {e.amount > 0 ? '+' : ''}
                                        {fmtMoney(e.amount, 2)}
                                    </Text>
                                </Group>
                            ))}
                        </SectionCard.Root>
                    </Stack>
                ))
            )}
        </Stack>
    )
}
