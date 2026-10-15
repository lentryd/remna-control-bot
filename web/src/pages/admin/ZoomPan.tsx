import { Box } from '@mantine/core'
import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'

const MAX_SCALE = 6
const SETTLE_MS = 180

type Pt = { x: number; y: number }
// Safari (macOS) reports a trackpad pinch as gesture events instead of ctrl+wheel.
type GestureEvent = Event & { scale: number; clientX: number; clientY: number }

// A viewer the way people expect it to behave: pinch on a touchscreen or trackpad,
// ctrl/⌘ + wheel, drag to move, scroll (two fingers or wheel) to move, double tap or
// double click to zoom in and out. The content is laid out to fit the width; zooming
// only transforms it, and onSettled tells the content to redraw sharply at the new scale.
export function ZoomPan({ children, onSettled }: { children: ReactNode; onSettled?: (scale: number) => void }) {
    const box = useRef<HTMLDivElement>(null)
    const content = useRef<HTMLDivElement>(null)
    const settled = useRef(onSettled)
    settled.current = onSettled

    useEffect(() => {
        const el = box.current
        const inner = content.current
        if (!el || !inner) return
        const v = { s: 1, x: 0, y: 0 }
        const pointers = new Map<number, Pt>()
        let last: { dist: number; mid: Pt } | null = null
        let tap: { t: number; p: Pt } | null = null
        let moved = false
        let timer: ReturnType<typeof setTimeout> | undefined

        const apply = () => {
            const vw = el.clientWidth, vh = el.clientHeight
            const w = inner.offsetWidth * v.s, h = inner.offsetHeight * v.s
            v.x = w <= vw ? (vw - w) / 2 : Math.min(0, Math.max(vw - w, v.x))
            v.y = h <= vh ? (vh - h) / 2 : Math.min(0, Math.max(vh - h, v.y))
            inner.style.transform = `translate(${v.x}px, ${v.y}px) scale(${v.s})`
            clearTimeout(timer)
            timer = setTimeout(() => settled.current?.(v.s), SETTLE_MS)
        }
        const local = (e: { clientX: number; clientY: number }): Pt => {
            const r = el.getBoundingClientRect()
            return { x: e.clientX - r.left, y: e.clientY - r.top }
        }
        const zoomAt = (p: Pt, s: number) => {
            const next = Math.min(MAX_SCALE, Math.max(1, s))
            v.x = p.x - ((p.x - v.x) * next) / v.s
            v.y = p.y - ((p.y - v.y) * next) / v.s
            v.s = next
        }
        const pair = () => {
            const [a, b] = [...pointers.values()]
            return { dist: Math.hypot(a.x - b.x, a.y - b.y) || 1, mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } }
        }

        const down = (e: PointerEvent) => {
            el.setPointerCapture(e.pointerId)
            pointers.set(e.pointerId, local(e))
            if (pointers.size === 1) moved = false
            if (pointers.size === 2) last = pair()
        }
        const move = (e: PointerEvent) => {
            const prev = pointers.get(e.pointerId)
            if (!prev) return
            const p = local(e)
            pointers.set(e.pointerId, p)
            if (pointers.size === 1) {
                if (Math.hypot(p.x - prev.x, p.y - prev.y) > 0) moved = true
                v.x += p.x - prev.x
                v.y += p.y - prev.y
            } else if (pointers.size === 2 && last) {
                moved = true
                const now = pair()
                zoomAt(now.mid, v.s * (now.dist / last.dist))
                v.x += now.mid.x - last.mid.x
                v.y += now.mid.y - last.mid.y
                last = now
            }
            apply()
        }
        const up = (e: PointerEvent) => {
            const p = local(e)
            const was = pointers.delete(e.pointerId)
            last = null
            if (!was || pointers.size > 0 || moved) return
            // a tap that did not move: the second one in a row zooms
            const now = Date.now()
            if (tap && now - tap.t < 320 && Math.hypot(p.x - tap.p.x, p.y - tap.p.y) < 24) {
                zoomAt(p, v.s > 1.05 ? 1 : 2.5)
                apply()
                tap = null
            } else tap = { t: now, p }
        }
        const wheel = (e: WheelEvent) => {
            e.preventDefault()
            if (e.ctrlKey || e.metaKey) {
                zoomAt(local(e), v.s * Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.01)))
            } else {
                v.x -= e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX
                v.y -= e.shiftKey && !e.deltaX ? 0 : e.deltaY
            }
            apply()
        }
        let base = 1
        const gStart = (e: Event) => {
            e.preventDefault()
            base = v.s
        }
        const gChange = (e: Event) => {
            e.preventDefault()
            const g = e as GestureEvent
            zoomAt(local(g), base * g.scale)
            apply()
        }

        el.addEventListener('pointerdown', down)
        el.addEventListener('pointermove', move)
        el.addEventListener('pointerup', up)
        el.addEventListener('pointercancel', up)
        el.addEventListener('wheel', wheel, { passive: false })
        el.addEventListener('gesturestart', gStart)
        el.addEventListener('gesturechange', gChange)
        const ro = new ResizeObserver(apply)
        ro.observe(el)
        ro.observe(inner)
        return () => {
            clearTimeout(timer)
            ro.disconnect()
            el.removeEventListener('pointerdown', down)
            el.removeEventListener('pointermove', move)
            el.removeEventListener('pointerup', up)
            el.removeEventListener('pointercancel', up)
            el.removeEventListener('wheel', wheel)
            el.removeEventListener('gesturestart', gStart)
            el.removeEventListener('gesturechange', gChange)
        }
    }, [])

    return (
        <Box ref={box} style={{ height: '100%', overflow: 'hidden', touchAction: 'none', userSelect: 'none', cursor: 'grab' }}>
            <Box ref={content} style={{ transformOrigin: '0 0', width: '100%', willChange: 'transform' }}>
                {children}
            </Box>
        </Box>
    )
}
