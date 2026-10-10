import type { TFunction } from 'i18next'
import { Accordion, Alert, Stack, Switch, Text } from '@mantine/core'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ShimmerSkeleton } from '@shared/ui/shimmer-skeleton'
import { StatusPill } from '@shared/ui/status'
import { useAdminSettings } from '../../adminApi'
import { SaveBar, SectionBlocks } from './SectionForm'
import { METHODS, sectionTitleKeys } from './sections'
import { isComplete, useSectionForms, type SectionCtl } from './use-section-forms'
import classes from './admin-methods.module.css'
import type { AdminSettings } from '../../types'

export function SettingsSkeleton() {
    return (
        <Stack gap="md">
            <ShimmerSkeleton height={180} radius="lg" width="100%" />
            <ShimmerSkeleton height={180} radius="lg" width="100%" />
        </Stack>
    )
}

export function AdminMethods() {
    const { t } = useTranslation('admin')
    const q = useAdminSettings()
    if (q.isPending) return <SettingsSkeleton />
    if (q.isError) return <Alert color="red" variant="soft">{t('settings.loadFailed')}</Alert>
    return <MethodsList settings={q.data} />
}

// A provider's state as the operator reads it: from the form, not the last save.
function providerState(t: TFunction<'admin'>, ctl: SectionCtl, lost: boolean): { color: string; label: string } {
    if (lost) return { color: 'red', label: t('settings.state.lost') }
    if (!ctl.v.enabled) return { color: 'gray', label: t('settings.state.off') }
    if (!isComplete(ctl.def, ctl.v)) return { color: 'yellow', label: t('settings.state.incomplete') }
    return { color: 'teal', label: t('settings.state.on') }
}

function MethodsList({ settings: s }: { settings: AdminSettings }) {
    const { t } = useTranslation('admin')
    const forms = useSectionForms(METHODS, s.sections)
    const lost = s.lost ?? []
    const [open, setOpen] = useState<string | null>(null)
    // One bar: the open section's if it has something to say, else the first that does.
    const has = (c: SectionCtl) => c.dirty || !!c.msg
    const target = open && has(forms[open]) ? forms[open] : METHODS.map((d) => forms[d.key]).find(has)
    return (
        <Stack gap="md">
            {lost.length > 0 && (
                <Alert color="yellow" variant="soft">
                    {t('settings.lost')} {sectionTitleKeys(lost).map((x) => (x.titleKey ? t(x.titleKey) : x.key)).join(', ')}
                </Alert>
            )}
            <Accordion
                chevronPosition="left"
                classNames={{ root: classes.list, item: classes.item, control: classes.control, label: classes.label, chevron: classes.chevron, content: classes.content }}
                onChange={setOpen}
                value={open}
                variant="separated"
            >
                {METHODS.map((def) => {
                    const ctl = forms[def.key]
                    const isLost = lost.includes(def.key)
                    const st = providerState(t, ctl, isLost)
                    return (
                        <Accordion.Item key={def.key} value={def.key}>
                            <div className={classes.head}>
                                <Accordion.Control>
                                    <Text className={classes.name}>
                                        {t(def.titleKey)}
                                        {ctl.dirty && <span aria-label={t('settings.unsaved')} className={classes.dirty} role="img" />}
                                    </Text>
                                    <StatusPill color={st.color} label={st.label} mt={4} size="sm" />
                                </Accordion.Control>
                                <Switch
                                    aria-label={`${t('settings.enabled')}: ${t(def.titleKey)}`}
                                    checked={!!ctl.v.enabled}
                                    className={classes.switch}
                                    onChange={(e) => ctl.set('enabled', e.currentTarget.checked)}
                                    size="md"
                                />
                            </div>
                            <Accordion.Panel>
                                <SectionBlocks ctl={ctl} intro={def.hintKey ? t(def.hintKey) : undefined} lost={isLost} webhookBase={s.webhook_base} />
                            </Accordion.Panel>
                        </Accordion.Item>
                    )
                })}
            </Accordion>
            <SaveBar ctl={target} />
        </Stack>
    )
}
