import { StatusPill } from '@shared/ui/status'
import { A } from './text'

// Colours of the order statuses, as the panel colours its own.
export const STATUS_COLOR: Record<string, string> = {
    new: 'gray',
    awaiting_receipt: 'orange',
    awaiting_payment: 'blue',
    pending_review: 'yellow',
    approving: 'blue',
    approved: 'teal',
    credited: 'teal',
    failed: 'red',
    rejected: 'red',
    expired: 'gray',
    cancelled: 'gray'
}

export const statusColor = (status: string) => STATUS_COLOR[status] ?? 'gray'

export function OrderStatusPill({ status, size }: { status: string; size?: 'xs' | 'sm' | 'md' }) {
    return <StatusPill color={statusColor(status)} label={A.status[status] ?? status} pulse={status === 'pending_review' || status === 'approving'} size={size} />
}
