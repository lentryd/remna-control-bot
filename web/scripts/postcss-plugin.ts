// Runs our own CSS (not node_modules) through postcss.config.cjs, so Mantine's
// light-dark(), @mixin hover/light/dark, $mantine-breakpoint-* and autoRem work
// under Bun's bundler. Used by both the dev server (bunfig.toml) and build.ts.
import type { BunPlugin } from 'bun'
import postcss from 'postcss'

import config from '../postcss.config.cjs'

const processor = postcss(Object.entries(config.plugins).map(([name, opts]) => require(name)(opts)))

const plugin: BunPlugin = {
    name: 'postcss',
    setup(build) {
        build.onLoad({ filter: /[\\/]src[\\/].*\.css$/ }, async ({ path }) => {
            const result = await processor.process(await Bun.file(path).text(), { from: path })
            return { contents: result.css, loader: 'css' }
        })
    }
}

export default plugin
