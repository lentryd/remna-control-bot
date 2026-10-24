// ResetInfo / RemainingInfo — "when does the limit reset" and "how long is left",
// shared by the subscription list, details page and add-on cards.
import { Group, Paper, Stack, Text } from '@mantine/core'
import { IconCalendar, IconRefresh } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { daysUntil, fmtDate } from '@shared/format'

export type InfoVariant = 'compact' | 'stack' | 'tile'

// Panel strategy meaning "the counter only resets when the subscription renews".
const NO_RESET = 'NO_RESET'

function Line({ icon, label, value, sub, variant }: { icon: ReactNode; label: string; value: string; sub?: string; variant: InfoVariant }) {
    if (variant === 'tile') {
        return (
            <Paper p="sm" radius="md" withBorder>
                <Stack gap={2}>
                    <Group c="dimmed" gap={6} wrap="nowrap">
                        {icon}
                        <Text c="dimmed" size="xs">{label}</Text>
                    </Group>
                    <Text fw={700} lh={1.25} size="md" style={{ overflowWrap: 'anywhere' }}>{value}</Text>
                    {sub && <Text c="dimmed" size="xs">{sub}</Text>}
                </Stack>
            </Paper>
        )
    }
    if (variant === 'stack') {
        return (
            <Stack gap={2} miw={0}>
                <Group c="dimmed" gap={6} wrap="nowrap">
                    {icon}
                    <Text c="dimmed" size="xs">{label}</Text>
                </Group>
                <Text fw={600} size="sm" style={{ overflowWrap: 'anywhere' }}>{sub ? `${value} · ${sub}` : value}</Text>
            </Stack>
        )
    }
    return (
        <Group c="dimmed" gap={6} wrap="nowrap">
            {icon}
            <Text c="dimmed" size="xs">{label}: {sub ? `${value} · ${sub}` : value}</Text>
        </Group>
    )
}

export interface ResetInfoProps {
    expireAt: string | null
    resetAt: string | null
    strategy: string
    limitBytes: number
    variant: InfoVariant
}

// "No limit" only when there is no traffic limit: a forever term with a real
// limit still resets it. Renders nothing when the reset moment is unknown (no date, or an empty
// strategy from an old panel): better no line than an invented one.
export function ResetInfo({ resetAt, strategy, limitBytes, variant }: ResetInfoProps) {
    const { t } = useTranslation()
    let value: string | null = null
    let sub: string | undefined
    if (limitBytes <= 0) {
        value = t('info.reset_none')
    } else if (strategy === NO_RESET) {
        value = t('info.reset_after_renewal')
    } else if (strategy) {
        const days = daysUntil(resetAt)
        if (days !== null) {
            value = days < 1 ? t('info.reset_today') : t('info.reset_in_days', { count: days })
            if (days >= 1) sub = fmtDate(resetAt)
        }
    }
    if (value === null) return null
    return <Line icon={<IconRefresh size={14} />} label={t('info.reset_label')} value={value} sub={sub} variant={variant} />
}

export interface RemainingInfoProps {
    expireAt: string | null
    unlimited: boolean
    variant: InfoVariant
}

// Days left plus the end date, "forever" for unlimited; an expired date shows
// the expired wording, never a negative number.
export function RemainingInfo({ expireAt, unlimited, variant }: RemainingInfoProps) {
    const { t } = useTranslation()
    let value: string
    let sub: string | undefined
    if (unlimited) {
        value = t('info.remaining_forever')
    } else {
        const days = daysUntil(expireAt)
        if (days === null) return null
        if (days < 0) value = t('info.remaining_expired')
        else if (days < 1) value = t('info.remaining_today')
        else {
            value = t('info.remaining_days', { count: days })
            sub = t('info.remaining_until', { date: fmtDate(expireAt) })
        }
    }
    return <Line icon={<IconCalendar size={14} />} label={t('info.remaining_label')} value={value} sub={sub} variant={variant} />
}
