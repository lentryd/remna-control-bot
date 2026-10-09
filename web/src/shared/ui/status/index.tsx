// Dot and StatusPill copied from vpn-control/web/src/components/badges.tsx;
// BackLink from vpn-control/web/src/components/ui.tsx. Adapted: StatusPill
// takes a colour and a label (the bot's statuses are orders, not panel users).
import { Anchor, Badge, type BadgeProps } from '@mantine/core'
import { IconChevronLeft } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import classes from './status.module.css'

// cssColor turns a theme colour ("teal", "yellow.4") into a CSS value.
export const cssColor = (c: string) =>
    c.startsWith('var(') || c.startsWith('#') ? c : c.includes('.') ? `var(--mantine-color-${c.replace('.', '-')})` : `var(--mantine-color-${c}-5)`

// Dot is the small status light used in pills and rows.
export function Dot({ color, pulse, size = 7 }: { color: string; pulse?: boolean; size?: number }) {
    return <span className={classes.dot} data-pulse={pulse || undefined} style={{ '--dot-color': cssColor(color), '--dot-size': `${size}px` } as React.CSSProperties} />
}

// StatusPill is a status as a tinted pill with a leading dot.
export function StatusPill({ color, label, pulse, ...props }: { color: string; label: ReactNode; pulse?: boolean } & Omit<BadgeProps, 'children' | 'color'>) {
    return (
        <Badge color={color} leftSection={<Dot color={color} pulse={pulse} size={6} />} radius="xl" size="md" variant="soft" {...props}>
            {label}
        </Badge>
    )
}

// BackLink is a page header's way back to the list it came from.
export function BackLink({ to, children }: { to: string; children: ReactNode }) {
    return (
        <Anchor c="dimmed" component={Link} size="sm" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }} to={to} underline="never">
            <IconChevronLeft size={14} stroke={2} />
            {children}
        </Anchor>
    )
}
