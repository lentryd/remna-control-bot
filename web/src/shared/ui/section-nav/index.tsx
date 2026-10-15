// Copied from remna-control/web/src/shared/ui/section-nav/index.tsx
import { Box, ScrollArea, Text, UnstyledButton } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './section-nav.module.css'

export interface SectionNavItem {
    value: string
    label: string
    hint?: string
    icon: React.ComponentType<{ size?: number; stroke?: number }>
}

// SectionNav is a page's own sub-navigation: a list on the left on wide
// screens, a scrollable strip above the content on narrow ones.
export function SectionNav({ items, value, onChange, children }: { items: SectionNavItem[]; value: string; onChange: (v: string) => void; children: ReactNode }) {
    return (
        <div className={classes.layout}>
            <ScrollArea className={classes.navScroll} scrollbarSize={0} type="never">
                <nav className={classes.nav}>
                    {items.map((item) => (
                        <UnstyledButton className={classes.item} data-active={item.value === value || undefined} key={item.value} onClick={() => onChange(item.value)}>
                            <item.icon size={18} stroke={1.75} />
                            <Box miw={0}>
                                <Text className={classes.label}>{item.label}</Text>
                                {item.hint && <Text className={classes.hint}>{item.hint}</Text>}
                            </Box>
                        </UnstyledButton>
                    ))}
                </nav>
            </ScrollArea>
            <div className={classes.content}>{children}</div>
        </div>
    )
}
