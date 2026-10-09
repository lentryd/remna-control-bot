import { Alert, Button, NumberInput, Stack, Text, Textarea } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useParams } from 'react-router'

import { fmtMoney } from '@shared/format'
import { InfoFieldShared } from '@shared/ui/info-field/info-field'
import { PageHeaderShared } from '@shared/ui/page-header'
import { SectionCard } from '@shared/ui/section-card'
import { ShimmerSkeleton } from '@shared/ui/shimmer-skeleton'
import { BackLink } from '@shared/ui/status'
import { ApiError } from '../../api'
import { approveOrder, rejectOrder, retryOrder, useAdminOrder } from '../../adminApi'
import type { AdminOrder as Order, Decision } from '../../types'
import classes from './admin-order.module.css'
import { Receipt } from './Receipt'
import { OrderStatusPill } from './status'
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

    const card = (children: ReactNode) => (
        <SectionCard.Root>
            <SectionCard.Section>
                <Text className={classes.sectionTitle}>{A.order.decision}</Text>
            </SectionCard.Section>
            <SectionCard.Section>
                <Stack gap="sm">{children}</Stack>
            </SectionCard.Section>
        </SectionCard.Root>
    )
    const alert = err && (
        <Alert color="red" variant="soft">
            {err}
        </Alert>
    )

    if (o.status === 'failed') {
        return card(
            <>
                {alert}
                <Button loading={busy} onClick={() => run(() => retryOrder(o.id))}>{A.order.retry}</Button>
            </>
        )
    }
    if (o.status !== 'pending_review') return err ? <>{alert}</> : null
    return card(
        <>
            <NumberInput decimalScale={2} error={valid ? undefined : A.order.amountInvalid} label={A.order.amount} min={0} onChange={setAmount} value={amount} />
            {alert}
            {valid && !under && <Button loading={busy} onClick={() => decide('full')}>{A.order.approve}</Button>}
            {under && o.kind === 'renew' && <Button loading={busy} onClick={() => decide('debt')}>{A.order.debt}</Button>}
            {under && <Button loading={busy} onClick={() => decide('credit')} variant="light">{A.order.credit}</Button>}
            <Textarea autosize label={A.order.reason} minRows={1} onChange={(e) => setReason(e.currentTarget.value)} value={reason} />
            <Button color="red" loading={busy} onClick={() => run(() => rejectOrder(o.id, reason))} variant="light">{A.order.reject}</Button>
        </>
    )
}

export function AdminOrder() {
    const { id = '' } = useParams()
    const q = useAdminOrder(id)
    // Outside the keyed child, so a failure survives the remount a refetch causes.
    const [err, setErr] = useState<string | null>(null)
    if (q.isPending) {
        return (
            <Stack gap="md">
                <ShimmerSkeleton height={56} radius="md" width="70%" />
                <ShimmerSkeleton height={190} radius="lg" width="100%" />
                <ShimmerSkeleton height={140} radius="lg" width="100%" />
            </Stack>
        )
    }
    if (q.isError) {
        return (
            <Stack gap="md">
                <BackLink to="/admin">{A.order.back}</BackLink>
                <Alert color="red" variant="soft">{q.error instanceof ApiError ? errMsg(q.error.code, q.error.detail) : A.order.loadFailed}</Alert>
            </Stack>
        )
    }
    const o = q.data
    return (
        <Stack gap="md">
            <PageHeaderShared badges={<OrderStatusPill status={o.status} />} description={o.title} eyebrow={<BackLink to="/admin">{A.order.back}</BackLink>} mb={0} title={`№${o.number}`} />
            <SectionCard.Root>
                <SectionCard.Section>
                    <Stack gap="xs">
                        <InfoFieldShared label={A.order.customer} value={`${o.customer_name ?? ''} #${o.customer_id} · tg ${o.telegram_id}`.trim()} />
                        <InfoFieldShared label={A.order.method} value={o.method} />
                        <InfoFieldShared label={A.order.price} value={`${fmtMoney(o.price, 2)}${o.from_balance > 0 ? ` · с баланса −${fmtMoney(o.from_balance, 2)}` : ''}`} />
                        <InfoFieldShared label={A.order.toPay} value={<b data-num>{fmtMoney(o.to_pay, 2)}</b>} />
                        <InfoFieldShared label={A.order.received} value={o.received != null ? fmtMoney(o.received, 2) : null} />
                        {o.admin && <InfoFieldShared label={A.order.decidedByLabel} value={o.admin} />}
                    </Stack>
                </SectionCard.Section>
                {o.reason && (
                    <SectionCard.Section>
                        <Text size="sm">{o.reason}</Text>
                    </SectionCard.Section>
                )}
            </SectionCard.Root>
            {o.comment && <Alert title={A.order.comment} variant="soft">{o.comment}</Alert>}
            {o.error && <Alert color="red" title={A.order.failed} variant="soft">{o.error}</Alert>}
            {(o.receipt_kind === 'photo' || o.receipt_kind === 'document') && (
                <SectionCard.Root>
                    <SectionCard.Section>
                        <Text className={classes.sectionTitle}>{A.order.receipt}</Text>
                    </SectionCard.Section>
                    <SectionCard.Section>
                        <Receipt id={o.id} kind={o.receipt_kind} />
                    </SectionCard.Section>
                </SectionCard.Root>
            )}
            <Actions err={err} key={`${o.status}:${o.received}`} o={o} setErr={setErr} />
        </Stack>
    )
}
