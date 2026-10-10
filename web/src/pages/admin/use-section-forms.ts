import { useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../../api'
import { checkSection, saveSection } from '../../adminApi'
import type { SectionDef } from './sections'

export type Values = Record<string, unknown>
export type Msg = { ok: boolean; text: string }

type Entry = { v: Values; base: Values; msg: Msg | null; busy: 'save' | 'test' | null }

/** One section's form: edited values, what is saved, and the actions. */
export type SectionCtl = {
    def: SectionDef
    v: Values
    base: Values
    msg: Msg | null
    busy: 'save' | 'test' | null
    dirty: boolean
    set: (name: string, x: unknown) => void
    save: () => void
    discard: () => void
    test: () => void
    dismiss: () => void
}

const toForm = (def: SectionDef, v: Values): Values =>
    Object.fromEntries(def.fields.map((f) => [f.name, f.kind === 'tags' ? ((v[f.name] as number[] | undefined) ?? []).map(String) : v[f.name]]))

export const isDay = (d: string) => /^\d+$/.test(d.trim()) && Number(d) > 0

// Returns the request body, or the first validation message.
function toBody(t: TFunction<'admin'>, def: SectionDef, v: Values): { body: Values } | { error: string } {
    const out: Values = {}
    for (const f of def.fields) {
        const x = v[f.name]
        if (f.kind === 'number') {
            const n = typeof x === 'number' ? x : typeof x === 'string' && x.trim() !== '' ? Number(x) : NaN
            if (!Number.isFinite(n)) return { error: t('settings.badNumber', { label: t(f.labelKey) }) }
            out[f.name] = n
        } else if (f.kind === 'tags') {
            const parts = ((x as string[] | undefined) ?? []).map((d) => d.trim()).filter((d) => d !== '')
            const days = parts.map((d) => (/^\d+$/.test(d) ? Number(d) : NaN))
            if (days.some((d) => !Number.isInteger(d) || d <= 0)) return { error: t('settings.badDays', { label: t(f.labelKey) }) }
            out[f.name] = days
        } else out[f.name] = x
    }
    return { body: out }
}

const errorText = (e: unknown) => (e instanceof ApiError ? (e.detail ?? e.code) : e instanceof Error ? e.message : String(e))

/**
 * State of several sections lifted into one place, so opening another section
 * (or leaving a card collapsed) never loses what was typed.
 */
export function useSectionForms(defs: SectionDef[], sections: Record<string, Values>): Record<string, SectionCtl> {
    const { t } = useTranslation('admin')
    const qc = useQueryClient()
    const [st, setSt] = useState<Record<string, Entry>>(() =>
        Object.fromEntries(defs.map((d) => [d.key, { v: toForm(d, sections[d.key] ?? {}), base: toForm(d, sections[d.key] ?? {}), msg: null, busy: null }]))
    )
    const ref = useRef(st)
    ref.current = st
    const patch = (key: string, p: Partial<Entry>) => setSt((s) => ({ ...s, [key]: { ...s[key], ...p } }))

    const run = async (key: string, busy: 'save' | 'test', fn: () => Promise<string>) => {
        patch(key, { busy })
        try {
            const text = await fn()
            patch(key, { msg: { ok: true, text }, busy: null })
            // A result is read in a moment; an error stays until the next edit.
            setTimeout(() => setSt((s) => (s[key].msg?.text === text && s[key].msg.ok ? { ...s, [key]: { ...s[key], msg: null } } : s)), 4000)
        } catch (e) {
            patch(key, { msg: { ok: false, text: errorText(e) }, busy: null })
        }
    }

    return Object.fromEntries(
        defs.map((def) => {
            const key = def.key
            const e = st[key]
            const ctl: SectionCtl = {
                def,
                v: e.v,
                base: e.base,
                msg: e.msg,
                busy: e.busy,
                dirty: def.fields.some((f) => String(e.v[f.name] ?? '') !== String(e.base[f.name] ?? '')),
                set: (name, x) => setSt((s) => ({ ...s, [key]: { ...s[key], msg: null, v: { ...s[key].v, [name]: x } } })),
                discard: () => patch(key, { v: ref.current[key].base, msg: null }),
                dismiss: () => patch(key, { msg: null }),
                save: () =>
                    run(key, 'save', async () => {
                        const r = toBody(t, def, ref.current[key].v)
                        if ('error' in r) throw new Error(r.error)
                        const saved = toForm(def, await saveSection(key, r.body))
                        patch(key, { v: saved, base: saved })
                        await qc.invalidateQueries({ queryKey: ['admin', 'settings'] })
                        return t('settings.saved')
                    }),
                test: () =>
                    run(key, 'test', async () => {
                        try {
                            const r = await checkSection(key)
                            if (!r.supported) return t('settings.noCheck')
                            if (!r.ok) throw new Error(r.message ?? t('settings.checkFailed'))
                            return r.message ?? 'OK'
                        } catch (err) {
                            if (err instanceof ApiError && err.code === 'not_active') throw new Error(t('settings.notActive'))
                            throw err
                        }
                    })
            }
            return [key, ctl]
        })
    )
}

/** Whether the values hold everything an enabled section needs (the server's required fields). */
export function isComplete(def: SectionDef, v: Values): boolean {
    return (def.required ?? []).every((name) => {
        const x = v[name]
        return typeof x === 'number' ? x > 0 : typeof x === 'string' && (def.fields.find((f) => f.name === name)?.kind === 'number' ? Number(x) > 0 : x.trim() !== '')
    })
}
