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
import { Fragment, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { BillingReportRow, DecimalValue } from '../types'

const countFormatter = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 0,
})

function decimal(value: DecimalValue | undefined): number {
  const parsed = Number(value ?? 0)
  return Number.isFinite(parsed) ? parsed : 0
}

function money(value: DecimalValue | undefined): string {
  return decimal(value).toLocaleString(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  })
}

function unitPrice(
  value: DecimalValue | undefined,
  pricingBreakdownKnown: boolean
): string {
  if (!pricingBreakdownKnown) return '—'
  return `${money(value)} / M`
}

function Breakdown({
  row,
  adjusted,
}: {
  row: BillingReportRow
  adjusted: boolean
}) {
  const { t } = useTranslation()
  const prefix = adjusted ? 'adjusted' : 'original'
  const amount = (name: string) =>
    row[`${prefix}_${name}` as keyof BillingReportRow] as DecimalValue
  const unit = (name: string) =>
    row[`${prefix}_${name}_unit` as keyof BillingReportRow] as DecimalValue
  const components = [
    { key: 'input', label: t('Input') },
    { key: 'output', label: t('Output') },
    { key: 'cache_read', label: t('Cache read') },
    { key: 'cache_write', label: t('Cache write') },
  ]

  return (
    <div className='space-y-3'>
      <div className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
        {adjusted ? t('Adjusted pricing') : t('Original pricing')}
      </div>
      <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
        {components.map((component) => (
          <div
            key={component.key}
            className='bg-background/70 rounded-lg border px-3 py-2'
          >
            <div className='text-muted-foreground text-xs'>
              {component.label}
            </div>
            <div className='mt-1 font-mono text-sm font-medium tabular-nums'>
              {money(amount(component.key))}
            </div>
            <div className='text-muted-foreground mt-0.5 font-mono text-xs tabular-nums'>
              {unitPrice(unit(component.key), row.pricing_breakdown_known)}
            </div>
          </div>
        ))}
      </div>
      <div className='text-muted-foreground flex flex-wrap gap-x-6 gap-y-1 text-xs'>
        <span>
          {t('Billing other or difference')}:&nbsp;
          <span className='text-foreground font-mono'>
            {money(amount('other'))}
          </span>
        </span>
        <span>
          {t('Total')}:&nbsp;
          <span className='text-foreground font-mono font-semibold'>
            {money(amount('total'))}
          </span>
        </span>
      </div>
    </div>
  )
}

export function BillingReportTable({
  rows,
  loading,
}: {
  rows: BillingReportRow[]
  loading: boolean
}) {
  const { t } = useTranslation()
  const [expandedId, setExpandedId] = useState<number | null>(null)

  return (
    <div className='overflow-hidden rounded-xl border'>
      <Table className='min-w-[1320px]'>
        <TableHeader className='bg-muted/40'>
          <TableRow>
            <TableHead className='w-10' />
            <TableHead>{t('Date')}</TableHead>
            <TableHead>{t('Customer')}</TableHead>
            <TableHead>{t('Model and token')}</TableHead>
            <TableHead>{t('Channel')}</TableHead>
            <TableHead className='text-right'>{t('Calls')}</TableHead>
            <TableHead className='text-right'>{t('Input')}</TableHead>
            <TableHead className='text-right'>{t('Output')}</TableHead>
            <TableHead className='text-right'>{t('Cache R/W')}</TableHead>
            <TableHead className='text-right'>{t('Original total')}</TableHead>
            <TableHead className='text-right'>{t('Group ratio')}</TableHead>
            <TableHead className='text-right'>{t('Adjusted total')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading && rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={12} className='h-32 text-center'>
                <span className='text-muted-foreground'>
                  {t('Loading billing data...')}
                </span>
              </TableCell>
            </TableRow>
          )}
          {!loading && rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={12} className='h-32 text-center'>
                <span className='text-muted-foreground'>
                  {t('No billing data in this range')}
                </span>
              </TableCell>
            </TableRow>
          )}
          {rows.map((row) => {
            const expanded = expandedId === row.id
            return (
              <Fragment key={row.id}>
                <TableRow className={cn(expanded && 'bg-muted/30')}>
                  <TableCell>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='size-7'
                      aria-label={t('View pricing details')}
                      onClick={() => setExpandedId(expanded ? null : row.id)}
                    >
                      {expanded ? (
                        <ChevronDown className='size-4' />
                      ) : (
                        <ChevronRight className='size-4' />
                      )}
                    </Button>
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {row.bill_date}
                  </TableCell>
                  <TableCell>
                    <div className='font-medium'>{row.username || '—'}</div>
                    <div className='text-muted-foreground text-xs'>
                      {row.user_group || '—'}
                      {row.third_party_group
                        ? ` · ${row.third_party_group}`
                        : ''}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className='max-w-52 truncate font-medium'>
                      {row.model_name || '—'}
                    </div>
                    <div className='text-muted-foreground max-w-52 truncate text-xs'>
                      {row.token_name || '—'}
                    </div>
                    {row.matched_tier && (
                      <div className='text-muted-foreground max-w-52 truncate text-xs'>
                        {t('Billing matched tier')}: {row.matched_tier}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className='max-w-48 truncate'>
                      {row.channel_name || `#${row.channel_id}`}
                    </div>
                    <div className='text-muted-foreground max-w-48 truncate text-xs'>
                      {row.channel_tag || row.upstream_url || '—'}
                    </div>
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {countFormatter.format(row.call_count)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {countFormatter.format(row.input_tokens)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {countFormatter.format(row.output_tokens)}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs tabular-nums'>
                    {countFormatter.format(row.cache_read_tokens)} /{' '}
                    {countFormatter.format(row.cache_write_tokens)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {row.group_ratio_known ? money(row.original_total) : '—'}
                  </TableCell>
                  <TableCell className='text-right'>
                    {row.group_ratio_known ? (
                      <Badge variant='secondary' className='font-mono'>
                        {decimal(row.group_ratio).toLocaleString(undefined, {
                          maximumFractionDigits: 6,
                        })}
                        ×
                      </Badge>
                    ) : (
                      <span className='text-muted-foreground'>—</span>
                    )}
                  </TableCell>
                  <TableCell className='text-right font-mono font-semibold tabular-nums'>
                    {money(row.adjusted_total)}
                  </TableCell>
                </TableRow>
                {expanded && (
                  <TableRow className='bg-muted/20'>
                    <TableCell colSpan={12} className='p-4'>
                      <div className='grid gap-5 xl:grid-cols-2'>
                        {row.group_ratio_known ? (
                          <Breakdown row={row} adjusted={false} />
                        ) : (
                          <div className='text-muted-foreground flex min-h-28 items-center justify-center rounded-lg border border-dashed px-4 text-center text-sm'>
                            {t(
                              'Original pricing is unavailable for logs created before the billing snapshot was enabled'
                            )}
                          </div>
                        )}
                        <Breakdown row={row} adjusted />
                      </div>
                      <div className='text-muted-foreground mt-4 flex flex-wrap gap-x-6 gap-y-1 border-t pt-3 text-xs'>
                        <span>
                          {t('Billing billing mode')}: {row.billing_mode || '—'}
                        </span>
                        <span>
                          {t('Billing matched tier')}: {row.matched_tier || '—'}
                        </span>
                        <span>
                          {t('Billing itemized unit prices')}:{' '}
                          {row.pricing_breakdown_known
                            ? t('Billing price confirmed')
                            : t('Billing price unconfirmed')}
                        </span>
                        <span>
                          {t('Upstream')}: {row.upstream_url || '—'}
                        </span>
                      </div>
                      <p className='text-muted-foreground mt-2 text-xs'>
                        {t('Billing difference fee explanation')}
                      </p>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
