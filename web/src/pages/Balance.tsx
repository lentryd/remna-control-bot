// Balance.tsx — totals and full history grouped by month.
import { Box, Button, Card, Group, SimpleGrid, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconPlus, IconReceipt, IconWallet } from '@tabler/icons-react'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'

import { fmtDate, fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { pay } from '../renew'
import type { LedgerEntry, Profile } from '../types'
import { Empty } from '../ui/Empty'
import classes from './balance.module.css'

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
            <Card padding="lg" radius="lg" withBorder>
                <Group justify="space-between" wrap="nowrap">
                    <Stack gap={2}>
                        <Text c="dimmed" size="sm">{t('balance.available')}</Text>
                        <Text fw={700} lh={1.1} size="32px">{fmtMoney(p.balance, 2)}</Text>
                    </Stack>
                    <ThemeIcon radius="xl" size={52} variant="light">
                        <IconWallet size={28} />
                    </ThemeIcon>
                </Group>
                <SimpleGrid cols={2} mt="md" spacing="sm">
                    <Box className={classes.part}>
                        <Text c="dimmed" size="xs">{t('balance.own')}</Text>
                        <Text fw={600}>{fmtMoney(p.balance_own, 2)}</Text>
                    </Box>
                    <Box className={classes.part}>
                        <Text c="dimmed" size="xs">{t('balance.referral')}</Text>
                        <Text c={p.balance_referral > 0 ? 'teal' : undefined} fw={600}>{fmtMoney(p.balance_referral, 2)}</Text>
                    </Box>
                </SimpleGrid>
                <Button fullWidth leftSection={<IconPlus size={18} />} mt="md" onClick={pay} radius="md" size="md">
                    {t('balance.top_up')}
                </Button>
            </Card>
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
