// Ambient types for what Bun's bundler handles in the browser build.
declare module '*.module.css' {
    const classes: Readonly<Record<string, string>>
    export default classes
}
declare module '*.css'

// replaced at bundle time ("production" in build.ts, "development" in the dev server)
declare const process: { env: { NODE_ENV?: string } }
