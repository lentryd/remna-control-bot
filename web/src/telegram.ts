// Thin wrapper over window.Telegram.WebApp; everything is optional so the
// page also renders in a normal browser (it then shows "open from the bot").
type ThemeParams = Partial<Record<'bg_color' | 'text_color' | 'button_color' | 'secondary_bg_color', string>>
interface WebApp {
    initData: string
    colorScheme: 'light' | 'dark'
    themeParams: ThemeParams
    ready(): void
    expand(): void
    onEvent(e: 'themeChanged', cb: () => void): void
    BackButton: { show(): void; hide(): void; onClick(cb: () => void): void; offClick(cb: () => void): void }
    HapticFeedback?: { notificationOccurred(t: 'success' | 'error'): void }
    openTelegramLink(url: string): void
}
declare global {
    interface Window {
        Telegram?: { WebApp: WebApp }
    }
}

export const tg: WebApp | undefined = window.Telegram?.WebApp?.initData ? window.Telegram.WebApp : undefined

export function initTelegram() {
    tg?.ready()
    tg?.expand()
}

export const haptic = () => tg?.HapticFeedback?.notificationOccurred('success')
