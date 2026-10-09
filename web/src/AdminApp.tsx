import { Box } from '@mantine/core'
import { IconCreditCard, IconReceipt, IconSettings } from '@tabler/icons-react'
import { Route, Routes, useLocation, useNavigate } from 'react-router'

import { setFormat } from '@shared/format'
import { PageHeaderShared } from '@shared/ui/page-header'
import { SectionNav } from '@shared/ui/section-nav'
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
    { value: '/admin', label: A.tabs.orders, icon: IconReceipt },
    { value: '/admin/methods', label: A.tabs.methods, icon: IconCreditCard },
    { value: '/admin/general', label: A.tabs.general, icon: IconSettings }
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
    // An order has its own header with a way back; the tabs belong to the lists.
    const detail = pathname.startsWith('/admin/orders/')
    const routes = (
        <Routes>
            <Route element={<AdminOrders />} path="/admin" />
            <Route element={<AdminOrder />} path="/admin/orders/:id" />
            <Route element={<AdminMethods />} path="/admin/methods" />
            <Route element={<AdminGeneral />} path="/admin/general" />
            <Route element={<AdminOrders />} path="*" />
        </Routes>
    )
    return (
        <Box maw={560} mx="auto" pb="calc(24px + env(safe-area-inset-bottom))" pt="md" px="md">
            {detail ? (
                <div className="vpnc-enter">{routes}</div>
            ) : (
                <>
                    <PageHeaderShared description={me.data.name} title={A.title} />
                    <SectionNav items={TABS} onChange={(v) => navigate(v)} value={tab}>
                        <div className="vpnc-enter" key={tab}>
                            {routes}
                        </div>
                    </SectionNav>
                </>
            )}
        </Box>
    )
}
