// Copied from remna-control/web/src/shared/ui/stat-strip/index.tsx
import { Card, Group, Text, UnstyledButton } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './stat-strip.module.css'

export interface StatStripItem {
    key?: string
    label: ReactNode
    value: ReactNode
    hint?: ReactNode
    // a status dot / tone for the value
    color?: string
    icon?: React.ComponentType<{ size?: number; stroke?: number }>
    onClick?: () => void
    active?: boolean
}

// StatStrip is a row of figures in one card, split by hairlines; cells
// with onClick act as filters.
export function StatStrip({ items, mb }: { items: StatStripItem[]; mb?: string | number }) {
    return (
        <Card className={classes.strip} mb={mb} padding={0} style={{ '--cells': items.length } as React.CSSProperties}>
            {items.map((item, i) => {
                const body = (
                    <>
                        <Group gap={6} wrap="nowrap">
                            {item.icon && <item.icon size={15} stroke={1.75} />}
                            <Text className={classes.label}>{item.label}</Text>
                        </Group>
                        <Text c={item.color} className={classes.value}>
                            {item.value}
                        </Text>
                        {item.hint && <Text className={classes.hint}>{item.hint}</Text>}
                    </>
                )
                return item.onClick ? (
                    <UnstyledButton className={classes.cell} data-active={item.active || undefined} data-clickable key={item.key ?? i} onClick={item.onClick}>
                        {body}
                    </UnstyledButton>
                ) : (
                    <div className={classes.cell} key={item.key ?? i}>
                        {body}
                    </div>
                )
            })}
        </Card>
    )
}
