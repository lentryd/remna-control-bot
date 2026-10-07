// SubscriptionDetails.tsx — everything about one subscription.
import { Button, Group, Progress, Stack, Text, Title } from '@mantine/core'
import { IconBolt, IconExternalLink } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { Navigate, useNavigate, useParams } from 'react-router'

import { fmtBytes, fmtDate, fmtMoney } from '@shared/format'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SectionCard } from '@shared/ui/section-card'
import { checkoutRenew, isExpired } from '../renew'
import { haptic, openExternal } from '../telegram'
import type { Profile } from '../types'
import { CtaButton } from '../ui/CtaButton'
import { RenewalSplit } from '../ui/RenewalSplit'
import { StateBadge } from '../ui/StateBadge'

const pct = (used: number, limit: number) => (limit > 0 ? Math.min(100, (used / limit) * 100) : 0)

export function SubscriptionDetails({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const { id } = useParams()
    const navigate = useNavigate()
    const s = p.subscriptions.find((x) => x.id === Number(id))
    if (!s) return <Navigate replace to="/subscriptions" />
    const until = s.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(s.expire_at) })
    return (
        <Stack gap="lg">
            <Group justify="space-between" wrap="nowrap">
                <Title order={3}>{s.title}</Title>
                <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
            </Group>
            <SectionCard.Root>
                <Group justify="space-between">
                    <Text c="dimmed">{s.tariff}</Text>
                    <Text fw={600}>{t('sub.per_month', { price: fmtMoney(s.price) })}</Text>
                </Group>
                <Text fw={500}>{until}</Text>
                <Stack gap={6}>
                    <Group justify="space-between">
                        <Text size="sm">{t('sub.traffic')}</Text>
                        <Text c="dimmed" size="sm">
                            {fmtBytes(s.used_bytes)} / {s.limit_bytes > 0 ? fmtBytes(s.limit_bytes) : '∞'}
                        </Text>
                    </Group>
                    {s.limit_bytes > 0 && <Progress radius="xl" value={pct(s.used_bytes, s.limit_bytes)} />}
                </Stack>
                {s.device_limit !== null && <Text size="sm">{t('sub.devices', { n: s.device_limit })}</Text>}
            </SectionCard.Root>
            {!s.unlimited && s.renewal.price > 0 && <RenewalSplit r={s.renewal} />}
            {!s.unlimited && (
                <CtaButton fullWidth leftSection={<IconBolt size={20} />} onClick={() => navigate(checkoutRenew(s.id))} size="lg">
                    {isExpired(s) ? t('renew.button_expired') : t('renew.button')}
                </CtaButton>
            )}
            {!s.unlimited && (
                <Button
                    fullWidth
                    onClick={() => navigate(`/checkout?kind=change_tariff&item_kind=subscription&item_id=${s.id}`)}
                    radius="xl"
                    size="md"
                    variant="default"
                >
                    {t('sub.change_tariff')}
                </Button>
            )}
            {s.url && (
                <Stack gap="xs">
                    <div onClick={haptic}>
                        <CopyableFieldShared label={t('sub.link')} size="md" value={s.url} />
                    </div>
                    <Button leftSection={<IconExternalLink size={18} />} onClick={() => openExternal(s.url)} radius="md" size="md" variant="light">
                        {t('sub.open_page')}
                    </Button>
                    <Text c="dimmed" size="xs" ta="center">{t('sub.open_hint')}</Text>
                </Stack>
            )}
            {s.addons.length > 0 && (
                <Stack gap="sm">
                    <Text fw={600}>{t('sub.addons')}</Text>
                    <SectionCard.Root>
                        {s.addons.map((a) => (
                            <Group justify="space-between" key={a.id} wrap="nowrap">
                                <Stack gap={0}>
                                    <Text fw={500}>{a.name}</Text>
                                    <Text c="dimmed" size="xs">
                                        {a.unlimited ? t('sub.forever') : t('sub.until', { date: fmtDate(a.expire_at) })}
                                        {a.included ? ` · ${t('sub.included')}` : ` · ${t('sub.per_month', { price: fmtMoney(a.price) })}`}
                                    </Text>
                                </Stack>
                                <StateBadge expireAt={a.expire_at} status={a.status} unlimited={a.unlimited} />
                            </Group>
                        ))}
                    </SectionCard.Root>
                </Stack>
            )}
        </Stack>
    )
}
