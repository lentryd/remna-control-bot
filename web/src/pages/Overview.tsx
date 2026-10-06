// Overview.tsx — greeting, balance card, then either the start card (no
// subscriptions yet) or the most urgent subscriptions.
import { Button, Card, Group, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconChevronRight, IconRocket, IconTag } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import type { Profile } from '../types'
import { BalanceCard } from '../ui/BalanceCard'
import { CtaButton } from '../ui/CtaButton'
import { SubscriptionRow } from '../ui/SubscriptionRow'

function StartCard() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    return (
        <Card padding="lg" radius="lg" withBorder>
            <Stack align="center" gap="sm" ta="center">
                <ThemeIcon radius="xl" size={56} variant="light">
                    <IconRocket size={30} />
                </ThemeIcon>
                <Text fw={700} size="lg">{t('overview.start_title')}</Text>
                <Text c="dimmed" maw={320} size="sm">{t('overview.start_text')}</Text>
                <CtaButton fullWidth mt="xs" onClick={() => navigate('/tariffs')} size="md">
                    {t('overview.start_button')}
                </CtaButton>
            </Stack>
        </Card>
    )
}

export function Overview({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const empty = p.subscriptions.length === 0
    return (
        <Stack gap="lg">
            <Title order={3}>{t('overview.hello', { name: p.name })}</Title>
            <BalanceCard p={p} />
            {empty ? (
                <StartCard />
            ) : (
                <>
                    <Group justify="space-between">
                        <Text fw={600}>{t('overview.subs')}</Text>
                        <Button onClick={() => navigate('/subscriptions')} rightSection={<IconChevronRight size={16} />} size="compact-sm" variant="subtle">
                            {t('overview.all')}
                        </Button>
                    </Group>
                    {p.subscriptions.slice(0, 3).map((s) => (
                        <SubscriptionRow key={s.id} s={s} />
                    ))}
                    <Button leftSection={<IconTag size={18} />} onClick={() => navigate('/tariffs')} radius="md" size="md" variant="light">
                        {t('overview.tariffs')}
                    </Button>
                </>
            )}
        </Stack>
    )
}
