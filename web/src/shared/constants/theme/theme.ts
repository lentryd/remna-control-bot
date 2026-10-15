// Copied from remna-control/web/src/shared/constants/theme/theme.ts
import { createTheme, type MantineColorsTuple } from '@mantine/core'

import { components } from './components'
import { variantColorResolver } from './variant-resolver'

// brand is the accent: a calm violet that reads on both near-black and white.
const brand: MantineColorsTuple = [
    '#f4f1ff',
    '#e6e0ff',
    '#cbbdfe',
    '#ad97fc',
    '#9477fa',
    '#8363f9',
    '#7958f8',
    '#6748dd',
    '#5b3fc6',
    '#4e34ae'
]

// Neutrals are zinc-like in both schemes: gray drives light surfaces and
// text, dark the dark ones (dark[0] is text, dark[7] the body).
const gray: MantineColorsTuple = [
    '#fafafa',
    '#f4f4f5',
    '#e4e4e7',
    '#d4d4d8',
    '#a1a1aa',
    '#71717a',
    '#52525b',
    '#3f3f46',
    '#27272a',
    '#18181b'
]

const dark: MantineColorsTuple = [
    '#ededef',
    '#d4d4d8',
    '#a1a1aa',
    '#71717a',
    '#3f3f46',
    '#2c2c31',
    '#1f1f23',
    '#151517',
    '#0f0f11',
    '#09090b'
]

export const theme = createTheme({
    primaryColor: 'brand',
    primaryShade: { light: 6, dark: 5 },
    colors: {
        brand,
        gray,
        dark,
        // the panel's "disabled" grey, kept for status colours
        'shaded-gray': gray
    },
    variantColorResolver,
    components,
    autoContrast: true,
    luminanceThreshold: 0.35,
    cursorType: 'pointer',
    focusRing: 'auto',
    fontSmoothing: true,
    white: '#ffffff',
    black: '#09090b',
    fontFamily: "'Geist Variable', 'Geist', ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif",
    fontFamilyMonospace: "'Geist Mono Variable', 'Geist Mono', ui-monospace, 'SF Mono', Menlo, monospace",
    headings: {
        fontFamily: "'Geist Variable', 'Geist', ui-sans-serif, system-ui, sans-serif",
        fontWeight: '600',
        sizes: {
            h1: { fontSize: '1.75rem', lineHeight: '1.2' },
            h2: { fontSize: '1.375rem', lineHeight: '1.25' },
            h3: { fontSize: '1.125rem', lineHeight: '1.3' },
            h4: { fontSize: '1rem', lineHeight: '1.35' },
            h5: { fontSize: '0.9375rem', lineHeight: '1.4' },
            h6: { fontSize: '0.875rem', lineHeight: '1.4' }
        }
    },
    fontSizes: {
        xs: '0.75rem',
        sm: '0.8125rem',
        md: '0.875rem',
        lg: '1rem',
        xl: '1.125rem'
    },
    radius: {
        xs: '4px',
        sm: '6px',
        md: '8px',
        lg: '12px',
        xl: '16px'
    },
    defaultRadius: 'md',
    shadows: {
        xs: '0 1px 2px rgb(9 9 11 / 0.05)',
        sm: '0 1px 2px rgb(9 9 11 / 0.06), 0 1px 3px rgb(9 9 11 / 0.06)',
        md: '0 4px 12px -2px rgb(9 9 11 / 0.10), 0 2px 4px -2px rgb(9 9 11 / 0.06)',
        lg: '0 12px 32px -8px rgb(9 9 11 / 0.22), 0 4px 8px -4px rgb(9 9 11 / 0.08)',
        xl: '0 24px 64px -12px rgb(9 9 11 / 0.35)'
    },
    breakpoints: {
        xs: '30em',
        sm: '40em',
        md: '48em',
        lg: '64em',
        xl: '80em',
        '2xl': '96em',
        '3xl': '120em',
        '4xl': '160em'
    },
    other: {
        sidebarWidth: 248,
        sidebarCollapsedWidth: 68
    }
})
