// Russian texts loaded from ./locales/ru/bot.json (relative: works under any WEBAPP_PATH).
import i18n from 'i18next'
import HttpBackend from 'i18next-http-backend'
import { initReactI18next } from 'react-i18next'

i18n.use(HttpBackend)
    .use(initReactI18next)
    .init({
        lng: 'ru',
        fallbackLng: 'ru',
        ns: ['bot'],
        defaultNS: 'bot',
        backend: { loadPath: './locales/{{lng}}/{{ns}}.json' },
        interpolation: { escapeValue: false },
        react: { useSuspense: true }
    })

export default i18n
