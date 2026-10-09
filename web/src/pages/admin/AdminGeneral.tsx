import { Alert } from '@mantine/core'

import { useAdminSettings } from '../../adminApi'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { SectionForm } from './SectionForm'
import { GENERAL } from './sections'
import { A } from './text'

export function AdminGeneral() {
    const q = useAdminSettings()
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">{A.settings.loadFailed}</Alert>
    return <SectionForm def={GENERAL} initial={q.data.sections[GENERAL.key] ?? {}} webhookBase={q.data.webhook_base} />
}
