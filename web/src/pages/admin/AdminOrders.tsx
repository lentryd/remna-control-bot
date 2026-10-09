import { Alert, Badge, Button, Card, Group, SegmentedControl, Stack, Text } from '@mantine/core'
import { IconInbox } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'

import { fmtDate, fmtMoney } from '@shared/format'
import { useAdminOrders } from '../../adminApi'
import { Empty } from '../../ui/Empty'
import { PageSkeleton } from '../../ui/PageSkeleton'
import { A } from './text'

const FILTERS: Record<string, string> = { review: 'pending_review,failed', all: 'all', failed: 'failed' }
const PAGE = 20 // matches the server's page size
const COLOR: Record<string, string> = { pending_review: 'yellow', failed: 'red', approved: 'teal', credited: 'teal', rejected: 'gray' }

export function AdminOrders() {
    const [filter, setFilter] = useState('review')
    const navigate = useNavigate()
    const [page, setPage] = useState(0)
    const q = useAdminOrders(FILTERS[filter], page)
    const total = q.data?.total
    const pages = Math.max(1, Math.ceil((total ?? 0) / PAGE))
    // The list may shrink under us (orders get decided); step back to the last real page.
    useEffect(() => {
        if (total !== undefined && page > 0 && page >= pages) setPage(pages - 1)
    }, [total, page, pages])
    return (
        <Stack gap="md">
            <SegmentedControl
                data={Object.keys(FILTERS).map((k) => ({ value: k, label: A.orders.filters[k as keyof typeof A.orders.filters] }))}
                fullWidth
                onChange={(v) => {
                    setFilter(v)
                    setPage(0)
                }}
                value={filter}
            />
            {q.data && q.data.unreachable_admins.length > 0 && <Alert color="yellow">{A.orders.unreachable(q.data.unreachable_admins)}</Alert>}
            {q.isPending ? (
                <PageSkeleton />
            ) : q.isError ? (
                <Alert color="red">
                    <Stack align="flex-start" gap="xs">
                        <Text size="sm">{A.orders.loadFailed}</Text>
                        <Button onClick={() => q.refetch()} size="compact-sm" variant="light">{A.orders.retry}</Button>
                    </Stack>
                </Alert>
            ) : q.data && q.data.orders.length > 0 ? (
                q.data.orders.map((o) => (
                    <Card component="button" key={o.id} onClick={() => navigate(`/admin/orders/${o.id}`)} padding="md" radius="lg" style={{ cursor: 'pointer', textAlign: 'left' }} type="button" withBorder>
                        <Group justify="space-between" wrap="nowrap">
                            <Stack gap={2}>
                                <Text fw={600} size="sm">№{o.number} · {o.title}</Text>
                                <Text c="dimmed" size="xs">{fmtDate(o.created_at)} · клиент #{o.customer_id}</Text>
                            </Stack>
                            <Stack align="flex-end" gap={2}>
                                <Text fw={600}>{fmtMoney(o.paid, 2)}</Text>
                                <Badge color={COLOR[o.status] ?? 'blue'} variant="light">{A.status[o.status] ?? o.status}</Badge>
                            </Stack>
                        </Group>
                    </Card>
                ))
            ) : (
                <Empty icon={<IconInbox />} title={A.orders.empty} />
            )}
            {q.data && (q.data.total > PAGE || page > 0) && (
                <Group justify="space-between">
                    <Button disabled={page === 0} onClick={() => setPage(page - 1)} variant="default">{A.orders.prev}</Button>
                    <Text c="dimmed" size="sm">{A.orders.page(page + 1, pages)}</Text>
                    <Button disabled={page + 1 >= pages} onClick={() => setPage(page + 1)} variant="default">{A.orders.next}</Button>
                </Group>
            )}
        </Stack>
    )
}
