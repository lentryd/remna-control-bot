import { Alert, Stack } from '@mantine/core'
import { useTranslation } from 'react-i18next'

import { useAdminSettings } from '../../adminApi'
import { SettingsSkeleton } from './AdminMethods'
import { SaveBar, SectionGroups } from './SectionForm'
import { GENERAL } from './sections'
import { useSectionForms } from './use-section-forms'
import type { AdminSettings } from '../../types'

export function AdminGeneral() {
    const { t } = useTranslation('admin')
    const q = useAdminSettings()
    if (q.isPending) return <SettingsSkeleton />
    if (q.isError) return <Alert color="red" variant="soft">{t('settings.loadFailed')}</Alert>
    return <GeneralForm settings={q.data} />
}

function GeneralForm({ settings }: { settings: AdminSettings }) {
    const forms = useSectionForms([GENERAL], settings.sections)
    const ctl = forms[GENERAL.key]
    return (
        <Stack gap={0}>
            <SectionGroups ctl={ctl} />
            <SaveBar ctl={ctl.dirty || ctl.msg ? ctl : undefined} />
        </Stack>
    )
}
