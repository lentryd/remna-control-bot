import { Box, Loader, Stack } from '@mantine/core'
import type { PDFDocumentProxy } from 'pdfjs-dist'
import { useEffect, useRef, useState } from 'react'

const MAX_PAGES = 10
const MAX_SIDE = 4096 // px of a canvas; bigger ones fail on phones

// Draws a PDF onto canvases: the Telegram WebView has no built-in PDF viewer
// and the file may not be opened in the app origin. pdf.js is loaded on demand.
// The pages fit the width; zoom only raises the resolution they are drawn at, so a
// zoomed view (see ZoomPan) stays sharp.
export function PdfView({ blob, zoom = 1, onError }: { blob: Blob; zoom?: number; onError: () => void }) {
    const box = useRef<HTMLDivElement>(null)
    const [doc, setDoc] = useState<PDFDocumentProxy | null>(null)
    const pages = doc ? Math.min(doc.numPages, MAX_PAGES) : 0

    useEffect(() => {
        let alive = true
        let destroy: (() => void) | undefined
        ;(async () => {
            const pdfjs = await import('pdfjs-dist')
            // next to index.html (see scripts/build.ts and dev.ts); the app routes by hash, so the page URL is the base
            pdfjs.GlobalWorkerOptions.workerSrc = new URL('./pdf.worker.min.mjs', document.baseURI).href
            const task = pdfjs.getDocument({ data: new Uint8Array(await blob.arrayBuffer()) })
            destroy = () => void task.destroy()
            const d = await task.promise
            if (alive) setDoc(d)
        })().catch(() => alive && onError())
        return () => {
            alive = false
            destroy?.()
        }
    }, [blob, onError])

    useEffect(() => {
        if (!doc || !box.current) return
        let alive = true
        const width = box.current.clientWidth
        ;(async () => {
            for (let i = 1; i <= pages; i++) {
                const page = await doc.getPage(i)
                const canvas = box.current?.querySelector<HTMLCanvasElement>(`canvas[data-page="${i}"]`)
                if (!alive || !canvas) return
                const base = page.getViewport({ scale: 1 })
                let scale = (width * zoom / base.width) * Math.min(window.devicePixelRatio || 1, 2)
                scale = Math.min(scale, MAX_SIDE / Math.max(base.width, base.height))
                const vp = page.getViewport({ scale })
                canvas.width = vp.width
                canvas.height = vp.height
                await page.render({ canvas, viewport: vp }).promise
            }
        })().catch(() => alive && onError())
        return () => {
            alive = false
        }
    }, [doc, pages, zoom, onError])

    return (
        <Box ref={box}>
            {!doc && <Loader size="sm" />}
            <Stack gap="xs">
                {Array.from({ length: pages }, (_, i) => (
                    <Box component="canvas" data-page={i + 1} key={i} style={{ width: '100%', background: '#fff' }} />
                ))}
            </Stack>
        </Box>
    )
}
