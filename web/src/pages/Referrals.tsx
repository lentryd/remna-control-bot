// Referrals.tsx — figures, the invite card (link, share, copy) and every
// invitee with what they brought, in the app's usual cards.
import { Button, CopyButton, Group, Stack, Text, Title } from '@mantine/core'
import { IconCheck, IconCopy, IconSend, IconUsers } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { StatStrip } from '@shared/ui/stat-strip'
import { haptic, tg } from '../telegram'
import type { Profile } from '../types'
import { Empty } from '../ui/Empty'
import classes from './referrals.module.css'

export function Referrals({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const share = () =>
        tg?.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(p.ref_link)}&text=${encodeURIComponent(t('refs.share_text'))}`)
    return (
        <Stack gap="lg">
            <Title order={3}>{t('refs.title')}</Title>

            <StatStrip
                items={[
                    { label: t('refs.earned'), value: fmtMoney(p.referrals_earned, 2), color: p.referrals_earned > 0 ? 'teal' : undefined },
                    { label: t('refs.friends'), value: p.referrals.length }
                ]}
            />

            <SectionCard.Root gap="sm">
                <Stack gap={4}>
                    <Text fw={600}>{t('refs.invite_title')}</Text>
                    <Text c="dimmed" size="sm">{t('refs.pitch')}</Text>
                </Stack>
                <Stack gap="sm">
                    <Text className={classes.link} ff="monospace" size="sm" truncate="end">
                        {p.ref_link.replace(/^https:\/\//, '')}
                    </Text>
                    <Group gap="sm" grow>
                        <Button leftSection={<IconSend size={18} />} onClick={share} radius="md">
                            {t('refs.share')}
                        </Button>
                        <CopyButton timeout={2000} value={p.ref_link}>
                            {({ copied, copy }) => (
                                <Button
                                    color={copied ? 'teal' : undefined}
                                    leftSection={copied ? <IconCheck size={18} /> : <IconCopy size={18} />}
                                    onClick={() => {
                                        copy()
                                        haptic()
                                    }}
                                    radius="md"
                                    variant="light"
                                >
                                    {copied ? t('copied') : t('refs.copy')}
                                </Button>
                            )}
                        </CopyButton>
                    </Group>
                </Stack>
            </SectionCard.Root>

            {p.referrals.length === 0 ? (
                <Empty hint={t('empty.refs_hint')} icon={<IconUsers />} title={t('empty.refs')} />
            ) : (
                <Stack gap="xs">
                    <Text fw={600}>{t('refs.friends')}</Text>
                    <SectionCard.Root gap="sm">
                        {p.referrals.map((r, i) => (
                            <Group gap="sm" justify="space-between" key={i} wrap="nowrap">
                                <Text c={r.active ? undefined : 'dimmed'} fw={500} truncate="end">
                                    {r.name}
                                </Text>
                                <Text c={r.earned > 0 ? 'teal' : 'dimmed'} fw={600} style={{ whiteSpace: 'nowrap' }}>
                                    {r.earned > 0 ? `+${fmtMoney(r.earned, 2)}` : '—'}
                                </Text>
                            </Group>
                        ))}
                    </SectionCard.Root>
                </Stack>
            )}
        </Stack>
    )
}
