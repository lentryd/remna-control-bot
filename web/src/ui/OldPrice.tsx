// OldPrice — the regular price next to a discounted one, struck through.
import { Text, type TextProps } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './OldPrice.module.css'

export function OldPrice({ children, ...props }: TextProps & { children: ReactNode }) {
    return (
        <Text className={classes.old} component="span" {...props}>
            {children}
        </Text>
    )
}
