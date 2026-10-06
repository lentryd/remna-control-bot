import { useQuery } from '@tanstack/react-query'

import { tg } from './telegram'
import type { Profile, Tariff } from './types'

export class ApiError extends Error {
    constructor(public status: number, public code: string) {
        super(code)
    }
}

// Relative to the page, so it works under any WEBAPP_PATH.
async function get<T>(path: string): Promise<T> {
    const res = await fetch(`./api/${path}`, { headers: { Authorization: `tma ${tg?.initData ?? ''}` } })
    if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new ApiError(res.status, body.code ?? 'error')
    }
    return res.json()
}

const retry = (n: number, e: unknown) => !(e instanceof ApiError && e.status < 500) && n < 2

export const useProfile = () => useQuery({ queryKey: ['me'], queryFn: () => get<Profile>('me'), retry })
export const useTariffs = () => useQuery({ queryKey: ['tariffs'], queryFn: () => get<Tariff[]>('tariffs'), retry })
