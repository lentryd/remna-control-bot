// Dev server: bundles index.html on each load and proxies ./api/* to the Go
// server (HTTP_ADDR) so the page can run outside the container.
import index from '../index.html'

const API = process.env.API_URL ?? 'http://localhost:8090/bot'
const publicDir = new URL('../public/', import.meta.url)

const server = Bun.serve({
    port: Number(process.env.DEV_PORT ?? 5174),
    development: { hmr: false },
    routes: {
        '/': index,
        '/api/*': (req) => {
            const url = new URL(req.url)
            return fetch(new Request(API + url.pathname + url.search, req))
        }
    },
    async fetch(req) {
        const file = Bun.file(new URL(`.${new URL(req.url).pathname}`, publicDir))
        return (await file.exists()) ? new Response(file) : new Response('Not Found', { status: 404 })
    }
})
console.log(`WebApp dev server on ${server.url} (api → ${API})`)
