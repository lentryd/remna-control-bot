import { Alert, Stack } from '@mantine/core'

import { useAdminSettings } from '../../adminApi'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { SectionForm } from './SectionForm'
import { METHODS } from './sections'
import { A } from './text'

export function AdminMethods() {
    const q = useAdminSettings()
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">{A.settings.loadFailed}</Alert>
    const s = q.data
    return (
        <Stack gap="md">
            {s.lost.length > 0 && <Alert color="yellow">{A.settings.lost}</Alert>}
            {METHODS.map((def) => (
                <SectionForm def={def} initial={s.sections[def.key] ?? {}} key={def.key} webhookBase={s.webhook_base} />
            ))}
        </Stack>
    )
}
