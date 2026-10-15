// Copied from remna-control/web/src/shared/ui/settings-row/index.tsx
import { Box, Card, Text } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './settings-row.module.css'

// SettingsGroup is a titled card of settings rows.
export function SettingsGroup({ title, description, actions, children }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode }) {
    return (
        <Card className={classes.group} padding={0}>
            <div className={classes.groupHeader}>
                <Box miw={0}>
                    <Text className={classes.groupTitle}>{title}</Text>
                    {description && (
                        <Text c="dimmed" mt={2} size="xs">
                            {description}
                        </Text>
                    )}
                </Box>
                {actions}
            </div>
            {children}
        </Card>
    )
}

// SettingsRow is one setting: what it is above, its control below at full
// width. An `inline` row (a switch) keeps the control on the right and the
// whole row tappable.
export function SettingsRow({ label, description, children, inline }: { label: ReactNode; description?: ReactNode; children: ReactNode; inline?: boolean }) {
    const text = (
        <div className={classes.text}>
            <Text fw={500} size="sm">
                {label}
            </Text>
            {description && (
                <Text c="dimmed" component="div" mt={2} size="xs">
                    {description}
                </Text>
            )}
        </div>
    )
    if (inline) {
        return (
            <label className={classes.row} data-inline>
                {text}
                <div className={classes.control}>{children}</div>
            </label>
        )
    }
    return (
        <div className={classes.row}>
            {text}
            <div className={classes.control}>{children}</div>
        </div>
    )
}
