// SubscriptionRow — tappable card: title and status, remaining time and limit
// reset, traffic used / limit with a thin bar, tariff and add-on count.
import { Card, Group, Progress, SimpleGrid, Stack, Text, UnstyledButton } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtBytes } from '@shared/format'
import { isExpired } from '../renew'
import type { Subscription } from '../types'
import { RemainingInfo, ResetInfo } from './ResetInfo'
import { StateBadge } from './StateBadge'

export const trafficPct = (used: number, limit: number) => (limit > 0 ? Math.min(100, (used / limit) * 100) : 0)

// Default colour normally, amber from 80%, red when the limit is used up.
export const trafficColor = (pct: number) => (pct >= 100 ? 'red' : pct >= 80 ? 'yellow' : undefined)

export function SubscriptionRow({ s }: { s: Subscription }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const showBar = !s.unlimited && s.limit_bytes > 0
    const expired = isExpired(s)
    const pct = trafficPct(s.used_bytes, s.limit_bytes)
    return (
        <UnstyledButton onClick={() => navigate(`/subscriptions/${s.id}`)}>
            <Card padding="md" radius="lg" withBorder>
                <Stack gap="xs">
                    <Group justify="space-between" wrap="nowrap">
                        <Text fw={600}>{s.title}</Text>
                        <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
                    </Group>
                    <SimpleGrid cols={2} spacing="sm">
                        <RemainingInfo expireAt={s.expire_at} unlimited={s.unlimited} variant="stack" />
                        {!expired && (
                            <ResetInfo
                            expireAt={s.expire_at}
                            limitBytes={s.limit_bytes}
                            resetAt={s.reset_at}
                            strategy={s.reset_strategy}
                            unlimited={s.unlimited}
                            variant="stack"
                            />
                        )}
                    </SimpleGrid>
                    <Group justify="space-between" wrap="nowrap">
                        <Text c="dimmed" size="xs">
                            {fmtBytes(s.used_bytes)} / {showBar ? fmtBytes(s.limit_bytes) : '∞'}
                        </Text>
                        <Text c="dimmed" lineClamp={1} size="xs" ta="right">
                            {s.tariff}
                            {s.addons.length > 0 && ` · ${t('sub.addons_count', { count: s.addons.length })}`}
                        </Text>
                    </Group>
                    {showBar && <Progress color={trafficColor(pct)} radius="xl" size="xs" value={pct} />}
                </Stack>
            </Card>
        </UnstyledButton>
    )
}
