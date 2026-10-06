import { Box, Group, Text, UnstyledButton } from '@mantine/core'
import { IconHome, IconPackage, IconUsers, IconWallet } from '@tabler/icons-react'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'

import { tg } from '../telegram'
import classes from './app-shell.module.css'

const tabs = [
    { to: '/', icon: IconHome, key: 'overview' },
    { to: '/subscriptions', icon: IconPackage, key: 'subscriptions' },
    { to: '/balance', icon: IconWallet, key: 'balance' },
    { to: '/referrals', icon: IconUsers, key: 'referrals' }
] as const

export function AppShell({ children }: { children: ReactNode }) {
    const { t } = useTranslation()
    const { pathname } = useLocation()
    const navigate = useNavigate()
    const nested = pathname.split('/').filter(Boolean).length > 1 || pathname === '/tariffs'

    useEffect(() => {
        if (!tg) return
        if (!nested) {
            tg.BackButton.hide()
            return
        }
        const back = () => navigate(-1)
        tg.BackButton.show()
        tg.BackButton.onClick(back)
        return () => {
            tg?.BackButton.offClick(back)
        }
    }, [nested, navigate])

    const active = (to: string) => (to === '/' ? pathname === '/' : pathname.startsWith(to))
    return (
        <>
            <Box className={classes.content}>{children}</Box>
            <Group className={classes.nav} gap={0} grow wrap="nowrap">
                {tabs.map(({ to, icon: Icon, key }) => (
                    <UnstyledButton className={classes.tab} data-active={active(to) || undefined} key={to} onClick={() => navigate(to)}>
                        <Icon size={22} stroke={1.75} />
                        <Text size="xs">{t(`nav.${key}`)}</Text>
                    </UnstyledButton>
                ))}
            </Group>
        </>
    )
}
