// Checkout.tsx — one page for every paid action: renewals, new subscriptions,
// add-ons, tariff changes and top-ups. Prices come from the server (quote).
import { Alert, Button, Card, CopyButton, Group, NumberInput, Select, SimpleGrid, Skeleton, Stack, Text, ThemeIcon, Title, UnstyledButton } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { IconAlertTriangle, IconBuildingBank, IconCheck, IconClockHour4, IconCopy, IconCreditCard, IconInfoCircle, IconPaperclip, IconReceipt, IconWallet } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router'

import { currencySymbol, fmtMoney } from '@shared/format'
import { ApiError, cancelOrder, createOrder, payFromBalance, requestReceipt, useOrderQuote, useTariffs } from '../api'
import { haptic, openExternal, tg } from '../telegram'
import type { Order, OrderKind, OrderRequest, Period, Profile } from '../types'
import { CtaButton } from '../ui/CtaButton'
import { ErrorScreen } from '../ui/ErrorScreen'
import { term } from '../term'
import classes from './checkout.module.css'

// saving of a period against paying monthly, in whole percent
const saving = (monthly: number, p: Period) => {
    const full = monthly * (p.months + p.days / 30)
    return full > 0 ? Math.round((1 - p.price / full) * 100) : 0
}

const BASE_MONTHS = [1, 3, 6, 12]
const KINDS: OrderKind[] = ['renew', 'subscribe', 'addon', 'change_tariff', 'topup']
const num = (v: string | null) => (v && Number(v) > 0 ? Number(v) : undefined)

function Row({ label, value, color }: { label: string; value: number; color?: string }) {
    return (
        <Group justify="space-between" wrap="nowrap">
            <Text c="dimmed" size="sm">{label}</Text>
            <Text c={color} fw={600} size="sm">{fmtMoney(value, 2)}</Text>
        </Group>
    )
}

function Header({ subtitle }: { subtitle?: string }) {
    const { t } = useTranslation()
    return (
        <Stack gap={4}>
            <Title order={3}>{t('checkout.title')}</Title>
            {subtitle && <Text c="dimmed" size="sm">{subtitle}</Text>}
        </Stack>
    )
}

// Summary — what is being bought and the total, in the tariff-card style.
function Summary({ title, toPay, children }: { title: string; toPay: number; children?: ReactNode }) {
    const { t } = useTranslation()
    return (
        <Card className={classes.summary} padding="lg" radius="lg" withBorder>
            <Stack gap="md">
                <Group align="flex-start" justify="space-between" wrap="nowrap">
                    <Stack gap={2} style={{ minWidth: 0 }}>
                        <Text c="dimmed" size="sm">{t('checkout.to_pay')}</Text>
                        <Text className={classes.total}>{fmtMoney(toPay, 2)}</Text>
                    </Stack>
                    <ThemeIcon radius="xl" size={52} variant="light">
                        <IconReceipt size={28} />
                    </ThemeIcon>
                </Group>
                <Text fw={600}>{title}</Text>
                {children}
            </Stack>
        </Card>
    )
}

function Note({ color, icon, children }: { color: string; icon: ReactNode; children: ReactNode }) {
    return (
        <Alert color={color} icon={icon} radius="lg" variant="light">
            {children}
        </Alert>
    )
}

export function Checkout({ p }: { p: Profile }) {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const qc = useQueryClient()
    const [params] = useSearchParams()
    const kindParam = params.get('kind') as OrderKind
    const kind: OrderKind = KINDS.includes(kindParam) ? kindParam : 'topup'
    const itemKind = params.get('item_kind') === 'addon' ? 'addon' : 'subscription'
    const itemId = num(params.get('item_id'))
    const tariffs = useTariffs()

    const [subId, setSubId] = useState<number | undefined>(itemId ?? (kind === 'addon' && p.subscriptions.length === 1 ? p.subscriptions[0].id : undefined))
    const [tariffId, setTariffId] = useState<number | undefined>(num(params.get('tariff_id')))
    const [termKey, setTermKey] = useState<string | null>(null)
    const [amount, setAmount] = useState<number | string>(500)
    const [debouncedAmount] = useDebouncedValue(amount, 300) // the quote follows the amount once typing pauses
    const settling = kind === 'topup' && debouncedAmount !== amount
    const [busy, setBusy] = useState(false)
    const [failed, setFailed] = useState<string | null>(null)
    const [pendingId, setPendingId] = useState<string | null>(null) // created, balance payment not done yet
    const [down, setDown] = useState(false)
    const [manual, setManual] = useState<{ order: Order; details: string } | null>(null)

    // the tariff whose periods define the term options
    const sub = p.subscriptions.find((s) => s.id === itemId)
    const tariff = tariffs.data?.find((tf) => (kind === 'renew' ? tf.kind === 'base' && (sub?.tariff_id != null ? tf.id === sub.tariff_id : tf.name === sub?.tariff) : tf.id === tariffId))
    const periods: Period[] = [
        ...BASE_MONTHS.map((months) => ({ months, days: 0, price: 0 })),
        ...(tariff?.periods ?? []).filter((x) => x.months > 0 || x.days > 0),
    ]
    const terms = [...new Map(periods.map((x) => [`${x.months}:${x.days}`, x])).values()].sort((a, b) => a.months * 30 + a.days - (b.months * 30 + b.days))
    const firstPeriod = tariff?.periods?.find((x) => x.months > 0 || x.days > 0)
    const curTerm = termKey ?? (firstPeriod ? `${firstPeriod.months}:${firstPeriod.days}` : '1:0')
    const [months, days] = curTerm.split(':').map(Number)

    let request: OrderRequest | null = null
    switch (kind) {
        case 'renew':
            if (itemId) request = { kind, item_kind: itemKind, item_id: itemId, months, days }
            break
        case 'subscribe':
            if (tariffId) request = { kind, tariff_id: tariffId, months, days }
            break
        case 'addon':
            if (tariffId && subId) request = { kind, tariff_id: tariffId, item_id: subId, months, days }
            break
        case 'change_tariff':
            if (itemId && tariffId) request = { kind, item_kind: itemKind, item_id: itemId, tariff_id: tariffId }
            break
        case 'topup':
            if (Number(debouncedAmount) >= 1) request = { kind, amount: Number(debouncedAmount) }
            break
    }
    const quote = useOrderQuote(manual ? null : request)
    const qErr = quote.error instanceof ApiError ? quote.error : null

    if (down) return <ErrorScreen code="unavailable" onRetry={() => setDown(false)} />
    if (qErr && qErr.status >= 500) return <ErrorScreen code="unavailable" onRetry={() => quote.refetch()} />

    const run = async (fn: () => Promise<void>) => {
        setBusy(true)
        setFailed(null)
        try {
            await fn()
        } catch (e) {
            if (!(e instanceof ApiError) || e.status >= 500) {
                setDown(true)
                return
            }
            setFailed(e.code === 'manual_off' ? t('checkout.manual_off') : e.code === 'provider_failed' ? t('checkout.provider_failed') : t('checkout.unavailable'))
        } finally {
            setBusy(false)
        }
    }
    const payBalance = () =>
        run(async () => {
            let id = pendingId
            if (id === null) {
                id = (await createOrder(request!)).order.id
                setPendingId(id)
            }
            const done = await payFromBalance(id)
            setPendingId(null)
            qc.invalidateQueries({ queryKey: ['me'] })
            qc.invalidateQueries({ queryKey: ['orders'] })
            if (done.status === 'failed') {
                setFailed(t('checkout.failed'))
                return
            }
            haptic()
            navigate('/balance')
        })
    // giving up on a created-but-unpaid balance order cancels it
    const abandon = () =>
        run(async () => {
            if (pendingId !== null) await cancelOrder(pendingId).catch(() => undefined)
            setPendingId(null)
            qc.invalidateQueries({ queryKey: ['orders'] })
            navigate(-1)
        })
    const payManual = () =>
        run(async () => {
            setManual(await createOrder(request!))
            qc.invalidateQueries({ queryKey: ['orders'] })
        })
    // An online provider: the order is created together with its invoice, then the customer pays on the provider's page
    // (Stars: Telegram's own invoice sheet) and finds the result in the history.
    const payProvider = (method: string) =>
        run(async () => {
            const { order } = await createOrder({ ...request!, method })
            const url = order.pay_url!
            const refresh = () => {
                qc.invalidateQueries({ queryKey: ['me'] })
                qc.invalidateQueries({ queryKey: ['orders'] })
            }
            refresh()
            if (method === 'stars' && tg?.openInvoice) {
                tg.openInvoice(url, (status) => {
                    if (status === 'paid') {
                        haptic()
                        refresh()
                        navigate('/balance')
                    }
                })
                return
            }
            openExternal(url)
            navigate('/balance')
        })
    const sendReceipt = () =>
        run(async () => {
            await requestReceipt(manual!.order.id)
            tg?.close()
        })

    const failNote = failed && (
        <Note color="red" icon={<IconAlertTriangle size={18} />}>
            {failed}
        </Note>
    )

    if (manual)
        return (
            <Stack gap="lg">
                <Header subtitle={t('checkout.manual_title')} />
                <Summary title={manual.order.title} toPay={manual.order.to_pay} />
                <Card padding="lg" radius="lg" style={{ boxShadow: 'none' }} withBorder>
                    <Stack gap="sm">
                        <Group justify="space-between" wrap="nowrap">
                            <Group gap="sm" wrap="nowrap">
                                <ThemeIcon radius="xl" size={36} variant="light">
                                    <IconBuildingBank size={20} />
                                </ThemeIcon>
                                <Text fw={600}>{t('checkout.details')}</Text>
                            </Group>
                            <CopyButton timeout={2000} value={manual.details}>
                                {({ copied, copy }) => (
                                    <Button
                                        color={copied ? 'teal' : undefined}
                                        leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                                        onClick={() => {
                                            copy()
                                            haptic()
                                        }}
                                        radius="md"
                                        size="xs"
                                        variant="light"
                                    >
                                        {copied ? t('copied') : t('refs.copy')}
                                    </Button>
                                )}
                            </CopyButton>
                        </Group>
                        <Text className={classes.details} size="sm">{manual.details}</Text>
                    </Stack>
                </Card>
                <Group align="flex-start" gap="sm" px={4} wrap="nowrap">
                    <IconInfoCircle color="var(--mantine-color-dimmed)" size={18} style={{ flexShrink: 0, marginTop: 2 }} />
                    <Text c="dimmed" size="sm">{t('checkout.receipt_hint')}</Text>
                </Group>
                {failNote}
                <CtaButton fullWidth leftSection={<IconPaperclip size={20} />} loading={busy} onClick={sendReceipt} size="lg">{t('checkout.send_receipt')}</CtaButton>
            </Stack>
        )

    const q = quote.data
    const locked = pendingId !== null
    const baseTariffs = (tariffs.data ?? []).filter((tf) => tf.kind === 'base')
    const label = (text: string) => <Text fw={600} size="sm">{text}</Text>
    return (
        <Stack gap="lg">
            <Header subtitle={t('checkout.subtitle')} />
            {kind === 'addon' && !itemId && (
                <Select
                    data={p.subscriptions.map((s) => ({ value: String(s.id), label: s.title }))}
                    disabled={locked}
                    label={label(t('checkout.subscription'))}
                    onChange={(v) => setSubId(num(v))}
                    radius="md"
                    size="md"
                    value={subId ? String(subId) : null}
                />
            )}
            {kind === 'change_tariff' && (
                <Select
                    data={baseTariffs.filter((tf) => (sub?.tariff_id != null ? tf.id !== sub.tariff_id : tf.name !== sub?.tariff)).map((tf) => ({ value: String(tf.id), label: tf.name }))}
                    disabled={locked}
                    label={label(t('checkout.tariff'))}
                    onChange={(v) => setTariffId(num(v))}
                    radius="md"
                    size="md"
                    value={tariffId ? String(tariffId) : null}
                />
            )}
            {(kind === 'renew' || kind === 'subscribe' || kind === 'addon') && (
                <Stack gap={6}>
                    {label(t('checkout.term'))}
                    <SimpleGrid cols={2} spacing="sm">
                        {terms.map((x) => {
                            const key = `${x.months}:${x.days}`
                            const off = tariff && x.price > 0 ? saving(tariff.monthly_price, x) : 0
                            return (
                                <UnstyledButton
                                    className={classes.term}
                                    data-active={key === curTerm || undefined}
                                    disabled={locked}
                                    key={key}
                                    onClick={() => setTermKey(key)}
                                >
                                    <Text fw={600}>{term(x, t)}</Text>
                                    <Group gap={6} wrap="nowrap">
                                        {x.price > 0 && <Text c="dimmed" size="sm">{fmtMoney(x.price)}</Text>}
                                        {off > 0 && <Text c="teal" fw={600} size="sm">−{off}%</Text>}
                                    </Group>
                                </UnstyledButton>
                            )
                        })}
                    </SimpleGrid>
                </Stack>
            )}
            {kind === 'topup' && (
                <NumberInput disabled={locked} label={label(t('checkout.amount'))} min={1} onChange={setAmount} radius="md" size="md" suffix={` ${currencySymbol()}`} value={amount} />
            )}
            {qErr && qErr.status < 500 && (
                <Note color="gray" icon={<IconInfoCircle size={18} />}>
                    {qErr.code === 'manual_off' ? t('checkout.manual_off') : t('checkout.unavailable')}
                </Note>
            )}
            {!q && request && quote.isPending && <Skeleton height={190} radius="lg" />}
            {q && (
                <>
                    <Summary title={q.title} toPay={q.to_pay}>
                        <Stack className={classes.lines} gap={6}>
                            <Row label={t('checkout.price')} value={q.price} />
                            {q.from_balance > 0 && <Row color="teal" label={t('checkout.from_balance')} value={-q.from_balance} />}
                        </Stack>
                    </Summary>
                    {locked && (
                        <Note color="yellow" icon={<IconClockHour4 size={18} />}>
                            {t('checkout.pending')}
                        </Note>
                    )}
                    {failNote}
                    {q.methods.length === 0 && (
                        <Note color="gray" icon={<IconInfoCircle size={18} />}>
                            {t('checkout.manual_off')}
                        </Note>
                    )}
                    {q.methods.length > 0 && (
                        <Stack gap="sm">
                            {label(t('checkout.method'))}
                            {q.methods.includes('balance') && (
                                <CtaButton fullWidth leftSection={<IconWallet size={20} />} disabled={settling} loading={busy} onClick={payBalance} size="lg">
                                    {t('checkout.pay_balance')}
                                </CtaButton>
                            )}
                            {q.methods.includes('manual') && !locked && (
                                <Button
                                    disabled={settling || (busy && q.methods.includes('balance'))}
                                    loading={busy && !q.methods.includes('balance')}
                                    fullWidth
                                    leftSection={<IconBuildingBank size={20} />}
                                    onClick={payManual}
                                    radius="xl"
                                    size="lg"
                                    variant={q.methods.includes('balance') ? 'light' : 'filled'}
                                >
                                    {t('checkout.pay_manual')}
                                </Button>
                            )}
                            {q.methods
                                .filter((m) => m !== 'balance' && m !== 'manual')
                                .map((m) => (
                                    <Button
                                        disabled={settling || locked}
                                        fullWidth
                                        key={m}
                                        leftSection={<IconCreditCard size={20} />}
                                        loading={busy}
                                        onClick={() => payProvider(m)}
                                        radius="xl"
                                        size="lg"
                                        variant="light"
                                    >
                                        {q.titles?.[m] ?? m}
                                    </Button>
                                ))}
                            {locked && (
                                <Button color="gray" disabled={busy} fullWidth onClick={abandon} radius="xl" variant="subtle">
                                    {t('orders.cancel')}
                                </Button>
                            )}
                        </Stack>
                    )}
                </>
            )}
        </Stack>
    )
}
