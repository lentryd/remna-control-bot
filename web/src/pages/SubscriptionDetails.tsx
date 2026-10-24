// SubscriptionDetails.tsx — everything about one subscription.
import { Button, Group, Progress, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { IconArrowsExchange, IconBolt, IconExternalLink } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { Navigate, useNavigate, useParams } from 'react-router'

import { fmtBytes, fmtMoney } from '@shared/format'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SectionCard } from '@shared/ui/section-card'
import { useTariffs } from '../api'
import { checkoutRenew, isExpired } from '../renew'
import { haptic, openExternal } from '../telegram'
import type { Profile } from '../types'
import { AddonGroups } from '../ui/AddonGroups'
import { CtaButton } from '../ui/CtaButton'
import { RemainingInfo, ResetInfo } from '../ui/ResetInfo'
import { RenewalSplit } from '../ui/RenewalSplit'
import { StateBadge } from '../ui/StateBadge'
import { trafficColor, trafficPct } from '../ui/SubscriptionRow'

export function SubscriptionDetails({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const { id } = useParams()
    const navigate = useNavigate()
    const { data: tariffs } = useTariffs()
    const s = p.subscriptions.find((x) => x.id === Number(id))
    if (!s) return <Navigate replace to="/subscriptions" />
    const showBar = !s.unlimited && s.limit_bytes > 0
    const pct = trafficPct(s.used_bytes, s.limit_bytes)
    return (
        <Stack gap="lg">
            <Group justify="space-between" wrap="nowrap">
                <Title order={3}>{s.title}</Title>
                <StateBadge expireAt={s.expire_at} status={s.status} unlimited={s.unlimited} />
            </Group>
            <SimpleGrid cols={2} spacing="sm">
                <RemainingInfo expireAt={s.expire_at} unlimited={s.unlimited} variant="tile" />
                <ResetInfo
                    expireAt={s.expire_at}
                    limitBytes={s.limit_bytes}
                    resetAt={s.reset_at}
                    strategy={s.reset_strategy}
                    unlimited={s.unlimited}
                    variant="tile"
                />
            </SimpleGrid>
            <SectionCard.Root>
                <Stack gap={6}>
                    <Group justify="space-between">
                        <Text size="sm">{t('sub.traffic')}</Text>
                        <Text c="dimmed" size="sm">
                            {fmtBytes(s.used_bytes)} / {showBar ? fmtBytes(s.limit_bytes) : '∞'}
                        </Text>
                    </Group>
                    {showBar && <Progress color={trafficColor(pct)} radius="xl" value={pct} />}
                </Stack>
                {s.device_limit !== null && s.device_limit > 0 && <Text size="sm">{t('sub.devices', { n: s.device_limit })}</Text>}
                <Group justify="space-between">
                    <Text c="dimmed">{s.tariff}</Text>
                    <Text fw={600}>{t('sub.per_month', { price: fmtMoney(s.price) })}</Text>
                </Group>
            </SectionCard.Root>
            {s.unlimited && <Text c="dimmed" size="sm">{t('sub.unlimited_note')}</Text>}
            {!s.unlimited && s.renewal.price > 0 && <RenewalSplit r={s.renewal} />}
            {!s.unlimited && (
                <CtaButton fullWidth leftSection={<IconBolt size={20} />} onClick={() => navigate(checkoutRenew(s.id))} size="lg">
                    {isExpired(s) ? t('renew.button_expired') : t('renew.button')}
                </CtaButton>
            )}
            {!s.unlimited && (
                <Button
                    fullWidth
                    leftSection={<IconArrowsExchange size={20} />}
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
            <AddonGroups addons={s.addons} subscriptionExpireAt={s.expire_at} tariffs={tariffs ?? []} />
        </Stack>
    )
}
