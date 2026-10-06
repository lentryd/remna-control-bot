// Copied from vpn-control/web/src/shared/ui/section-card/section-card.section.tsx
import { Box, BoxProps } from '@mantine/core'
import { ReactNode, Ref } from 'react'

interface ISectionCardSectionProps extends BoxProps {
    children: ReactNode
    ref?: Ref<HTMLDivElement>
}

export function SectionCardSection({ children, ...props }: ISectionCardSectionProps) {
    return <Box {...props}>{children}</Box>
}
