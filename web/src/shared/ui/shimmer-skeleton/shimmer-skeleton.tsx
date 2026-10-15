// Copied from remna-control/web/src/shared/ui/shimmer-skeleton/shimmer-skeleton.tsx
import { Skeleton, type MantineSize, type SkeletonProps } from '@mantine/core'

type SizeValue = MantineSize | number | (string & {})

interface IProps extends Omit<SkeletonProps, 'h' | 'w' | 'height' | 'width'> {
    height: SizeValue
    width: SizeValue
}

export function ShimmerSkeleton({ height, width, ...rest }: IProps) {
    return <Skeleton h={height} w={width} {...rest} />
}
