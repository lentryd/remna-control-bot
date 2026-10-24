// AddonGroups — add-ons of a subscription in two groups: part of the tariff
// and connected separately, each as a card with its own traffic and reset info.
import { Button, Card, Group, Progress, Stack, Text } from '@mantine/core'
import { IconAlertTriangle, IconPlus } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtBytes, fmtDate, fmtMoney } from '@shared/format'
import type { Addon, Tariff } from '../types'
import { addonName } from './addonName'
import { RemainingInfo, ResetInfo } from './ResetInfo'
import { StateBadge } from './StateBadge'

const pct = (used: number, limit: number) => (limit > 0 ? Math.min(100, (used / limit) * 100) : 0)

const ts = (d: string | null) => (d ? new Date(d).getTime() : NaN)

// Name and description come from the add-on's tariff; an empty or missing one
// falls back to the add-on's own name so a card is never blank.
function cardTexts(a: Addon, tariffs: Tariff[]) {
    const tf = a.tariff_id != null ? tariffs.find((x) => x.id === a.tariff_id) : undefined
    const name = addonName(tf?.name, a.name)
    const description = tf?.description?.trim() || ''
    return { name, description }
}

function AddonCard({ a, tariffs, subscriptionExpireAt }: { a: Addon; tariffs: Tariff[]; subscriptionExpireAt: string | null }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const { name, description } = cardTexts(a, tariffs)
    // a separate add-on that ends before the subscription stops working earlier than the rest
    const endsBefore = !a.included && !a.unlimited && ts(a.expire_at) < ts(subscriptionExpireAt)
    return (
        <Card padding="md" radius="lg" withBorder>
            <Stack gap="xs">
                <Group align="flex-start" justify="space-between" wrap="nowrap">
                    <Stack gap={2}>
                        <Text fw={600}>{name}</Text>
                        {description && <Text c="dimmed" size="sm">{description}</Text>}
                    </Stack>
                    <StateBadge expireAt={a.expire_at} status={a.status} unlimited={a.unlimited} />
                </Group>
                <Group gap="xs" justify="space-between" wrap="nowrap">
                    <Text c="dimmed" size="sm">{a.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(a.expire_at) })}</Text>
                    {!a.included && <Text fw={600} size="sm">{t('sub.per_month', { price: fmtMoney(a.price) })}</Text>}
                </Group>
                {endsBefore && (
                    <Group c="yellow" gap={6} wrap="nowrap">
                        <IconAlertTriangle size={14} />
                        <Text c="yellow" size="xs">{t('addon_groups.ends_before', { date: fmtDate(a.expire_at) })}</Text>
                    </Group>
                )}
                <Stack gap={6}>
                    <Group justify="space-between">
                        <Text size="sm">{t('addon_groups.traffic')}</Text>
                        <Text c="dimmed" size="sm">{fmtBytes(a.used_bytes)} / {a.limit_bytes > 0 ? fmtBytes(a.limit_bytes) : '∞'}</Text>
                    </Group>
                    {a.limit_bytes > 0 && <Progress radius="xl" value={pct(a.used_bytes, a.limit_bytes)} />}
                </Stack>
                <ResetInfo expireAt={a.expire_at} limitBytes={a.limit_bytes} resetAt={a.reset_at} strategy={a.reset_strategy} unlimited={a.unlimited} variant="compact" />
                {!a.included && !a.unlimited && (
                    <>
                        <RemainingInfo expireAt={a.expire_at} unlimited={a.unlimited} variant="compact" />
                        <Button onClick={() => navigate(`/checkout?kind=renew&item_kind=addon&item_id=${a.id}`)} radius="xl" size="sm" variant="light">
                            {t('addon_groups.renew')}
                        </Button>
                    </>
                )}
            </Stack>
        </Card>
    )
}

export interface AddonGroupsProps {
    addons: Addon[]
    tariffs: Tariff[]
    subscriptionExpireAt: string | null
}

export function AddonGroups({ addons, tariffs, subscriptionExpireAt }: AddonGroupsProps) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const groups = [
        { key: 'included', title: t('addon_groups.included'), items: addons.filter((a) => a.included) },
        { key: 'separate', title: t('addon_groups.separate'), items: addons.filter((a) => !a.included) },
    ].filter((g) => g.items.length > 0)
    return (
        <Stack gap="md">
            {groups.map((g) => (
                <Stack gap="sm" key={g.key}>
                    <Text fw={600}>{g.title}</Text>
                    {g.items.map((a) => (
                        <AddonCard a={a} key={a.id} subscriptionExpireAt={subscriptionExpireAt} tariffs={tariffs} />
                    ))}
                </Stack>
            ))}
            <Button leftSection={<IconPlus size={18} />} onClick={() => navigate('/tariffs')} radius="xl" size="md" variant="default">
                {t('addon_groups.connect')}
            </Button>
        </Stack>
    )
}
