import { Anchor, Image, Loader, Text } from '@mantine/core'
import { useEffect, useState } from 'react'

import { fetchReceipt } from '../../adminApi'
import { A } from './text'

// Shows the receipt file of an order: a photo inline, a document only as a download link
// (never rendered or opened in the app origin).
export function Receipt({ id, kind }: { id: string; kind: 'photo' | 'document' }) {
    const [url, setUrl] = useState<string | null>(null)
    const [failed, setFailed] = useState(false)
    useEffect(() => {
        let made: string | null = null
        let alive = true
        fetchReceipt(id)
            .then((b) => {
                if (!alive) return
                made = URL.createObjectURL(b)
                setUrl(made)
            })
            .catch(() => alive && setFailed(true))
        return () => {
            alive = false
            if (made) URL.revokeObjectURL(made)
        }
    }, [id])
    if (failed) return <Text c="red" size="sm">{A.order.receiptFailed}</Text>
    if (!url) return <Loader size="sm" />
    return kind === 'photo' ? <Image alt={A.order.receiptAlt} radius="md" src={url} /> : <Anchor download href={url}>{A.order.receiptOpen}</Anchor>
}
