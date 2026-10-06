// SubscriptionRow — tappable card: title, until when, status.
import { Card, Group, Stack, Text, UnstyledButton } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtDate } from '@shared/format'
import type { Subscription } from '../types'
import { StateBadge } from './StateBadge'

export function SubscriptionRow({ s }: { s: Subscription }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    return (
        <UnstyledButton onClick={() => navigate(`/subscriptions/${s.id}`)}>
            <Card padding="md" radius="lg" withBorder>
                <Group justify="space-between" wrap="nowrap">
                    <Stack gap={2}>
                        <Text fw={600}>{s.title}</Text>
                        <Text c="dimmed" size="sm">
                            {s.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(s.expire_at) })}
                            {s.addons.length > 0 && ` · ${t('sub.addons_count', { count: s.addons.length })}`}
                        </Text>
                    </Stack>
                    <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
                </Group>
            </Card>
        </UnstyledButton>
    )
}
