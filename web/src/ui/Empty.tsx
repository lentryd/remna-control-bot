// Empty.tsx — empty state with a hint and an optional action.
import { Button, Stack, Text, ThemeIcon } from '@mantine/core'
import type { ReactNode } from 'react'

export function Empty({ icon, title, hint, action }: { icon: ReactNode; title: string; hint?: string; action?: { label: string; onClick: () => void } }) {
    return (
        <Stack align="center" gap="xs" py="xl" ta="center">
            <ThemeIcon color="gray" radius="xl" size={56} variant="light">
                {icon}
            </ThemeIcon>
            <Text fw={600}>{title}</Text>
            {hint && <Text c="dimmed" size="sm" maw={320}>{hint}</Text>}
            {action && <Button mt="sm" onClick={action.onClick} radius="md">{action.label}</Button>}
        </Stack>
    )
}
