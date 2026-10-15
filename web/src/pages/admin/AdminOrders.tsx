import { Alert, Button, SegmentedControl, Stack, Text } from '@mantine/core'
import { IconInbox } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'

import { fmtDate, fmtMoney } from '@shared/format'
import { SectionCard } from '@shared/ui/section-card'
import { ShimmerSkeleton } from '@shared/ui/shimmer-skeleton'
import { Dot } from '@shared/ui/status'
import { useAdminOrders } from '../../adminApi'
import classes from './admin-orders.module.css'
import { OrderStatusPill, statusColor } from './status'
import { useTranslation } from 'react-i18next'

const FILTERS: Record<string, string> = { review: 'pending_review,failed', all: 'all', failed: 'failed' }
const PAGE = 20 // matches the server's page size

export function AdminOrders() {
    const [filter, setFilter] = useState('review')
    const { t } = useTranslation('admin')
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
                classNames={{ root: classes.filter, indicator: classes.indicator }}
                data={Object.keys(FILTERS).map((k) => ({ value: k, label: t(`orders.filters.${k}`) }))}
                fullWidth
                onChange={(v) => {
                    setFilter(v)
                    setPage(0)
                }}
                value={filter}
            />
            {q.data && q.data.unreachable_admins.length > 0 && (
                <Alert color="yellow" variant="soft">
                    {t('orders.unreachable', { ids: q.data.unreachable_admins.join(', ') })}
                </Alert>
            )}
            {q.isPending ? (
                <Stack gap="xs">
                    <ShimmerSkeleton height={64} radius="lg" width="100%" />
                    <ShimmerSkeleton height={64} radius="lg" width="100%" />
                    <ShimmerSkeleton height={64} radius="lg" width="100%" />
                </Stack>
            ) : q.isError ? (
                <Alert color="red" variant="soft">
                    <Stack align="flex-start" gap="xs">
                        <Text size="sm">{t('orders.loadFailed')}</Text>
                        <Button onClick={() => q.refetch()} size="compact-sm" variant="light">
                            {t('orders.retry')}
                        </Button>
                    </Stack>
                </Alert>
            ) : q.data && q.data.orders.length > 0 ? (
                <SectionCard.Root gap={0} p={0}>
                    {q.data.orders.map((o) => (
                        <SectionCard.Section key={o.id}>
                            <button className={classes.row} onClick={() => navigate(`/admin/orders/${o.id}`)} type="button">
                                <Dot color={statusColor(o.status)} pulse={o.status === 'pending_review'} />
                                <div className={classes.main}>
                                    <Text fw={600} size="sm" truncate="end">
                                        №{o.number} · {o.title}
                                    </Text>
                                    <Text c="dimmed" mt={2} size="xs" truncate="end">
                                        {fmtDate(o.created_at)} · {t('orders.customer', { id: o.customer_id })}
                                    </Text>
                                </div>
                                <div className={classes.side}>
                                    <Text data-num fw={600} size="sm">
                                        {fmtMoney(o.paid, 2)}
                                    </Text>
                                    <OrderStatusPill size="sm" status={o.status} />
                                </div>
                            </button>
                        </SectionCard.Section>
                    ))}
                </SectionCard.Root>
            ) : (
                <SectionCard.Root>
                    <div className="remnac-empty">
                        <IconInbox size={28} stroke={1.75} />
                        <Text c="dimmed" mt={6} size="sm">
                            {t('orders.empty')}
                        </Text>
                    </div>
                </SectionCard.Root>
            )}
            {q.data && (q.data.total > PAGE || page > 0) && (
                <div className={classes.pager}>
                    <Button disabled={page === 0} onClick={() => setPage(page - 1)} variant="default">
                        {t('orders.prev')}
                    </Button>
                    <Text c="dimmed" size="sm">
                        {t('orders.page', { n: page + 1, of: pages })}
                    </Text>
                    <Button disabled={page + 1 >= pages} onClick={() => setPage(page + 1)} variant="default">
                        {t('orders.next')}
                    </Button>
                </div>
            )}
        </Stack>
    )
}
