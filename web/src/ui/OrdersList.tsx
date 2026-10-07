// OrdersList — the customer's payment requests (last 20) with actions for
// the ones still waiting for a receipt.
import { Badge, Button, Group, Stack, Text } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { fmtDate, fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { cancelOrder, requestReceipt, useOrders } from '../api'
import { tg } from '../telegram'
import type { Order } from '../types'

const colors: Record<Order['status'], string> = {
    new: 'gray',
    awaiting_receipt: 'yellow',
    pending_review: 'yellow',
    approving: 'yellow',
    approved: 'teal',
    credited: 'blue',
    failed: 'orange',
    rejected: 'gray',
    expired: 'gray',
    cancelled: 'gray',
}

export function OrdersList() {
    const { t } = useTranslation()
    const qc = useQueryClient()
    const q = useOrders()
    const list = (q.data?.orders ?? []).slice(0, 20)
    const [pending, setPending] = useState<string | null>(null)
    const act = (id: string, fn: () => Promise<unknown>) => {
        setPending(id)
        fn()
            .catch(() => undefined)
            .finally(() => {
                setPending(null)
                qc.invalidateQueries({ queryKey: ['orders'] })
            })
    }
    const receipt = (id: string) => act(id, () => requestReceipt(id).then(() => tg?.close()))
    const cancel = (id: string) => act(id, () => cancelOrder(id))
    if (list.length === 0) return null
    return (
        <Stack gap="xs">
            <Text fw={600}>{t('orders.title')}</Text>
            <SectionCard.Root gap="sm">
                {list.map((o) => (
                    <Stack gap={4} key={o.id}>
                        <Group justify="space-between" wrap="nowrap">
                            <Stack gap={0} style={{ minWidth: 0 }}>
                                <Text size="sm" truncate="end">{o.title}</Text>
                                <Text c="dimmed" size="xs">#{o.number} · {fmtDate(o.created_at)}</Text>
                            </Stack>
                            <Stack align="flex-end" gap={2}>
                                <Badge color={colors[o.status]} variant="light">{t(`orders.status.${o.status}`, { defaultValue: o.status })}</Badge>
                                <Text fw={600} size="sm">{fmtMoney(o.paid, 2)}</Text>
                            </Stack>
                        </Group>
                        {o.status === 'rejected' && o.reason && <Text c="dimmed" size="xs">{o.reason}</Text>}
                        {o.status === 'failed' && <Text c="orange" size="xs">{t('orders.failed')}</Text>}
                        {o.status === 'awaiting_receipt' && (
                            <Group gap="xs">
                                <Button disabled={pending !== null} loading={pending === o.id} onClick={() => receipt(o.id)} radius="xl" size="xs" variant="light">{t('orders.send_receipt')}</Button>
                                <Button color="gray" disabled={pending !== null} onClick={() => cancel(o.id)} radius="xl" size="xs" variant="subtle">{t('orders.cancel')}</Button>
                            </Group>
                        )}
                    </Stack>
                ))}
            </SectionCard.Root>
        </Stack>
    )
}
