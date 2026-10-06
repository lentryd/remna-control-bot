import '@fontsource-variable/geist'
import '@fontsource-variable/geist-mono'
import './app/vendor.css'
import './app/global.css'
import './i18n'

import { MantineProvider } from '@mantine/core'
import dayjs from 'dayjs'
import 'dayjs/locale/ru'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode, Suspense, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter } from 'react-router'

import { App } from './App'
import { initTelegram, tg } from './telegram'
import { PageSkeleton } from './ui/PageSkeleton'
import { cssVariablesResolver, theme } from '@shared/constants/theme'
import { setFormat } from '@shared/format'

initTelegram()
dayjs.locale('ru')
setFormat({ base: 'RUB', locale: 'ru-RU' })

const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } })

// Follows the Telegram client's light/dark switch.
function Root() {
    const [scheme, setScheme] = useState<'light' | 'dark'>(tg?.colorScheme ?? 'dark')
    useEffect(() => {
        if (!tg) return
        const sync = () => setScheme(tg!.colorScheme)
        tg.onEvent('themeChanged', sync)
        return () => {
            tg?.offEvent('themeChanged', sync)
        }
    }, [])
    return (
        <MantineProvider cssVariablesResolver={cssVariablesResolver} forceColorScheme={scheme} theme={theme}>
            <Suspense fallback={<PageSkeleton />}>
                <QueryClientProvider client={queryClient}>
                    <HashRouter>
                        <App />
                    </HashRouter>
                </QueryClientProvider>
            </Suspense>
        </MantineProvider>
    )
}

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <Root />
    </StrictMode>
)
