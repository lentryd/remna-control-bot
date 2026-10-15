// Copied from remna-control/web/src/shared/ui/info-field/info-field.tsx
// Adapted from remnawave/frontend (AGPL-3.0)
import { Group, Text } from '@mantine/core'

export const InfoFieldShared = ({ label, value }: { label: string; value: React.ReactNode }) => (
    <Group align="center" justify="flex-start">
        <Text c="dimmed" size="sm">
            {label}
        </Text>
        <Text size="sm">{value || '—'}</Text>
    </Group>
)
