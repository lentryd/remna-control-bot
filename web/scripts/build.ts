// Copied from remna-control/web/scripts/build.ts, adapted: the WebApp lives
// under a configurable path (WEBAPP_PATH), so every asset URL is relative.
import { cp, mkdir, rm } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'

import type { BunPlugin } from 'bun'
import { bundleAsync } from 'lightningcss'

import postcss from './postcss-plugin'

const root = new URL('..', import.meta.url).pathname
const outdir = `${root}dist`
const vendorCss = `${root}src/app/vendor.css`

await rm(outdir, { recursive: true, force: true })

// Bun inlines every font into the CSS as a data: URL (~1 MB of subsets the
// browser would otherwise fetch lazily by unicode-range). Keep the fontsource
// url(./files/x.woff2) references as they are and copy the files next to the CSS.
const fonts = new Map<string, string>()
const externalFonts: BunPlugin = {
    name: 'external-fonts',
    setup(build) {
        build.onResolve({ filter: /\.woff2?$/ }, ({ path, importer }) => {
            fonts.set(path, resolve(dirname(importer), path))
            return { path, external: true }
        })
    }
}

// Library styles are built separately below (see src/app/vendor.css).
const skipVendorCss: BunPlugin = {
    name: 'skip-vendor-css',
    setup(build) {
        build.onLoad({ filter: /[\\/]src[\\/]app[\\/]vendor\.css$/ }, () => ({ contents: '', loader: 'css' }))
    }
}

const result = await Bun.build({
    entrypoints: [`${root}index.html`],
    outdir,
    target: 'browser',
    minify: true,
    splitting: true,
    sourcemap: 'none',
    publicPath: './',
    naming: { chunk: 'assets/[name]-[hash].[ext]', asset: 'assets/[name]-[hash].[ext]' },
    define: { 'process.env.NODE_ENV': '"production"' },
    plugins: [skipVendorCss, postcss, externalFonts]
})

if (!result.success) {
    for (const log of result.logs) console.error(log)
    process.exit(1)
}

// Modern targets: logical properties and :is() stay as they are.
const version = (major: number, minor = 0) => (major << 16) | (minor << 8)
const vendor = await bundleAsync({
    filename: vendorCss,
    minify: true,
    targets: { chrome: version(100), edge: version(100), firefox: version(100), safari: version(15, 4), ios_saf: version(15, 4) },
    resolver: { resolve: (specifier, from) => Bun.resolveSync(specifier, dirname(from)) }
})
const vendorName = `assets/vendor-${Bun.hash(vendor.code).toString(36).slice(0, 8)}.css`
await Bun.write(`${outdir}/${vendorName}`, vendor.code)

// publicPath './' makes a lazy chunk's import() relative to the page, but import()
// resolves against the importing file, which already sits in assets/.
for (const out of result.outputs) {
    if (!out.path.endsWith('.js')) continue
    const code = await Bun.file(out.path).text()
    const fixed = code.replaceAll('import("./assets/', 'import("./')
    if (fixed !== code) await Bun.write(out.path, fixed)
}

// Before the app stylesheet, so our overrides still win the cascade.
let html = await Bun.file(`${outdir}/index.html`).text()
const link = `<link rel="stylesheet" crossorigin href="./${vendorName}">`
html = html.includes('<link rel="stylesheet"') ? html.replace('<link rel="stylesheet"', `${link}<link rel="stylesheet"`) : html.replace('</head>', `${link}</head>`)

await Bun.write(`${outdir}/index.html`, html)

await cp(`${root}public`, outdir, { recursive: true })
// the PDF viewer's worker is loaded by URL, not bundled
await cp(`${root}node_modules/pdfjs-dist/build/pdf.worker.min.mjs`, `${outdir}/pdf.worker.min.mjs`)
// tracked in git, so `go build` works (with an empty SPA) before a web build
await Bun.write(`${outdir}/.gitkeep`, '')

for (const [ref, src] of fonts) {
    const dest = resolve(outdir, 'assets', ref)
    await mkdir(dirname(dest), { recursive: true })
    await cp(src, dest)
}
console.log(`assets/files/  ${fonts.size} fonts (${[...new Set([...fonts.keys()].map((f) => basename(f).split('-')[0]))].join(', ')})`)

const sizes = [...result.outputs.map((o) => [o.path.replace(root, ''), o.size] as const), [`dist/${vendorName}`, vendor.code.length] as const]
for (const [path, size] of sizes) console.log(`${path}  ${(size / 1024).toFixed(1)} kB`)
