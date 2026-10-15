import { useQuery } from '@tanstack/react-query'

import { ApiError, authHeaders, call } from './api'
import type { AdminOrder, AdminSettings, CheckResult, Decision, ProfileSaved, ProfileTexts } from './types'

export interface AdminMe {
    id: number
    name: string
    currency: string
}

const retry = (n: number, e: unknown) => !(e instanceof ApiError && e.status < 500) && n < 2

export const useAdminMe = () => useQuery({ queryKey: ['admin', 'me'], queryFn: () => call<AdminMe>('GET', 'admin/me'), retry })

export const useAdminOrders = (status: string, page: number) =>
    useQuery({
        queryKey: ['admin', 'orders', status, page],
        queryFn: () => call<{ orders: AdminOrder[]; total: number; unreachable_admins: number[] }>('GET', `admin/orders?status=${status}&page=${page}`),
        refetchInterval: 15_000,
        retry
    })

export const useAdminOrder = (id: string) =>
    useQuery({ queryKey: ['admin', 'order', id], queryFn: () => call<AdminOrder>('GET', `admin/orders/${id}`), retry })

export const approveOrder = (id: string, decision: Decision, amount?: number) =>
    call<AdminOrder>('POST', `admin/orders/${id}/approve`, { decision, amount })
export const rejectOrder = (id: string, reason: string) => call<AdminOrder>('POST', `admin/orders/${id}/reject`, { reason })
export const retryOrder = (id: string) => call<AdminOrder>('POST', `admin/orders/${id}/retry`)

export const useAdminSettings = () =>
    useQuery({ queryKey: ['admin', 'settings'], queryFn: () => call<AdminSettings>('GET', 'admin/settings'), retry })
export const saveSection = (key: string, body: Record<string, unknown>) =>
    call<Record<string, unknown>>('PUT', `admin/settings/${key}`, body)
export const saveProfile = (texts: Record<string, ProfileTexts>) => call<ProfileSaved>('PUT', 'admin/settings/profile', { texts })
export const checkSection = (key: string) => call<CheckResult>('POST', `admin/settings/${key}/test`)

// The receipt needs the Authorization header, so it is fetched and shown from a blob URL.
export async function fetchReceipt(id: string): Promise<Blob> {
    const res = await fetch(`./api/admin/orders/${id}/receipt`, { headers: authHeaders() })
    if (!res.ok) throw new ApiError(res.status, 'error')
    return res.blob()
}
