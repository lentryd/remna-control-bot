// Texts are loaded from ./locales/<lang>/<ns>.json (relative: works under any WEBAPP_PATH).
// English is the base; the language follows Telegram's language_code.
import i18n from 'i18next'
import HttpBackend from 'i18next-http-backend'
import { initReactI18next } from 'react-i18next'

import { tg } from './telegram'

export const supported = ['en', 'ru'] as const
export type Lang = (typeof supported)[number]

export function langFromTelegram(code?: string): Lang {
    const primary = (code ?? '').toLowerCase().split(/[-_]/)[0]
    return (supported as readonly string[]).includes(primary) ? (primary as Lang) : 'en'
}

// Known synchronously; i18n.language is only set once the async init has run.
export const lng = langFromTelegram(tg?.initDataUnsafe?.user?.language_code)

i18n.use(HttpBackend)
    .use(initReactI18next)
    .init({
        lng,
        fallbackLng: 'en',
        supportedLngs: [...supported],
        ns: ['bot'],
        defaultNS: 'bot',
        backend: { loadPath: './locales/{{lng}}/{{ns}}.json' },
        interpolation: { escapeValue: false },
        react: { useSuspense: true }
    })

export default i18n
