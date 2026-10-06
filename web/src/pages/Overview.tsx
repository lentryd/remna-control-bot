// Overview.tsx — greeting, figures, nearest subscriptions, shortcuts.
import { Button, Group, Stack, Text, Title } from '@mantine/core'
import { IconChevronRight, IconPackage, IconTag } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import type { Profile } from '../types'
import { BalanceCard } from '../ui/BalanceCard'
import { Empty } from '../ui/Empty'
import { SubscriptionRow } from '../ui/SubscriptionRow'

export function Overview({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    return (
        <Stack gap="lg">
            <Title order={3}>{t('overview.hello', { name: p.name })}</Title>
            <BalanceCard p={p} />
            <Group justify="space-between">
                <Text fw={600}>{t('overview.subs')}</Text>
                {p.subscriptions.length > 0 && (
                    <Button onClick={() => navigate('/subscriptions')} rightSection={<IconChevronRight size={16} />} size="compact-sm" variant="subtle">
                        {t('overview.all')}
                    </Button>
                )}
            </Group>
            {p.subscriptions.length === 0 ? (
                <Empty
                    action={{ label: t('overview.tariffs'), onClick: () => navigate('/tariffs') }}
                    hint={t('empty.subs_hint')}
                    icon={<IconPackage />}
                    title={t('empty.subs')}
                />
            ) : (
                p.subscriptions.slice(0, 3).map((s) => <SubscriptionRow key={s.id} s={s} />)
            )}
            <Button leftSection={<IconTag size={18} />} onClick={() => navigate('/tariffs')} radius="md" size="md" variant="light">
                {t('overview.tariffs')}
            </Button>
        </Stack>
    )
}
