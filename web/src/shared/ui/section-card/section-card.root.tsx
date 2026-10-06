// Copied from vpn-control/web/src/shared/ui/section-card/section-card.root.tsx
import { Card, Divider, Stack, type CardProps, type MantineSpacing } from '@mantine/core'
import { Children, type ReactNode, type RefObject } from 'react'

interface ISectionCardRootProps extends Omit<CardProps, 'children'> {
    children: ReactNode
    dividerOpacity?: number
    onlyFirstDivider?: boolean
    allDividers?: boolean
    gap?: MantineSpacing
    ref?: RefObject<HTMLDivElement | null>
}

// SectionCardRoot stacks sections in a bordered card with hairlines between.
export function SectionCardRoot({ children, onlyFirstDivider = false, allDividers = true, gap = 'md', p = 'md', ref, dividerOpacity: _o, ...props }: ISectionCardRootProps) {
    const childArray = Children.toArray(children).filter(Boolean)

    const withDividers = childArray.flatMap((child, index) => {
        const divide = (onlyFirstDivider && index === 0 && childArray.length > 1) || (!onlyFirstDivider && allDividers && index < childArray.length - 1)
        return divide ? [child, <Divider key={`divider-${index}`} />] : [child]
    })

    return (
        <Card p={p} radius="lg" ref={ref} style={{ boxShadow: 'none' }} {...props}>
            <Stack gap={gap}>{withDividers}</Stack>
        </Card>
    )
}
