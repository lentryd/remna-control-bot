// Copied from vpn-control/web/src/shared/ui/page-header/page-header.shared.tsx
import { Box, Group, Text, Title, type BoxProps } from '@mantine/core'
import { forwardRef, type ReactNode } from 'react'

import classes from './page-header.module.css'

export interface PageHeaderSharedProps extends BoxProps {
    actions?: ReactNode
    // small line above the title: breadcrumbs, a back link, a kind
    eyebrow?: ReactNode
    description?: ReactNode
    // kept for callers; the header itself is text-only
    icon?: ReactNode
    // shown right of the title (status badges and the like)
    badges?: ReactNode
    title: ReactNode
    wrapActions?: boolean
}

// PageHeaderShared is the top of every page: title, a muted line under it
// and the page's main actions on the right.
export const PageHeaderShared = forwardRef<HTMLDivElement, PageHeaderSharedProps>(
    ({ title, description, actions, eyebrow, badges, wrapActions = true, icon: _icon, ...props }, ref) => (
        <Box className={classes.root} ref={ref} {...props}>
            <Box className={classes.text}>
                {eyebrow && <Box className={classes.eyebrow}>{eyebrow}</Box>}
                <Group gap="sm" wrap="wrap">
                    <Title className={classes.title} order={1}>
                        {title}
                    </Title>
                    {badges}
                </Group>
                {description && (
                    <Text c="dimmed" className={classes.description} component="div" size="sm">
                        {description}
                    </Text>
                )}
            </Box>
            {actions && (
                <Group className={classes.actions} gap="xs" wrap={wrapActions ? 'wrap' : 'nowrap'}>
                    {actions}
                </Group>
            )}
        </Box>
    )
)
