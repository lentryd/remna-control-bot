// BalanceCard — the home screen's top block: balance, what it costs per
// month, and a renewal call-to-action for the most urgent subscription.
import { Card, Divider, Group, Stack, Text, ThemeIcon } from '@mantine/core'
import { IconAlertTriangle, IconBolt, IconClockHour4, IconWallet } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtDate, fmtMoney } from '@shared/format'
import { daysLeftText, isExpired, needsRenewal, checkoutRenew } from '../renew'
import type { Profile } from '../types'
import { CtaButton } from './CtaButton'

export function BalanceCard({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    // subscriptions come sorted by urgency
    const urgent = p.subscriptions.find(needsRenewal)
    const expired = urgent && isExpired(urgent)
    return (
        <Card padding="lg" radius="lg" withBorder>
            <Group justify="space-between" wrap="nowrap">
                <Stack gap={2}>
                    <Text c="dimmed" size="sm">{t('overview.balance')}</Text>
                    <Text fw={700} lh={1.1} size="32px">{fmtMoney(p.balance, 2)}</Text>
                    {p.monthly > 0 && (
                        <Text c="dimmed" size="sm">{t('overview.monthly_line', { sum: fmtMoney(p.monthly) })}</Text>
                    )}
                </Stack>
                <ThemeIcon radius="xl" size={52} variant="light">
                    <IconWallet size={28} />
                </ThemeIcon>
            </Group>
            {urgent && (
                <>
                    <Divider my="md" />
                    <Group gap="sm" justify="space-between" wrap="nowrap">
                        <Group gap="sm" style={{ minWidth: 0 }} wrap="nowrap" onClick={() => navigate(`/subscriptions/${urgent.id}`)}>
                            <ThemeIcon color={expired ? 'red' : 'yellow'} radius="xl" size={36} variant="light">
                                {expired ? <IconAlertTriangle size={20} /> : <IconClockHour4 size={20} />}
                            </ThemeIcon>
                            <Stack gap={0} style={{ minWidth: 0 }}>
                                <Text fw={600} truncate="end">{urgent.title}</Text>
                                <Text c={expired ? 'red' : 'yellow.7'} size="sm">
                                    {expired ? t('renew.expired', { date: fmtDate(urgent.expire_at) }) : daysLeftText(urgent)}
                                </Text>
                            </Stack>
                        </Group>
                        <CtaButton leftSection={<IconBolt size={18} />} onClick={() => navigate(checkoutRenew(urgent.id))}>
                            {t('renew.button')}
                        </CtaButton>
                    </Group>
                </>
            )}
            {!urgent && p.nearest_expire_at && (
                <>
                    <Divider my="md" />
                    <Text c="dimmed" size="sm">{t('overview.all_good', { date: fmtDate(p.nearest_expire_at) })}</Text>
                </>
            )}
        </Card>
    )
}
