// Tariffs.tsx — the catalog as cards worth tapping: price up front, what's
// included, savings for longer terms and a bright "connect" button.
import { Badge, Card, Group, List, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconBolt, IconCheck, IconDevices, IconGauge, IconPuzzle } from '@tabler/icons-react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import { fmtBytes, fmtMoney } from '@shared/format'
import { useTariffs } from '../api'
import { pay } from '../renew'
import type { Period, Tariff } from '../types'
import { CtaButton } from '../ui/CtaButton'
import { Empty } from '../ui/Empty'
import { PageSkeleton } from '../ui/PageSkeleton'
import classes from './tariffs.module.css'

// saving of a period against paying monthly, in whole percent
const saving = (tf: Tariff, p: Period) => {
    const months = p.months + p.days / 30
    const full = tf.monthly_price * months
    return full > 0 ? Math.round((1 - p.price / full) * 100) : 0
}

function TariffCard({ tf }: { tf: Tariff }) {
    const { t } = useTranslation()
    const periods = (tf.periods ?? []).filter((p) => p.months > 0 || p.days > 0)
    const best = Math.max(0, ...periods.map((p) => saving(tf, p)))
    const addon = tf.kind === 'addon'
    return (
        <Card className={classes.card} data-addon={addon || undefined} padding="lg" radius="lg" withBorder>
            <Stack gap="md">
                <Group align="flex-start" justify="space-between" wrap="nowrap">
                    <Stack gap={2} style={{ minWidth: 0 }}>
                        {addon && tf.addon_name && <Text c="dimmed" size="xs" tt="uppercase">{t('tariffs.addon')}</Text>}
                        <Text fw={700} size="xl" truncate="end">{addon && tf.addon_name ? tf.addon_name : tf.name}</Text>
                    </Stack>
                    {best > 0 && (
                        <Badge className={classes.save} color="teal" radius="sm" size="lg" variant="light">
                            {t('tariffs.save_up_to', { pct: best })}
                        </Badge>
                    )}
                </Group>

                <Group align="baseline" gap={6}>
                    <Text className={classes.price}>{fmtMoney(tf.monthly_price)}</Text>
                    <Text c="dimmed">{t('tariffs.per_month')}</Text>
                </Group>

                {tf.description && <Text size="sm">{tf.description}</Text>}

                <List center icon={<ThemeIcon color="teal" radius="xl" size={20} variant="light"><IconCheck size={13} /></ThemeIcon>} spacing={6} size="sm">
                    {addon ? (
                        <List.Item icon={<ThemeIcon color="violet" radius="xl" size={20} variant="light"><IconPuzzle size={13} /></ThemeIcon>}>
                            {t('tariffs.addon_hint')}
                        </List.Item>
                    ) : null}
                    <List.Item icon={<ThemeIcon radius="xl" size={20} variant="light"><IconGauge size={13} /></ThemeIcon>}>
                        {tf.traffic_limit_bytes > 0 ? t('tariffs.traffic', { size: fmtBytes(tf.traffic_limit_bytes) }) : t('tariffs.traffic_unlimited')}
                    </List.Item>
                    {tf.hwid_limit !== null && (
                        <List.Item icon={<ThemeIcon radius="xl" size={20} variant="light"><IconDevices size={13} /></ThemeIcon>}>
                            {t('tariffs.devices', { count: tf.hwid_limit })}
                        </List.Item>
                    )}
                </List>

                {periods.length > 0 && (
                    <Group gap={6}>
                        {periods.map((p, i) => {
                            const pct = saving(tf, p)
                            return (
                                <Badge className={classes.period} color="gray" key={i} radius="sm" size="lg" variant="light">
                                    {t('tariffs.period', { term: term(p, t), price: fmtMoney(p.price) })}
                                    {pct > 0 && <Text c="teal" component="span" fw={700} size="xs"> −{pct}%</Text>}
                                </Badge>
                            )
                        })}
                    </Group>
                )}

                <CtaButton fullWidth leftSection={<IconBolt size={20} />} onClick={pay} size="lg">
                    {t('tariffs.connect')}
                </CtaButton>
            </Stack>
        </Card>
    )
}

function term(p: Period, t: TFunction) {
    return [p.months && t('tariffs.months', { count: p.months }), p.days && t('tariffs.days', { count: p.days })].filter(Boolean).join(' ')
}

export function Tariffs() {
    const { t } = useTranslation()
    const q = useTariffs()
    if (q.isPending) return <PageSkeleton />
    const all = q.data ?? []
    const base = all.filter((tf) => tf.kind === 'base')
    const addons = all.filter((tf) => tf.kind === 'addon')
    return (
        <Stack gap="lg">
            <Stack gap={4}>
                <Title order={3}>{t('tariffs.title')}</Title>
                <Text c="dimmed" size="sm">{t('tariffs.subtitle')}</Text>
            </Stack>
            {all.length === 0 && <Empty icon={<IconBolt />} title={t('tariffs.empty')} />}
            {base.map((tf) => (
                <TariffCard key={tf.id} tf={tf} />
            ))}
            {addons.length > 0 && (
                <>
                    <Stack gap={4} mt="sm">
                        <Title order={4}>{t('tariffs.addons_title')}</Title>
                        <Text c="dimmed" size="sm">{t('tariffs.addons_subtitle')}</Text>
                    </Stack>
                    {addons.map((tf) => (
                        <TariffCard key={tf.id} tf={tf} />
                    ))}
                </>
            )}
        </Stack>
    )
}
