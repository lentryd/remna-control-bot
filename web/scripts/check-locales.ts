import { readdirSync, readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'

const root = join(import.meta.dir, '..', 'public', 'locales')
const flat = (o: unknown, p = ''): Record<string, string> =>
    Object.entries(o as Record<string, unknown>).reduce((acc, [k, v]) => {
        const key = p ? `${p}.${k}` : k
        return typeof v === 'object' && v !== null ? { ...acc, ...flat(v, key) } : { ...acc, [key]: String(v) }
    }, {} as Record<string, string>)
const vars = (s: string) => (s.match(/{{\s*\w+\s*}}/g) ?? []).map(x => x.replace(/\s/g, '')).sort().join(',')
const plural = /_(zero|one|two|few|many|other)$/
const base = (k: string) => k.replace(plural, '')

let bad = 0
const langs = readdirSync(root)
const namespaces = new Set(langs.flatMap(l => readdirSync(join(root, l))))
for (const ns of namespaces) {
    const enFile = join(root, 'en', ns)
    if (!existsSync(enFile)) { console.error(`en/${ns} is missing`); bad++; continue }
    const en = flat(JSON.parse(readFileSync(enFile, 'utf8')))
    const enBase = new Map(Object.entries(en).map(([k, v]) => [base(k), vars(v)]))
    for (const lang of langs.filter(l => l !== 'en')) {
        const file = join(root, lang, ns)
        if (!existsSync(file)) { console.error(`${lang}/${ns} is missing`); bad++; continue }
        const other = flat(JSON.parse(readFileSync(file, 'utf8')))
        const otherBase = new Map(Object.entries(other).map(([k, v]) => [base(k), vars(v)]))
        for (const [k, v] of enBase) {
            if (!otherBase.has(k)) { console.error(`${lang}/${ns}: missing ${k}`); bad++ }
            else if (otherBase.get(k) !== v) { console.error(`${lang}/${ns}: ${k} placeholders differ`); bad++ }
        }
        for (const k of otherBase.keys()) if (!enBase.has(k)) { console.error(`${lang}/${ns}: extra ${k}`); bad++ }
    }
}
process.exit(bad ? 1 : 0)
