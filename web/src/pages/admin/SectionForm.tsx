import { ActionIcon, Button, NumberInput, PasswordInput, SegmentedControl, Select, Stack, Switch, TagsInput, Text, Textarea, TextInput } from '@mantine/core'
import { IconX } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ActionBar } from '@shared/ui/action-bar'
import { CopyableFieldShared } from '@shared/ui/copyable-field/copyable-field'
import { SettingsGroup, SettingsRow } from '@shared/ui/settings-row'
import { Dot } from '@shared/ui/status'
import classes from './section-form.module.css'
import { fieldGroups } from './sections'
import { isDay, type SectionCtl } from './use-section-forms'
import type { Field } from './sections'

const MASK = '••••'

const unitSection = (unit?: string) =>
    unit ? (
        <Text c="dimmed" className={classes.unit} size="sm">
            {unit}
        </Text>
    ) : undefined

function SecretState({ v, base }: { v: unknown; base: unknown }) {
    const { t } = useTranslation('admin')
    const s = String(v ?? '')
    if (s === '') return <SecretLine color="gray" text={t('settings.secretNone')} />
    if (s.startsWith(MASK) && s === String(base ?? '')) return <SecretLine color="teal" text={t('settings.secretSaved', { mask: s })} />
    return <SecretLine color="blue" text={t('settings.secretNew')} />
}

function SecretLine({ color, text }: { color: string; text: string }) {
    return (
        <span className={classes.secretState}>
            <Dot color={color} size={6} />
            {text}
        </span>
    )
}

function FieldInput({ f, v, base, set }: { f: Field; v: unknown; base: unknown; set: (x: unknown) => void }) {
    const { t } = useTranslation('admin')
    const common = { 'aria-label': t(f.labelKey), size: 'md' as const }
    switch (f.kind) {
        case 'bool':
            return <Switch {...common} checked={!!v} onChange={(e) => set(e.currentTarget.checked)} />
        case 'secret':
            // The masked value is sent back unchanged; the server keeps the stored secret.
            return (
                <PasswordInput
                    {...common}
                    autoCapitalize="off"
                    autoComplete="off"
                    description={<SecretState base={base} v={v} />}
                    onChange={(e) => set(e.currentTarget.value)}
                    spellCheck={false}
                    value={String(v ?? '')}
                />
            )
        case 'number':
            return (
                <NumberInput
                    {...common}
                    allowDecimal={!!f.decimal}
                    allowNegative={false}
                    hideControls
                    inputMode={f.decimal ? 'decimal' : 'numeric'}
                    onChange={set}
                    rightSection={unitSection(f.unitKey ? t(f.unitKey) : undefined)}
                    rightSectionPointerEvents="none"
                    rightSectionWidth={f.unitKey ? 52 : undefined}
                    value={v as number | string}
                />
            )
        case 'tags': {
            const tags = (v as string[] | undefined) ?? []
            return (
                <TagsInput
                    {...common}
                    error={tags.some((d) => !isDay(d)) ? t('settings.daysInvalid') : undefined}
                    inputMode="numeric"
                    onChange={set}
                    placeholder={tags.length === 0 ? t('settings.daysPlaceholder') : undefined}
                    rightSection={unitSection(f.unitKey ? t(f.unitKey) : undefined)}
                    rightSectionPointerEvents="none"
                    rightSectionWidth={f.unitKey ? 52 : undefined}
                    splitChars={[',', ' ', ';']}
                    value={tags}
                />
            )
        }
        case 'segmented': {
            const hintKey = f.options?.find((o) => o.value === String(v))?.hintKey
            return (
                <Stack gap={8}>
                    <SegmentedControl
                        aria-label={t(f.labelKey)}
                        classNames={{ label: classes.segLabel }}
                        data={(f.options ?? []).map(({ value, labelKey }) => ({ value, label: t(labelKey) }))}
                        fullWidth
                        onChange={set}
                        value={String(v ?? '')}
                    />
                    {hintKey && (
                        <Text c="dimmed" lh={1.35} size="xs">
                            {t(hintKey)}
                        </Text>
                    )}
                </Stack>
            )
        }
        case 'select':
            return <Select {...common} allowDeselect={false} data={(f.options ?? []).map(({ value, labelKey }) => ({ value, label: t(labelKey) }))} onChange={set} value={String(v ?? '')} />
        case 'textarea':
            return <Textarea {...common} autosize minRows={3} onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
        default:
            return (
                <TextInput
                    {...common}
                    autoCapitalize={f.name === 'base_currency' ? 'characters' : 'off'}
                    autoComplete="off"
                    inputMode={f.input}
                    maxLength={f.maxLength}
                    onChange={(e) => set(e.currentTarget.value)}
                    spellCheck={false}
                    value={String(v ?? '')}
                />
            )
    }
}

function Rows({ ctl, fields }: { ctl: SectionCtl; fields: Field[] }) {
    const { t } = useTranslation('admin')
    return fields.map((f) => (
        <SettingsRow description={f.hintKey ? t(f.hintKey) : undefined} inline={f.kind === 'bool'} key={f.name} label={t(f.labelKey)}>
            <FieldInput base={ctl.base[f.name]} f={f} set={(x) => ctl.set(f.name, x)} v={ctl.v[f.name]} />
        </SettingsRow>
    ))
}

/** Fields of a section as titled cards (the general settings screen). */
export function SectionGroups({ ctl }: { ctl: SectionCtl }) {
    const { t } = useTranslation('admin')
    return (
        <Stack className={classes.form} gap="md">
            {fieldGroups(ctl.def.fields).map((g) => (
                <SettingsGroup key={g.key} title={g.key ? t(g.key) : ''}>
                    <Rows ctl={ctl} fields={g.fields} />
                </SettingsGroup>
            ))}
        </Stack>
    )
}

/** Fields of a section as blocks with small titles (the body of a provider card). */
export function SectionBlocks({ ctl, webhookBase, lost, intro }: { ctl: SectionCtl; webhookBase: string; lost?: boolean; intro?: ReactNode }) {
    const { t } = useTranslation('admin')
    const { def } = ctl
    const groups = fieldGroups(def.fields.filter((f) => f.name !== 'enabled'))
    return (
        <div className={classes.form}>
            {(intro || lost) && (
                <div className={classes.intro}>
                    {intro && (
                        <Text c="dimmed" lh={1.4} size="xs">
                            {intro}
                        </Text>
                    )}
                    {lost && (
                        <Text c="yellow" lh={1.4} size="xs">
                            {t('settings.lostHere')}
                        </Text>
                    )}
                </div>
            )}
            {groups.map((g) => (
                <section key={g.key}>
                    <Text className={classes.blockTitle}>{g.key ? t(g.key) : ''}</Text>
                    <Rows ctl={ctl} fields={g.fields} />
                </section>
            ))}
            {def.webhook && (
                <section>
                    <Text className={classes.blockTitle}>{t('settings.notifications')}</Text>
                    <SettingsRow description={t('settings.webhookHint')} label={t('settings.webhook')}>
                        <Stack gap="sm">
                            <CopyableFieldShared size="md" value={`${webhookBase}/pay/${def.webhook}`} />
                            <Button fullWidth loading={ctl.busy === 'test'} onClick={ctl.test} size="md" variant="default">
                                {t('settings.test')}
                            </Button>
                        </Stack>
                    </SettingsRow>
                </section>
            )}
        </div>
    )
}

/** The one sticky bar of a screen: for `ctl`, the section being edited. */
export function SaveBar({ ctl }: { ctl: SectionCtl | undefined }) {
    const { t } = useTranslation('admin')
    const msg = ctl?.msg
    return (
        <ActionBar message={msg ? msg.text : ctl?.dirty ? t('settings.unsaved') : undefined} mounted={!!ctl} title={ctl ? t(ctl.def.titleKey) : undefined} tone={msg ? (msg.ok ? 'ok' : 'error') : undefined}>
            {ctl?.dirty ? (
                <>
                    <Button disabled={!!ctl.busy} onClick={ctl.discard} size="md" variant="default">
                        {t('settings.discard')}
                    </Button>
                    <Button loading={ctl.busy === 'save'} onClick={ctl.save} size="md" variant="filled">
                        {t('settings.save')}
                    </Button>
                </>
            ) : (
                <ActionIcon aria-label={t('settings.close')} onClick={ctl?.dismiss} size={40}>
                    <IconX size={18} stroke={1.75} />
                </ActionIcon>
            )}
        </ActionBar>
    )
}
