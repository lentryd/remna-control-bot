// ErrorScreen.tsx
import { Button, Center, Stack, Text, ThemeIcon } from '@mantine/core'
import { IconLock, IconPlugConnectedX, IconBrandTelegram } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

const icons = { no_tg: IconBrandTelegram, no_access: IconLock, unavailable: IconPlugConnectedX }

export function ErrorScreen({ code, onRetry }: { code: keyof typeof icons; onRetry?: () => void }) {
    const { t } = useTranslation()
    const Icon = icons[code]
    return (
        <Center h="100dvh" p="lg">
            <Stack align="center" gap="sm" ta="center" maw={340}>
                <ThemeIcon radius="xl" size={64} variant="light">
                    <Icon size={32} />
                </ThemeIcon>
                <Text fw={500}>{t(`errors.${code}`)}</Text>
                {onRetry && <Button onClick={onRetry} variant="light">{t('errors.retry')}</Button>}
            </Stack>
        </Center>
    )
}
