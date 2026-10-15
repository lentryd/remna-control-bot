// Orders.tsx — the customer's payment requests as a page of their own (the bot's "Payments" button).
import { Stack, Title } from '@mantine/core'
import { IconReceipt } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'

import { useOrders } from '../api'
import { Empty } from '../ui/Empty'
import { OrdersList } from '../ui/OrdersList'

export function Orders() {
    const { t } = useTranslation()
    const q = useOrders()
    const none = !q.isPending && (q.data?.orders ?? []).length === 0
    return (
        <Stack gap="lg">
            <Title order={3}>{t('orders.title')}</Title>
            {none ? <Empty icon={<IconReceipt />} title={t('empty.orders')} /> : <OrdersList heading={false} />}
        </Stack>
    )
}
