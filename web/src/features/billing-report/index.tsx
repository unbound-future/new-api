/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  CalendarSync,
  ChevronLeft,
  ChevronRight,
  Download,
  Filter,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
import { SectionPageLayout } from '@/components/layout'
import { getGroups } from '@/features/users/api'
import {
  exportBillingReport,
  getBillingReport,
  getBillingReportStatus,
  rebuildBillingReport,
  updateBillingReportAuto,
} from './api'
import { BillingGroupCombobox } from './components/billing-group-combobox'
import { BillingReportTable } from './components/billing-report-table'
import type {
  BillingReportFilters,
  BillingReportStatus,
  BillingReportTotals,
  DecimalValue,
} from './types'

const pageSize = 20
const countFormatter = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 0,
})

function localDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function createDefaultFilters(): BillingReportFilters {
  const now = new Date()
  const firstDay = new Date(now.getFullYear(), now.getMonth(), 1)
  return {
    start_date: localDate(firstDay),
    end_date: localDate(now),
    username: '',
    user_group: '',
    third_party_group: '',
    channel_tag: '',
    channel_name: '',
    upstream_url: '',
    model_name: '',
    token_name: '',
  }
}

function number(value: DecimalValue | undefined): number {
  const parsed = Number(value ?? 0)
  return Number.isFinite(parsed) ? parsed : 0
}

function money(value: DecimalValue | undefined): string {
  return number(value).toLocaleString(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  })
}

function timeLabel(timestamp: number | undefined): string {
  if (!timestamp) return '—'
  return new Date(timestamp * 1000).toLocaleString()
}

function statusTone(status?: string) {
  if (status === 'error') return 'destructive' as const
  return 'secondary' as const
}

function statusLabel(status?: string) {
  if (status === 'idle') return 'Idle'
  if (status === 'syncing') return 'Syncing'
  if (status === 'rebuilding') return 'Rebuilding'
  if (status === 'error') return 'Error'
  return status || ''
}

function BillingStatus({
  status,
  isFetching,
  onRefresh,
}: {
  status?: BillingReportStatus
  isFetching: boolean
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  const activeJob = status?.active_job
  const progress =
    activeJob && activeJob.total_days > 0
      ? Math.min(100, (activeJob.processed_days / activeJob.total_days) * 100)
      : 0

  return (
    <Card className='border-primary/15 from-primary/[0.06] via-card to-card bg-gradient-to-br'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <span className='bg-primary size-2 rounded-full' />
          {t('Aggregation status')}
        </CardTitle>
        <CardDescription>
          {t(
            'Billing data is aggregated asynchronously and never blocks API requests'
          )}
        </CardDescription>
        <CardAction>
          <Button
            type='button'
            size='sm'
            variant='ghost'
            disabled={isFetching}
            onClick={onRefresh}
          >
            <RefreshCw className={isFetching ? 'animate-spin' : ''} />
            {t('Refresh')}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <div>
            <div className='text-muted-foreground text-xs'>{t('State')}</div>
            <div className='mt-1'>
              <Badge variant={statusTone(status?.state?.status)}>
                {status?.state?.status
                  ? t(statusLabel(status.state.status))
                  : '—'}
              </Badge>
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Last synced')}
            </div>
            <div className='mt-1 font-mono text-xs'>
              {timeLabel(status?.state?.last_synced_at)}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Processed logs')}
            </div>
            <div className='mt-1 font-mono font-medium tabular-nums'>
              {countFormatter.format(status?.state?.processed_logs || 0)}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Pending rebuilds')}
            </div>
            <div className='mt-1 font-mono font-medium tabular-nums'>
              {countFormatter.format(status?.pending_jobs || 0)}
            </div>
          </div>
        </div>

        {activeJob && (
          <div className='bg-background/70 space-y-2 rounded-lg border p-3'>
            <div className='flex flex-wrap items-center justify-between gap-2 text-xs'>
              <span className='font-medium'>
                {t('Rebuilding')} {activeJob.start_date} → {activeJob.end_date}
              </span>
              <span className='text-muted-foreground font-mono'>
                {activeJob.processed_days}/{activeJob.total_days} {t('days')}
              </span>
            </div>
            <Progress value={progress} />
            <div className='text-muted-foreground text-xs'>
              {t('Current date')}: {activeJob.current_date || '—'} ·{' '}
              {countFormatter.format(activeJob.processed_logs)} {t('logs')}
            </div>
          </div>
        )}

        {status?.state?.last_error && (
          <Alert variant='destructive'>
            <AlertCircle />
            <AlertTitle>{t('Last aggregation error')}</AlertTitle>
            <AlertDescription className='font-mono text-xs break-all'>
              {status.state.last_error}
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}

function SummaryCards({ totals }: { totals?: BillingReportTotals }) {
  const { t } = useTranslation()
  const totalTokens =
    (totals?.input_tokens || 0) +
    (totals?.output_tokens || 0) +
    (totals?.cache_read_tokens || 0) +
    (totals?.cache_write_tokens || 0)
  const cards = [
    {
      label: t('Adjusted total'),
      value: money(totals?.adjusted_total),
      note: t('Actual charged amount'),
    },
    {
      label: t('Original total'),
      value: money(totals?.original_total),
      note: t('Before user/group ratio'),
    },
    {
      label: t('Calls'),
      value: countFormatter.format(totals?.call_count || 0),
      note: t('Matched calls in current filters'),
    },
    {
      label: t('Billing token total'),
      value: countFormatter.format(totalTokens),
      note: t('Input, output and cache tokens'),
    },
  ]
  return (
    <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
      {cards.map((card, index) => (
        <Card key={card.label} className='relative'>
          <div
            className={`absolute inset-y-4 left-0 w-0.5 rounded-r ${
              index === 0 ? 'bg-primary' : 'bg-border'
            }`}
          />
          <CardHeader>
            <CardDescription>{card.label}</CardDescription>
            <CardTitle className='font-mono text-xl font-semibold tracking-tight tabular-nums'>
              {card.value}
            </CardTitle>
          </CardHeader>
          <CardContent className='text-muted-foreground text-xs'>
            {card.note}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

export function BillingReport() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initialFilters = useMemo(createDefaultFilters, [])
  const [draftFilters, setDraftFilters] =
    useState<BillingReportFilters>(initialFilters)
  const [filters, setFilters] = useState<BillingReportFilters>(initialFilters)
  const [page, setPage] = useState(1)
  const [pageInput, setPageInput] = useState('1')
  const [exporting, setExporting] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['billing-report-status'],
    queryFn: getBillingReportStatus,
    refetchInterval: 5000,
  })
  const enabled = statusQuery.data?.enabled === true
  const groupsQuery = useQuery({
    queryKey: ['billing-report-groups'],
    queryFn: getGroups,
    enabled,
    staleTime: 5 * 60 * 1000,
  })
  const reportQuery = useQuery({
    queryKey: ['billing-report', filters, page, pageSize],
    queryFn: () => getBillingReport(filters, page, pageSize),
    enabled,
    placeholderData: (previous) => previous,
  })

  const autoMutation = useMutation({
    mutationFn: updateBillingReportAuto,
    onSuccess: async () => {
      toast.success(t('Automatic billing update changed'))
      await queryClient.invalidateQueries({
        queryKey: ['billing-report-status'],
      })
    },
  })
  const rebuildMutation = useMutation({
    mutationFn: () =>
      rebuildBillingReport(filters.start_date, filters.end_date),
    onSuccess: async () => {
      toast.success(t('Billing rebuild queued'))
      await queryClient.invalidateQueries({
        queryKey: ['billing-report-status'],
      })
    },
  })

  const updateDraft = (key: keyof BillingReportFilters, value: string) => {
    setDraftFilters((current) => ({ ...current, [key]: value }))
  }
  const applyFilters = () => {
    setFilters(draftFilters)
    setPage(1)
  }
  const resetFilters = () => {
    const next = createDefaultFilters()
    setDraftFilters(next)
    setFilters(next)
    setPage(1)
  }
  const handleRebuild = () => {
    if (!filters.start_date || !filters.end_date) {
      toast.error(t('Select a start and end date first'))
      return
    }
    if (
      !window.confirm(
        t(
          'Rebuild the selected range? Existing daily aggregates are replaced atomically after each day finishes.'
        )
      )
    ) {
      return
    }
    rebuildMutation.mutate()
  }
  const handleExport = async () => {
    setExporting(true)
    try {
      await exportBillingReport(filters)
      toast.success(t('Billing workbook downloaded'))
    } finally {
      setExporting(false)
    }
  }

  const totalPages = Math.max(
    1,
    Math.ceil((reportQuery.data?.total || 0) / pageSize)
  )
  useEffect(() => {
    setPageInput(String(page))
  }, [page])

  const jumpToPage = () => {
    const requestedPage = Number.parseInt(pageInput, 10)
    if (!Number.isFinite(requestedPage)) {
      setPageInput(String(page))
      return
    }
    const targetPage = Math.min(totalPages, Math.max(1, requestedPage))
    setPage(targetPage)
    setPageInput(String(targetPage))
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Billing Report')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          variant='outline'
          disabled={!enabled || exporting}
          onClick={handleExport}
        >
          {exporting ? <LoaderCircle className='animate-spin' /> : <Download />}
          {t('Export Excel')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto max-w-[1800px] space-y-4'>
          {statusQuery.data && !statusQuery.data.enabled && (
            <Alert>
              <AlertCircle />
              <AlertTitle>{t('Billing report is disabled')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Set BILLING_REPORT_ENABLED=true and restart the application to initialize this isolated module.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {enabled && (
            <>
              <div className='grid gap-4 2xl:grid-cols-[minmax(0,1fr)_420px]'>
                <Card>
                  <CardHeader>
                    <CardTitle className='flex items-center gap-2'>
                      <Filter className='size-4' />
                      {t('Billing filters')}
                    </CardTitle>
                    <CardDescription>
                      {t('All totals and exports follow the current filters')}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='space-y-4'>
                    <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
                      <div className='space-y-1.5'>
                        <Label htmlFor='billing-start'>{t('Start date')}</Label>
                        <Input
                          id='billing-start'
                          type='date'
                          value={draftFilters.start_date}
                          onChange={(event) =>
                            updateDraft('start_date', event.target.value)
                          }
                        />
                      </div>
                      <div className='space-y-1.5'>
                        <Label htmlFor='billing-end'>{t('End date')}</Label>
                        <Input
                          id='billing-end'
                          type='date'
                          value={draftFilters.end_date}
                          onChange={(event) =>
                            updateDraft('end_date', event.target.value)
                          }
                        />
                      </div>
                      {(
                        [
                          ['username', 'Customer'],
                          ['model_name', 'Model'],
                          ['channel_name', 'Channel name'],
                          ['channel_tag', 'Channel tag'],
                          ['token_name', 'Billing token name'],
                          ['upstream_url', 'Upstream URL'],
                        ] as const
                      ).map(([key, label]) => (
                        <div key={key} className='space-y-1.5'>
                          <Label htmlFor={`billing-${key}`}>{t(label)}</Label>
                          <Input
                            id={`billing-${key}`}
                            value={draftFilters[key]}
                            placeholder={t('Contains...')}
                            onChange={(event) =>
                              updateDraft(key, event.target.value)
                            }
                            onKeyDown={(event) => {
                              if (event.key === 'Enter') applyFilters()
                            }}
                          />
                        </div>
                      ))}
                      <div className='space-y-1.5'>
                        <Label htmlFor='billing-third_party_group'>
                          {t('Billing group')}
                        </Label>
                        <BillingGroupCombobox
                          id='billing-third_party_group'
                          groups={groupsQuery.data?.data || []}
                          value={draftFilters.third_party_group}
                          disabled={groupsQuery.isFetching}
                          onValueChange={(value) =>
                            updateDraft('third_party_group', value)
                          }
                        />
                      </div>
                    </div>
                    <div className='flex flex-wrap items-center gap-2 border-t pt-4'>
                      <Button type='button' onClick={applyFilters}>
                        <Filter />
                        {t('Apply filters')}
                      </Button>
                      <Button
                        type='button'
                        variant='ghost'
                        onClick={resetFilters}
                      >
                        <RotateCcw />
                        {t('Reset')}
                      </Button>
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle>{t('Update control')}</CardTitle>
                    <CardDescription>
                      {t('Automatic updates run every five minutes')}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='space-y-5'>
                    <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                      <div>
                        <Label htmlFor='billing-auto'>
                          {t('Automatic update')}
                        </Label>
                        <p className='text-muted-foreground mt-1 text-xs'>
                          {t('Pause only the aggregator, never API traffic')}
                        </p>
                      </div>
                      <Switch
                        id='billing-auto'
                        checked={statusQuery.data?.state?.auto_enabled || false}
                        disabled={autoMutation.isPending}
                        onCheckedChange={(checked) =>
                          autoMutation.mutate(checked)
                        }
                      />
                    </div>
                    <div className='space-y-2 rounded-lg border p-3'>
                      <div className='flex items-center gap-2 text-sm font-medium'>
                        <CalendarSync className='size-4' />
                        {t('Manual range rebuild')}
                      </div>
                      <p className='text-muted-foreground text-xs'>
                        {filters.start_date || '—'} → {filters.end_date || '—'}
                      </p>
                      <Button
                        type='button'
                        variant='outline'
                        className='w-full'
                        disabled={
                          rebuildMutation.isPending ||
                          Boolean(statusQuery.data?.active_job)
                        }
                        onClick={handleRebuild}
                      >
                        {rebuildMutation.isPending ? (
                          <LoaderCircle className='animate-spin' />
                        ) : (
                          <CalendarSync />
                        )}
                        {t('Rebuild selected range')}
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              </div>

              <BillingStatus
                status={statusQuery.data}
                isFetching={statusQuery.isFetching}
                onRefresh={() => void statusQuery.refetch()}
              />
              <SummaryCards totals={reportQuery.data?.totals} />

              <Card>
                <CardHeader>
                  <CardTitle>{t('Usage details')}</CardTitle>
                  <CardDescription>
                    {countFormatter.format(reportQuery.data?.total || 0)}{' '}
                    {t('daily billing buckets')}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-4'>
                  <BillingReportTable
                    rows={reportQuery.data?.items || []}
                    loading={reportQuery.isFetching}
                  />
                  <div className='flex flex-wrap items-center justify-between gap-3'>
                    <div className='text-muted-foreground text-xs'>
                      {t('Page')} {page} / {totalPages}
                    </div>
                    <div className='flex flex-wrap items-center justify-end gap-2'>
                      <Button
                        type='button'
                        size='sm'
                        variant='outline'
                        disabled={page <= 1 || reportQuery.isFetching}
                        onClick={() => setPage((current) => current - 1)}
                      >
                        <ChevronLeft />
                        {t('Billing previous page')}
                      </Button>
                      <Button
                        type='button'
                        size='sm'
                        variant='outline'
                        disabled={page >= totalPages || reportQuery.isFetching}
                        onClick={() => setPage((current) => current + 1)}
                      >
                        {t('Billing next page')}
                        <ChevronRight />
                      </Button>
                      <div className='ml-1 flex items-center gap-1.5'>
                        <Label
                          htmlFor='billing-page-jump'
                          className='text-muted-foreground text-xs'
                        >
                          {t('Billing jump to page')}
                        </Label>
                        <Input
                          id='billing-page-jump'
                          type='number'
                          min={1}
                          max={totalPages}
                          inputMode='numeric'
                          className='h-8 w-20 font-mono tabular-nums'
                          value={pageInput}
                          onChange={(event) => setPageInput(event.target.value)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter') jumpToPage()
                          }}
                        />
                        <Button
                          type='button'
                          size='sm'
                          variant='secondary'
                          disabled={reportQuery.isFetching}
                          onClick={jumpToPage}
                        >
                          {t('Billing jump')}
                        </Button>
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
