// Copied from remna-control/web/src/shared/constants/theme/variant-resolver.ts
import { defaultVariantColorsResolver, parseThemeColor, type VariantColorsResolver } from '@mantine/core'

// "soft" is the app's tinted variant: Mantine's per-scheme light colours
// (so it works in both themes) plus a hairline border of the same hue.
export const variantColorResolver: VariantColorsResolver = (input) => {
    if (input.variant !== 'soft') return defaultVariantColorsResolver(input)

    const { color } = parseThemeColor({ color: input.color || input.theme.primaryColor, theme: input.theme })
    const neutral = color === 'gray' || color === 'dark' || color === 'shaded-gray'
    if (neutral) {
        return {
            background: 'var(--app-fill)',
            hover: 'var(--app-fill-hover)',
            color: 'var(--mantine-color-text)',
            border: '1px solid var(--app-border)'
        }
    }
    return {
        background: `var(--mantine-color-${color}-light)`,
        hover: `var(--mantine-color-${color}-light-hover)`,
        color: `var(--mantine-color-${color}-light-color)`,
        border: `1px solid color-mix(in srgb, var(--mantine-color-${color}-light-color) 16%, transparent)`
    }
}
