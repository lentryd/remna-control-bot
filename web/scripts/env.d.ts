declare module '*.html' {
    const html: import('bun').HTMLBundle
    export default html
}
declare module '*/postcss.config.cjs' {
    const config: { plugins: Record<string, unknown> }
    export default config
}
