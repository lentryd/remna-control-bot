// PageSkeleton.tsx
import { Stack } from '@mantine/core'

import { ShimmerSkeleton } from '@shared/ui/shimmer-skeleton'

export function PageSkeleton() {
    return (
        <Stack gap="md" p="md" maw={560} mx="auto">
            <ShimmerSkeleton height={28} radius="md" width="60%" />
            <ShimmerSkeleton height={96} radius="lg" width="100%" />
            <ShimmerSkeleton height={140} radius="lg" width="100%" />
            <ShimmerSkeleton height={140} radius="lg" width="100%" />
        </Stack>
    )
}
