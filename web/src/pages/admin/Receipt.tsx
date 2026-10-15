import { ActionIcon, Alert, Box, Button, Group, Image, Loader, Modal, Stack, Text } from '@mantine/core'
import { IconMaximize } from '@tabler/icons-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { fetchReceipt, sendReceipt } from '../../adminApi'
import { PdfView } from './PdfView'
import { ZoomPan } from './ZoomPan'

type Sent = 'sent' | 'failed' | null

// Shows the receipt file of an order: a photo or a PDF inline, and full screen with
// pinch / wheel / drag zoom on tap. Telegram opens every file type, so the file can
// also be sent to the admin's chat.
export function Receipt({ id }: { id: string }) {
    const { t } = useTranslation('admin')
    const [file, setFile] = useState<{ url: string; blob: Blob } | null>(null)
    const [failed, setFailed] = useState(false)
    const [pdfFailed, setPdfFailed] = useState(false)
    const [busy, setBusy] = useState(false)
    const [sent, setSent] = useState<Sent>(null)
    const [open, setOpen] = useState(false)
    const [sharp, setSharp] = useState(1)
    useEffect(() => {
        let made: string | null = null
        let alive = true
        fetchReceipt(id)
            .then((blob) => {
                if (!alive) return
                made = URL.createObjectURL(blob)
                setFile({ url: made, blob })
            })
            .catch(() => alive && setFailed(true))
        return () => {
            alive = false
            if (made) URL.revokeObjectURL(made)
        }
    }, [id])
    const onPdfError = useCallback(() => setPdfFailed(true), [])
    const send = async () => {
        setBusy(true)
        try {
            await sendReceipt(id)
            setSent('sent')
        } catch {
            setSent('failed')
        } finally {
            setBusy(false)
        }
    }
    if (failed) return <Text c="red" size="sm">{t('order.receiptFailed')}</Text>
    if (!file) return <Loader size="sm" />
    const type = file.blob.type
    const isImage = type.startsWith('image/')
    const isPdf = type === 'application/pdf' && !pdfFailed
    const show = (zoom: number) =>
        isImage ? <Image alt={t('order.receiptAlt')} draggable={false} src={file.url} /> : <PdfView blob={file.blob} onError={onPdfError} zoom={zoom} />
    return (
        <Stack gap="sm">
            {(isImage || isPdf) && (
                <>
                    <Box onClick={() => { setSharp(1); setOpen(true) }} style={{ cursor: 'zoom-in', borderRadius: 8, overflow: 'hidden' }}>
                        {show(1)}
                    </Box>
                    <Modal fullScreen onClose={() => setOpen(false)} opened={open} padding={0} title={t('order.receipt')} styles={{ body: { height: 'calc(100dvh - 60px)' } }}>
                        <ZoomPan onSettled={(s) => setSharp(Math.min(4, Math.max(1, Math.ceil(s))))}>{show(sharp)}</ZoomPan>
                    </Modal>
                </>
            )}
            {pdfFailed && <Text c="red" size="sm">{t('order.receiptPdfFailed')}</Text>}
            <Group gap="sm">
                {(isImage || isPdf) && (
                    <ActionIcon aria-label={t('order.receiptExpand')} onClick={() => { setSharp(1); setOpen(true) }} size="lg" variant="default"><IconMaximize size={18} /></ActionIcon>
                )}
                <Button loading={busy} onClick={send} size="xs" variant="default">{t('order.receiptSend')}</Button>
            </Group>
            {sent === 'sent' && <Alert color="green" variant="soft">{t('order.receiptSent')}</Alert>}
            {sent === 'failed' && <Alert color="red" variant="soft">{t('order.receiptSendFailed')}</Alert>}
        </Stack>
    )
}
