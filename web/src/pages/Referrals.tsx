// Referrals.tsx — one card with earnings, the link and share/copy, then
// every invitee with what they brought.
import { ActionIcon, Button, Card, CopyButton, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconCheck, IconCopy, IconGift, IconSend, IconUsers } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
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

            <Card className={classes.hero} padding="lg" radius="lg">
                <Stack gap="md">
                    <Group gap="sm" wrap="nowrap">
                        <ThemeIcon radius="xl" size={44} variant="white">
                            <IconGift size={24} />
                        </ThemeIcon>
                        <Stack gap={0}>
                            <Text className={classes.muted} size="sm">{t('refs.earned')}</Text>
                            <Text fw={700} lh={1.1} size="28px">{fmtMoney(p.referrals_earned, 2)}</Text>
                        </Stack>
                    </Group>
                    <Text className={classes.muted} size="sm">
                        {p.referrals.length > 0 ? t('refs.invited_count', { count: p.referrals.length }) : t('refs.pitch')}
                    </Text>
                    <CopyButton timeout={2000} value={p.ref_link}>
                        {({ copied, copy }) => (
                            <Group
                                className={classes.link}
                                gap="xs"
                                onClick={() => {
                                    copy()
                                    haptic()
                                }}
                                wrap="nowrap"
                            >
                                <Text ff="monospace" size="sm" truncate="end">{p.ref_link.replace(/^https:\/\//, '')}</Text>
                                <ActionIcon aria-label={t('copied')} color="white" variant="transparent">
                                    {copied ? <IconCheck size={18} /> : <IconCopy size={18} />}
                                </ActionIcon>
                            </Group>
                        )}
                    </CopyButton>
                    <Button fullWidth leftSection={<IconSend size={18} />} onClick={share} radius="md" size="md" variant="white">
                        {t('refs.share')}
                    </Button>
                </Stack>
            </Card>

            {p.referrals.length === 0 ? (
                <Empty hint={t('empty.refs_hint')} icon={<IconUsers />} title={t('empty.refs')} />
            ) : (
                <Stack gap="xs">
                    <Text fw={600}>{t('refs.friends')}</Text>
                    <SectionCard.Root gap="sm">
                        {p.referrals.map((r, i) => (
                            <Group justify="space-between" key={i} wrap="nowrap">
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
