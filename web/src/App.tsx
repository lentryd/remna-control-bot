import { Route, Routes } from 'react-router'

import { ApiError, useProfile } from './api'
import { AppShell } from './layout/AppShell'
import { Balance } from './pages/Balance'
import { Checkout } from './pages/Checkout'
import { Overview } from './pages/Overview'
import { Referrals } from './pages/Referrals'
import { SubscriptionDetails } from './pages/SubscriptionDetails'
import { Subscriptions } from './pages/Subscriptions'
import { Tariffs } from './pages/Tariffs'
import { tg } from './telegram'
import { ErrorScreen } from './ui/ErrorScreen'
import { PageSkeleton } from './ui/PageSkeleton'
import { setFormat } from '@shared/format'

export function App() {
    const profile = useProfile()
    if (!tg) return <ErrorScreen code="no_tg" />
    if (profile.isPending) return <PageSkeleton />
    if (profile.isError) {
        const e = profile.error
        const code = e instanceof ApiError && e.code === 'no_access' ? 'no_access' : e instanceof ApiError && e.status === 401 ? 'no_tg' : 'unavailable'
        return <ErrorScreen code={code} onRetry={code === 'unavailable' ? () => profile.refetch() : undefined} />
    }
    const p = profile.data
    // Synchronously, so the first paint is already in the base currency.
    setFormat({ base: p.currency })
    return (
        <AppShell>
            <Routes>
                <Route element={<Overview p={p} />} path="/" />
                <Route element={<Subscriptions p={p} />} path="/subscriptions" />
                <Route element={<SubscriptionDetails p={p} />} path="/subscriptions/:id" />
                <Route element={<Balance p={p} />} path="/balance" />
                <Route element={<Referrals p={p} />} path="/referrals" />
                <Route element={<Tariffs />} path="/tariffs" />
                <Route element={<Checkout p={p} />} path="/checkout" />
                <Route element={<Overview p={p} />} path="*" />
            </Routes>
        </AppShell>
    )
}
