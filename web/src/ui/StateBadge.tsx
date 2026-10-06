// StateBadge.tsx — status pill with the same colors as the bot's icons.
import { Badge } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { daysLeft } from '@shared/format'

export function stateColor(expireAt: string | null, unlimited: boolean) {
    if (unlimited) return 'teal'
    const d = daysLeft(expireAt)
    if (d === null) return 'gray'
    return d < 0 ? 'red' : d < 3 ? 'yellow' : 'teal'
}

export function StateBadge({ status, expireAt, unlimited }: { status: string; expireAt: string | null; unlimited: boolean }) {
    const { t } = useTranslation()
    return (
        <Badge color={stateColor(expireAt, unlimited)} radius="sm" variant="light">
            {t(`sub.status.${status}`, { defaultValue: status })}
        </Badge>
    )
}
