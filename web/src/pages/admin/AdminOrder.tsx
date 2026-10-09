import { Alert, Badge, Button, Card, Group, NumberInput, Stack, Text, Textarea, Title } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { fmtMoney } from '@shared/format'
import { ApiError } from '../../api'
import { approveOrder, rejectOrder, retryOrder, useAdminOrder } from '../../adminApi'
import type { AdminOrder as Order, Decision } from '../../types'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { Receipt } from './Receipt'
import { A, errMsg } from './text'

function Actions({ o, err, setErr }: { o: Order; err: string | null; setErr: (e: string | null) => void }) {
    const qc = useQueryClient()
    const initial = o.received ?? o.to_pay
    // Kept as typed: the input emits strings such as "1." while the admin is typing.
    const [amount, setAmount] = useState<number | string>(initial)
    const [reason, setReason] = useState('')
    const [busy, setBusy] = useState(false)
    const run = async (fn: () => Promise<Order>) => {
        setBusy(true)
        setErr(null)
        try {
            const next = await fn()
            qc.setQueryData(['admin', 'order', o.id], next)
            await qc.invalidateQueries({ queryKey: ['admin', 'orders'] })
        } catch (e) {
            setErr(e instanceof ApiError ? errMsg(e.code, e.detail) : String(e))
            await qc.invalidateQueries({ queryKey: ['admin', 'order', o.id] })
        } finally {
            setBusy(false)
        }
    }
    const n = typeof amount === 'number' ? amount : amount.trim() === '' ? NaN : Number(amount)
    const valid = Number.isFinite(n) && n > 0
    // Send the amount only when the admin changed it.
    const sent = valid && n !== initial ? n : undefined
    const decide = (d: Decision) => run(() => approveOrder(o.id, d, sent))
    const under = valid && n < o.to_pay

    if (o.status === 'failed') {
        return (
            <Stack>
                {err && <Alert color="red">{err}</Alert>}
                <Button loading={busy} onClick={() => run(() => retryOrder(o.id))}>{A.order.retry}</Button>
            </Stack>
        )
    }
    if (o.status !== 'pending_review') return err ? <Alert color="red">{err}</Alert> : null
    return (
        <Stack>
            <NumberInput decimalScale={2} error={valid ? undefined : A.order.amountInvalid} label={A.order.amount} min={0} onChange={setAmount} value={amount} />
            {err && <Alert color="red">{err}</Alert>}
            {valid && !under && <Button loading={busy} onClick={() => decide('full')}>{A.order.approve}</Button>}
            {under && o.kind === 'renew' && <Button loading={busy} onClick={() => decide('debt')}>{A.order.debt}</Button>}
            {under && <Button loading={busy} onClick={() => decide('credit')} variant="light">{A.order.credit}</Button>}
            <Textarea autosize label={A.order.reason} minRows={1} onChange={(e) => setReason(e.currentTarget.value)} value={reason} />
            <Button color="red" loading={busy} onClick={() => run(() => rejectOrder(o.id, reason))} variant="light">{A.order.reject}</Button>
        </Stack>
    )
}

export function AdminOrder() {
    const { id = '' } = useParams()
    const navigate = useNavigate()
    const q = useAdminOrder(id)
    // Outside the keyed child, so a failure survives the remount a refetch causes.
    const [err, setErr] = useState<string | null>(null)
    if (q.isPending) return <PageSkeleton />
    if (q.isError) return <Alert color="red">{q.error instanceof ApiError ? errMsg(q.error.code, q.error.detail) : A.order.loadFailed}</Alert>
    const o = q.data
    return (
        <Stack gap="md">
            <Button onClick={() => navigate('/admin')} variant="subtle" w="fit-content">{A.order.back}</Button>
            <Group justify="space-between">
                <Title order={4}>№{o.number}</Title>
                <Badge variant="light">{A.status[o.status] ?? o.status}</Badge>
            </Group>
            <Card padding="md" radius="lg" withBorder>
                <Stack gap={4}>
                    <Text fw={600}>{o.title}</Text>
                    <Text size="sm">{fmtMoney(o.price, 2)}{o.from_balance > 0 ? ` · с баланса −${fmtMoney(o.from_balance, 2)}` : ''} · к оплате <b>{fmtMoney(o.to_pay, 2)}</b></Text>
                    <Text c="dimmed" size="sm">{A.order.customer}: {o.customer_name ?? ''} #{o.customer_id} · tg {o.telegram_id}</Text>
                    <Text c="dimmed" size="sm">{A.order.method}: {o.method}</Text>
                    {o.admin && <Text c="dimmed" size="sm">{A.order.decidedBy(o.admin)}</Text>}
                    {o.reason && <Text size="sm">{o.reason}</Text>}
                </Stack>
            </Card>
            {o.comment && <Alert title={A.order.comment}>{o.comment}</Alert>}
            {o.error && <Alert color="red" title={A.order.failed}>{o.error}</Alert>}
            {(o.receipt_kind === 'photo' || o.receipt_kind === 'document') && <Receipt id={o.id} kind={o.receipt_kind} />}
            <Actions err={err} key={`${o.status}:${o.received}`} o={o} setErr={setErr} />
        </Stack>
    )
}
