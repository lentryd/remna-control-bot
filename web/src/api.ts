import { useQuery } from '@tanstack/react-query'

import { tg } from './telegram'
import type { Order, OrderQuote, OrderRequest, Profile, Tariff } from './types'

export class ApiError extends Error {
    constructor(public status: number, public code: string) {
        super(code)
    }
}

// Relative to the page, so it works under any WEBAPP_PATH.
async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Authorization: `tma ${tg?.initData ?? ''}` }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    const res = await fetch(`./api/${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
    if (!res.ok) {
        const b = await res.json().catch(() => ({}))
        throw new ApiError(res.status, b.code ?? 'error')
    }
    if (res.status === 204) return undefined as T
    return res.json()
}

const get = <T,>(path: string) => call<T>('GET', path)

const retry = (n: number, e: unknown) => !(e instanceof ApiError && e.status < 500) && n < 2

export const useProfile = () => useQuery({ queryKey: ['me'], queryFn: () => get<Profile>('me'), retry })
export const useTariffs = () => useQuery({ queryKey: ['tariffs'], queryFn: () => get<Tariff[]>('tariffs'), retry })
export const useOrders = () => useQuery({ queryKey: ['orders'], queryFn: () => get<{ orders: Order[]; total: number }>('orders'), retry })
export const useOrderQuote = (r: OrderRequest | null) =>
    useQuery({ queryKey: ['quote', r], queryFn: () => call<OrderQuote>('POST', 'orders/quote', r), enabled: !!r, retry })
export const createOrder = (r: OrderRequest) => call<{ order: Order; details: string }>('POST', 'orders', r)
export const requestReceipt = (id: string) => call<void>('POST', `orders/${id}/receipt`)
export const payFromBalance = (id: string) => call<Order>('POST', `orders/${id}/pay-balance`)
export const cancelOrder = (id: string) => call<void>('POST', `orders/${id}/cancel`)
