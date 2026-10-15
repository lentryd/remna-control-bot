// Rebuilds dist (scripts/build.ts) on every frontend change. The bot serves dist from disk in
// `task dev`, so nothing restarts: reload the WebApp.
import { watch } from 'node:fs'

const root = new URL('..', import.meta.url).pathname
let running = false
let queued = false
let timer: ReturnType<typeof setTimeout> | undefined

// A change during a build queues exactly one more build.
async function build() {
    if (running) return void (queued = true)
    running = true
    console.log('[watch] building')
    const ok = (await Bun.spawn(['bun', 'scripts/build.ts'], { cwd: root, stdout: 'inherit', stderr: 'inherit' }).exited) === 0
    console.log(ok ? '[watch] dist is up to date' : '[watch] build failed, waiting for changes')
    running = false
    if (queued) {
        queued = false
        void build()
    }
}

for (const p of ['src', 'public', 'index.html']) {
    watch(`${root}${p}`, { recursive: true }, () => {
        clearTimeout(timer)
        timer = setTimeout(build, 250)
    })
}
for (const sig of ['SIGINT', 'SIGTERM'] as const) process.on(sig, () => process.exit(0))
void build()
