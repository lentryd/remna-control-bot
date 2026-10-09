import { Box, SegmentedControl, Stack, Title } from '@mantine/core'
import { Route, Routes, useLocation, useNavigate } from 'react-router'

import { setFormat } from '@shared/format'
import { ApiError } from './api'
import { useAdminMe } from './adminApi'
import { AdminGeneral } from './pages/admin/AdminGeneral'
import { AdminMethods } from './pages/admin/AdminMethods'
import { AdminOrder } from './pages/admin/AdminOrder'
import { AdminOrders } from './pages/admin/AdminOrders'
import { A } from './pages/admin/text'
import { tg } from './telegram'
import { ErrorScreen } from './ui/ErrorScreen'
import { PageSkeleton } from './ui/PageSkeleton'

const TABS = [
    { value: '/admin', label: A.tabs.orders },
    { value: '/admin/methods', label: A.tabs.methods },
    { value: '/admin/general', label: A.tabs.general }
]

export function AdminApp() {
    const me = useAdminMe()
    const { pathname } = useLocation()
    const navigate = useNavigate()
    if (!tg) return <ErrorScreen code="no_tg" />
    if (me.isPending) return <PageSkeleton />
    if (me.isError) {
        const e = me.error
        const code = e instanceof ApiError && e.status === 403 ? 'no_access' : e instanceof ApiError && e.status === 401 ? 'no_tg' : 'unavailable'
        return <ErrorScreen code={code} onRetry={code === 'unavailable' ? () => me.refetch() : undefined} />
    }
    setFormat({ base: me.data.currency })
    const tab = TABS.find((t) => t.value !== '/admin' && pathname.startsWith(t.value))?.value ?? '/admin'
    return (
        <Box p="md">
            <Stack gap="md">
                <Title order={3}>{A.title}</Title>
                <SegmentedControl data={TABS} fullWidth onChange={(v) => navigate(v)} value={tab} />
                <Routes>
                    <Route element={<AdminOrders />} path="/admin" />
                    <Route element={<AdminOrder />} path="/admin/orders/:id" />
                    <Route element={<AdminMethods />} path="/admin/methods" />
                    <Route element={<AdminGeneral />} path="/admin/general" />
                    <Route element={<AdminOrders />} path="*" />
                </Routes>
            </Stack>
        </Box>
    )
}
