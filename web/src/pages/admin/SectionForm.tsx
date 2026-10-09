import { Alert, Button, Card, Group, NumberInput, PasswordInput, Select, Stack, Switch, Text, Textarea, TextInput, Title } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ApiError } from '../../api'
import { checkSection, saveSection } from '../../adminApi'
import { A } from './text'
import type { Field, SectionDef } from './sections'

type Values = Record<string, unknown>

const toForm = (def: SectionDef, v: Values): Values =>
    Object.fromEntries(def.fields.map((f) => [f.name, f.kind === 'days' ? ((v[f.name] as number[]) ?? []).join(',') : v[f.name]]))

// Returns the request body, or the first validation message.
function toBody(def: SectionDef, v: Values): { body: Values } | { error: string } {
    const out: Values = {}
    for (const f of def.fields) {
        const x = v[f.name]
        if (f.kind === 'number') {
            const n = typeof x === 'number' ? x : typeof x === 'string' && x.trim() !== '' ? Number(x) : NaN
            if (!Number.isFinite(n)) return { error: A.settings.badNumber(f.label) }
            out[f.name] = n
        } else if (f.kind === 'days') {
            const parts = String(x ?? '').split(',').map((d) => d.trim()).filter((d) => d !== '')
            const days = parts.map((d) => (/^\d+$/.test(d) ? Number(d) : NaN))
            if (days.some((d) => !Number.isInteger(d) || d <= 0)) return { error: A.settings.badDays(f.label) }
            out[f.name] = days
        } else out[f.name] = x
    }
    return { body: out }
}

function Input({ f, v, set }: { f: Field; v: unknown; set: (x: unknown) => void }) {
    const common = { label: f.label, description: f.hint }
    switch (f.kind) {
        case 'bool':
            return <Switch checked={!!v} label={f.label} onChange={(e) => set(e.currentTarget.checked)} />
        case 'secret':
            // The masked value is sent back unchanged; the server keeps the stored secret.
            return <PasswordInput {...common} autoComplete="off" onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
        case 'number':
            return <NumberInput {...common} allowDecimal={!!f.decimal} onChange={set} value={v as number | string} />
        case 'select':
            return <Select {...common} allowDeselect={false} data={f.options ?? []} onChange={set} value={String(v ?? '')} />
        case 'textarea':
            return <Textarea {...common} autosize minRows={3} onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
        default:
            return <TextInput {...common} onChange={(e) => set(e.currentTarget.value)} value={String(v ?? '')} />
    }
}

const errorText = (e: unknown) => (e instanceof ApiError ? (e.detail ?? e.code) : e instanceof Error ? e.message : String(e))

export function SectionForm({ def, initial, webhookBase }: { def: SectionDef; initial: Values; webhookBase: string }) {
    const qc = useQueryClient()
    const [v, setV] = useState<Values>(() => toForm(def, initial))
    const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
    const [busy, setBusy] = useState(false)

    const run = async (fn: () => Promise<string>) => {
        setBusy(true)
        try {
            setMsg({ ok: true, text: await fn() })
        } catch (e) {
            setMsg({ ok: false, text: errorText(e) })
        } finally {
            setBusy(false)
        }
    }
    const save = () =>
        run(async () => {
            const r = toBody(def, v)
            if ('error' in r) throw new Error(r.error)
            const saved = await saveSection(def.key, r.body)
            setV(toForm(def, saved))
            await qc.invalidateQueries({ queryKey: ['admin', 'settings'] })
            return A.settings.saved
        })
    const test = () =>
        run(async () => {
            try {
                const r = await checkSection(def.key)
                if (!r.supported) return A.settings.noCheck
                if (!r.ok) throw new Error(r.message ?? A.settings.checkFailed)
                return r.message ?? 'OK'
            } catch (e) {
                if (e instanceof ApiError && e.code === 'not_active') throw new Error(A.settings.notActive)
                throw e
            }
        })

    return (
        <Card padding="md" radius="lg" withBorder>
            <Stack gap="sm">
                <Title order={5}>{def.title}</Title>
                {def.fields.map((f) => (
                    <Input f={f} key={f.name} set={(x) => {
                            setMsg(null)
                            setV((p) => ({ ...p, [f.name]: x }))
                        }} v={v[f.name]} />
                ))}
                {def.webhook && (
                    <Text c="dimmed" size="xs">
                        {A.settings.webhook}: {webhookBase}/pay/{def.webhook}
                    </Text>
                )}
                {msg && <Alert color={msg.ok ? 'teal' : 'red'}>{msg.text}</Alert>}
                <Group>
                    <Button loading={busy} onClick={save}>{A.settings.save}</Button>
                    {def.webhook && <Button loading={busy} onClick={test} variant="default">{A.settings.test}</Button>}
                </Group>
            </Stack>
        </Card>
    )
}
