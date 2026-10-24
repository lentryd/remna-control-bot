// Tariffs.tsx — the catalog as cards worth tapping: price up front, what's
// included, savings for longer terms and a bright "connect" button.
import { Badge, Card, Group, List, Modal, SegmentedControl, Select, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconBolt, IconCheck, IconDevices, IconGauge, IconGift, IconPuzzle } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { fmtBytes, fmtMoney } from '@shared/format'
import { useProfile, useTariffs } from '../api'
import { bestPeriod, termKey } from '../pricing'
import type { Tariff } from '../types'
import { changeCandidates, checkoutChange } from '../renew'
import { term } from '../term'
import { CtaButton } from '../ui/CtaButton'
import { Empty } from '../ui/Empty'
import { OldPrice } from '../ui/OldPrice'
import { PageSkeleton } from '../ui/PageSkeleton'
import classes from './tariffs.module.css'

function TariffCard({ tf }: { tf: Tariff }) {
    const { t } = useTranslation()
    const best = bestPeriod(tf)
    const addon = tf.kind === 'addon'
    const navigate = useNavigate()
    const profile = useProfile()
    const [choosing, setChoosing] = useState(false)
    const [choice, setChoice] = useState<'new' | 'update'>('update')
    const [picked, setPicked] = useState<string | null>(null)
    // while the profile loads (or fails) the plain CTA stays
    const candidates = !addon && profile.data ? changeCandidates(profile.data.subscriptions, tf.id, tf.name) : []
    const newUrl = `/checkout?kind=${addon ? 'addon' : 'subscribe'}&tariff_id=${tf.id}${best ? `&term=${termKey(best.period)}` : ''}`
    const subId = candidates.find((s) => String(s.id) === picked)?.id ?? candidates[0]?.id
    const go = () => {
        setChoosing(false)
        navigate(choice === 'update' && subId != null ? checkoutChange(subId, tf.id) : newUrl)
    }
    return (
        <Card className={classes.card} data-addon={addon || undefined} padding="lg" radius="lg" withBorder>
            <Stack gap="md">
                <Group align="flex-start" justify="space-between" wrap="nowrap">
                    <Stack gap={2} style={{ minWidth: 0 }}>
                        {addon && <Text c="dimmed" size="xs" tt="uppercase">{t('tariffs.addon')}</Text>}
                        <Text fw={700} size="xl" truncate="end">{tf.name}</Text>
                    </Stack>
                    {best && (
                        <Badge className={classes.save} color="teal" radius="sm" size="lg" variant="light">
                            {t('tariffs.save_up_to', { pct: best.pct })}
                        </Badge>
                    )}
                </Group>

                <Stack gap={4}>
                    <Group align="stretch" gap={8} wrap="nowrap">
                        <Text className={classes.price}>{fmtMoney(best ? best.perMonth : tf.monthly_price)}</Text>
                        <Stack gap={0} justify={best ? 'space-between' : 'flex-end'}>
                            {best && <OldPrice size="sm">{fmtMoney(tf.monthly_price)}</OldPrice>}
                            <Text c="dimmed">{t('tariffs.per_month')}</Text>
                        </Stack>
                    </Group>
                    {best && (
                        <Text c="dimmed" size="xs">
                            {t('tariffs.best_period', { term: term(best.period, t) })}
                            {' · '}
                            <Text c="teal" component="span" fw={700} size="xs">{t('tariffs.saving', { sum: fmtMoney(best.saved) })}</Text>
                        </Text>
                    )}
                </Stack>

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

                {(tf.includes ?? []).length > 0 && (
                    <Stack className={classes.includes} gap={8}>
                        <Group gap={6}>
                            <IconGift color="var(--mantine-color-violet-filled)" size={16} />
                            <Text c="violet" fw={700} size="xs" tt="uppercase">{t('tariffs.includes_title', { count: tf.includes.length })}</Text>
                        </Group>
                        {tf.includes.map((a) => (
                            <Group gap="xs" key={a.name} wrap="nowrap">
                                <ThemeIcon color="violet" radius="xl" size={22} variant="light"><IconPuzzle size={13} /></ThemeIcon>
                                <Stack gap={0} style={{ minWidth: 0 }}>
                                    <Text fw={600} size="sm" truncate="end">{a.name}</Text>
                                    <Text c="dimmed" size="xs">
                                        {a.traffic_limit_bytes > 0 ? t('tariffs.traffic', { size: fmtBytes(a.traffic_limit_bytes) }) : t('tariffs.traffic_unlimited')}
                                    </Text>
                                </Stack>
                            </Group>
                        ))}
                    </Stack>
                )}

                <CtaButton fullWidth leftSection={<IconBolt size={20} />} onClick={() => (candidates.length > 0 ? setChoosing(true) : navigate(newUrl))} size="lg">
                    {t('tariffs.connect')}
                </CtaButton>
                {candidates.length > 0 && (
                    <Modal centered onClose={() => setChoosing(false)} opened={choosing} title={tf.name}>
                        <Stack gap="md">
                            <SegmentedControl
                                data={[
                                    { value: 'update', label: t('tariffs.choice_update') },
                                    { value: 'new', label: t('tariffs.choice_new') },
                                ]}
                                fullWidth
                                onChange={(v) => setChoice(v as 'new' | 'update')}
                                value={choice}
                            />
                            {choice === 'update' ? (
                                <>
                                    <Text c="dimmed" size="sm">{t('tariffs.update_hint')}</Text>
                                    <Select
                                        allowDeselect={false}
                                        data={candidates.map((s) => ({ value: String(s.id), label: `${s.title} · ${s.tariff}` }))}
                                        label={t('tariffs.pick_subscription')}
                                        onChange={setPicked}
                                        radius="md"
                                        value={subId != null ? String(subId) : null}
                                    />
                                </>
                            ) : (
                                <Text c="dimmed" size="sm">{t('tariffs.new_hint')}</Text>
                            )}
                            <CtaButton fullWidth onClick={go} size="lg">{t('tariffs.continue')}</CtaButton>
                        </Stack>
                    </Modal>
                )}
            </Stack>
        </Card>
    )
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
