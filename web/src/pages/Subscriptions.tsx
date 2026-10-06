// Subscriptions.tsx — every subscription as a card.
import { Stack, Title } from '@mantine/core'
import { IconPackage } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import type { Profile } from '../types'
import { Empty } from '../ui/Empty'
import { SubscriptionRow } from '../ui/SubscriptionRow'

export function Subscriptions({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    return (
        <Stack gap="md">
            <Title order={3}>{t('nav.subscriptions')}</Title>
            {p.subscriptions.length === 0 ? (
                <Empty
                    action={{ label: t('overview.tariffs'), onClick: () => navigate('/tariffs') }}
                    hint={t('empty.subs_hint')}
                    icon={<IconPackage />}
                    title={t('empty.subs')}
                />
            ) : (
                p.subscriptions.map((s) => <SubscriptionRow key={s.id} s={s} />)
            )}
        </Stack>
    )
}
