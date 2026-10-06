// Tariffs.tsx — catalog; paying is not available yet.
import { Badge, Button, Card, Group, Stack, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { useTariffs } from '../api'
import { fmtBytes, fmtMoney } from '@shared/format'
import { PageSkeleton } from '../ui/PageSkeleton'

const term = (m: number, d: number) => [m && `${m} мес`, d && `${d} дн`].filter(Boolean).join(' ')

export function Tariffs() {
    const { t } = useTranslation()
    const q = useTariffs()
    if (q.isPending) return <PageSkeleton />
    return (
        <Stack gap="lg">
            <Title order={3}>{t('tariffs.title')}</Title>
            {(q.data ?? []).map((tf) => (
                <Card key={tf.id} padding="lg" radius="lg" withBorder>
                    <Stack gap="xs">
                        <Group justify="space-between" wrap="nowrap">
                            <Text fw={700} size="lg">{tf.kind === 'addon' && tf.addon_name ? `${tf.addon_name} · ${tf.name}` : tf.name}</Text>
                            {tf.kind === 'addon' && <Badge variant="light">{t('tariffs.addon')}</Badge>}
                        </Group>
                        <Text>
                            <Text fw={700} size="xl" span>{fmtMoney(tf.monthly_price)}</Text>{' '}
                            <Text c="dimmed" span>{t('tariffs.per_month')}</Text>
                        </Text>
                        {tf.description && <Text c="dimmed" size="sm">{tf.description}</Text>}
                        <Group gap="xs">
                            {tf.traffic_limit_bytes > 0 && <Badge color="gray" variant="light">{fmtBytes(tf.traffic_limit_bytes)}</Badge>}
                            {tf.hwid_limit !== null && <Badge color="gray" variant="light">≤ {tf.hwid_limit} устр.</Badge>}
                        </Group>
                        {tf.periods && tf.periods.length > 0 && (
                            <Text c="dimmed" size="xs">
                                {t('tariffs.periods')} {tf.periods.map((p) => `${term(p.months, p.days)} — ${fmtMoney(p.price)}`).join(', ')}
                            </Text>
                        )}
                        <Button disabled mt="xs" radius="md" variant="light">{t('tariffs.pay_soon')}</Button>
                    </Stack>
                </Card>
            ))}
        </Stack>
    )
}
