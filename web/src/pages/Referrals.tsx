// Referrals.tsx — link with copy/share, totals, every invitee with income.
import { Button, Group, Stack, Text, Title } from '@mantine/core'
import { IconSend, IconUsers } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { fmtMoney } from '@shared/format'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SectionCard } from '@shared/ui/section-card'
import { StatStrip } from '@shared/ui/stat-strip'
import { haptic, tg } from '../telegram'
import type { Profile } from '../types'
import { Empty } from '../ui/Empty'

export function Referrals({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const share = () =>
        tg?.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(p.ref_link)}&text=${encodeURIComponent(t('refs.share_text'))}`)
    return (
        <Stack gap="lg">
            <Title order={3}>{t('refs.title')}</Title>
            <div onClick={haptic}>
                <CopyableFieldShared label={t('refs.link')} size="md" value={p.ref_link} />
            </div>
            <Button leftSection={<IconSend size={18} />} onClick={share} radius="md" size="md">
                {t('refs.share')}
            </Button>
            <StatStrip
                items={[
                    { label: t('refs.invited'), value: p.referrals.length },
                    { label: t('refs.earned'), value: fmtMoney(p.referrals_earned, 2) }
                ]}
            />
            {p.referrals.length === 0 ? (
                <Empty hint={t('empty.refs_hint')} icon={<IconUsers />} title={t('empty.refs')} />
            ) : (
                <SectionCard.Root gap="sm">
                    {p.referrals.map((r, i) => (
                        <Group justify="space-between" key={i} wrap="nowrap">
                            <Stack gap={0}>
                                <Text c={r.active ? undefined : 'dimmed'} fw={500}>{r.name}</Text>
                                <Text c="dimmed" size="xs">
                                    {r.active ? t('refs.per_month', { sum: fmtMoney(r.monthly) }) : t('refs.inactive')} ·{' '}
                                    {t('refs.paid', { sum: fmtMoney(r.total_paid) })}
                                </Text>
                            </Stack>
                            <Text c="teal" fw={600}>+{fmtMoney(r.earned, 2)}</Text>
                        </Group>
                    ))}
                </SectionCard.Root>
            )}
        </Stack>
    )
}
