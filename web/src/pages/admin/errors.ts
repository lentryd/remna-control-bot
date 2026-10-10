import type { TFunction } from 'i18next'

// Readable text for an API error code; falls back to the server message or the code itself.
export const errMsg = (t: TFunction<'admin'>, code: string, detail?: string) => t(`order.errors.${code}`, { defaultValue: detail ?? code })
