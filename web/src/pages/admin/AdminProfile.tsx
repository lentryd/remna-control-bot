import { Alert, Button, Stack, Textarea, TextInput } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ActionBar } from '@shared/ui/action-bar'
import { SettingsGroup, SettingsRow } from '@shared/ui/settings-row'
import { ApiError } from '../../api'
import { saveProfile, useAdminSettings } from '../../adminApi'
import { SettingsSkeleton } from './AdminMethods'
import type { AdminSettings, ProfileApply, ProfileTexts } from '../../types'

// Telegram's limits, in characters; the server checks them too.
const LIMITS = { name: 64, description: 512, about: 120 } as const
type Field = keyof typeof LIMITS
const FIELDS: Field[] = ['name', 'about', 'description']

type Texts = Record<string, ProfileTexts>
type Msg = { ok: boolean; text: string }

const empty: ProfileTexts = { name: '', description: '', about: '' }
const same = (a: Texts, b: Texts, langs: string[]) => langs.every((l) => FIELDS.every((f) => (a[l]?.[f] ?? '') === (b[l]?.[f] ?? '')))
// The form always shows the text in use: the admin's own, or the built-in one where nothing is overridden.
const effective = (saved: Texts, defaults: Texts, langs: string[]): Texts =>
    Object.fromEntries(langs.map((l) => [l, Object.fromEntries(FIELDS.map((f) => [f, saved[l]?.[f] || defaults[l]?.[f] || ''])) as unknown as ProfileTexts]))

export function AdminProfile() {
    const { t } = useTranslation('admin')
    const q = useAdminSettings()
    if (q.isPending) return <SettingsSkeleton />
    if (q.isError) return <Alert color="red" variant="soft">{t('settings.loadFailed')}</Alert>
    return <ProfileForm settings={q.data} />
}

function ProfileForm({ settings }: { settings: AdminSettings }) {
    const { t, i18n } = useTranslation('admin')
    const qc = useQueryClient()
    const defaults = settings.profile_defaults ?? {}
    const langs = Object.keys(defaults)
    const shown = (texts: Texts) => effective(texts, defaults, langs)
    const initial = shown((settings.sections.profile?.texts ?? {}) as Texts)
    const [base, setBase] = useState<Texts>(initial)
    const [v, setV] = useState<Texts>(initial)
    const [msg, setMsg] = useState<Msg | null>(null)
    const [busy, setBusy] = useState(false)
    const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
    const dirty = !same(v, base, langs)
    const langName = (l: string) => new Intl.DisplayNames([i18n.language], { type: 'language' }).of(l) ?? l

    const set = (lang: string, field: Field, x: string) => {
        setMsg(null)
        setV((s) => ({ ...s, [lang]: { ...empty, ...s[lang], [field]: x } }))
    }

    const when = (sec: number) => (sec >= 3600 ? t('profile.hours', { n: Math.ceil(sec / 3600) }) : t('profile.minutes', { n: Math.max(1, Math.ceil(sec / 60)) }))
    const report = (a: ProfileApply): Msg => {
        if (a.retry_after > 0) return { ok: false, text: t('profile.limited', { when: when(a.retry_after) }) }
        if (a.failed.length > 0) return { ok: false, text: t('profile.failed', { what: a.failed.map((f) => `${langName(f.lang)}: ${t(`profile.${f.field}.label`)} — ${f.message}`).join('; ') }) }
        return { ok: true, text: t('profile.applied') }
    }

    const save = async () => {
        setBusy(true)
        clearTimeout(timer.current)
        try {
            const body: Texts = Object.fromEntries(langs.map((l) => [l, Object.fromEntries(FIELDS.map((f) => [f, (v[l]?.[f] ?? '').trim()])) as unknown as ProfileTexts]))
            const r = await saveProfile(body)
            // The texts are stored whatever Telegram said, so the form now matches the server.
            const next = shown(r.texts)
            setBase(next)
            setV(next)
            setMsg(r.apply ? report(r.apply) : { ok: true, text: t('settings.saved') })
            await qc.invalidateQueries({ queryKey: ['admin', 'settings'] })
            if (!r.apply || report(r.apply).ok) timer.current = setTimeout(() => setMsg(null), 4000)
        } catch (e) {
            setMsg({ ok: false, text: e instanceof ApiError ? (e.detail ?? e.code) : e instanceof Error ? e.message : String(e) })
        } finally {
            setBusy(false)
        }
    }

    return (
        <Stack gap="md">
            <Alert color="blue" variant="soft">{t('profile.intro')}</Alert>
            {langs.map((l) => (
                <SettingsGroup key={l} title={langName(l)}>
                    {FIELDS.map((f) => {
                        const value = v[l]?.[f] ?? ''
                        const Input = f === 'name' ? TextInput : Textarea
                        return (
                            <SettingsRow description={t(`profile.${f}.hint`, { max: LIMITS[f] })} key={f} label={t(`profile.${f}.label`)}>
                                <Input
                                    {...(f === 'name' ? {} : { autosize: true, minRows: f === 'about' ? 2 : 4 })}
                                    aria-label={`${langName(l)}: ${t(`profile.${f}.label`)}`}
                                    maxLength={LIMITS[f]}
                                    onChange={(e) => set(l, f, e.currentTarget.value)}
                                    size="md"
                                    value={value}
                                />
                            </SettingsRow>
                        )
                    })}
                </SettingsGroup>
            ))}
            <ActionBar message={msg ? msg.text : dirty ? t('settings.unsaved') : undefined} mounted={dirty || !!msg} title={t('profile.title')} tone={msg ? (msg.ok ? 'ok' : 'error') : undefined}>
                {dirty ? (
                    <>
                        <Button disabled={busy} onClick={() => { setV(base); setMsg(null) }} size="md" variant="default">
                            {t('settings.discard')}
                        </Button>
                        <Button loading={busy} onClick={save} size="md" variant="filled">
                            {t('settings.save')}
                        </Button>
                    </>
                ) : undefined}
            </ActionBar>
        </Stack>
    )
}
