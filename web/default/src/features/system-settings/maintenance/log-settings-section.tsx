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
import { useCallback, useEffect, useMemo, useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { DateTimePicker } from '@/components/datetime-picker'
import { deleteLogsBefore, getCosLogStatus } from '../api'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { CosLogStatus } from '../types'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  CosLogSamplePercent: z
    .number()
    .min(0)
    .max(100)
    .refine(
      (value) => Math.abs(value * 100 - Math.round(value * 100)) < 0.000001,
      { message: 'Use at most two decimal places.' }
    ),
})

type LogSettingsFormValues = z.infer<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultEnabled: boolean
  defaultSamplePercent: number
}

const HOURS_IN_DAY = 24
const COSLOG_STATUS_REFRESH_MS = 5000
const COSLOG_SAMPLE_PRESETS = [0, 1, 10, 25, 50, 100]

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const unitIndex = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )
  const value = bytes / 1024 ** unitIndex
  return `${value.toFixed(unitIndex === 0 ? 0 : 2)} ${units[unitIndex]}`
}

export function LogSettingsSection(props: LogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [cosLogStatus, setCosLogStatus] = useState<CosLogStatus | null>(null)
  const [cosLogStatusUnavailable, setCosLogStatusUnavailable] = useState(false)
  const form = useForm<LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues: {
      LogConsumeEnabled: props.defaultEnabled,
      CosLogSamplePercent: props.defaultSamplePercent,
    },
  })

  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isCleaning, setIsCleaning] = useState(false)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)

  useEffect(() => {
    form.reset({
      LogConsumeEnabled: props.defaultEnabled,
      CosLogSamplePercent: props.defaultSamplePercent,
    })
  }, [form, props.defaultEnabled, props.defaultSamplePercent])

  const refreshCosLogStatus = useCallback(async () => {
    try {
      const response = await getCosLogStatus()
      if (!response.success) throw new Error(response.message)
      setCosLogStatus(response.data)
      setCosLogStatusUnavailable(false)
    } catch {
      setCosLogStatusUnavailable(true)
    }
  }, [])

  useEffect(() => {
    void refreshCosLogStatus()
    const timer = window.setInterval(
      () => void refreshCosLogStatus(),
      COSLOG_STATUS_REFRESH_MS
    )
    return () => window.clearInterval(timer)
  }, [refreshCosLogStatus])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const onSubmit = async (values: LogSettingsFormValues) => {
    const updates: Array<{ key: string; value: boolean | number }> = []
    if (values.LogConsumeEnabled !== props.defaultEnabled) {
      updates.push({
        key: 'LogConsumeEnabled',
        value: values.LogConsumeEnabled,
      })
    }
    if (values.CosLogSamplePercent !== props.defaultSamplePercent) {
      updates.push({
        key: 'CosLogSamplePercent',
        value: values.CosLogSamplePercent,
      })
    }
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    await refreshCosLogStatus()
  }

  const cosLogInactive = !cosLogStatus?.enabled || !cosLogStatus.initialized

  const lastUploadText = cosLogStatus?.last_successful_upload
    ? new Date(cosLogStatus.last_successful_upload * 1000).toLocaleString()
    : t('Never')

  const handleRequestCleanLogs = () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setIsCleaning(true)
    try {
      const res = await deleteLogsBefore(purgeTimestamp)
      if (!res.success) {
        throw new Error(res.message || t('Failed to clean logs'))
      }
      const count = res.data ?? 0
      toast.success(
        count > 0
          ? t('{{count}} log entries removed.', { count })
          : t('No log entries matched the selected time.')
      )
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      toast.error(message)
    } finally {
      setIsCleaning(false)
    }
  }

  return (
    <SettingsSection
      title={t('Log Maintenance')}
      description={t('Control log retention and clean historical data.')}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='LogConsumeEnabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-start justify-between rounded-lg border p-4'>
                <div className='space-y-0.5 pe-4'>
                  <FormLabel className='text-base'>
                    {t('Record quota usage')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='space-y-4 rounded-lg border p-4'>
            <div>
              <h4 className='text-sm font-medium'>
                {t('COSLOG payload sampling')}
              </h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Store complete request and response payloads for a stable percentage of requests. Changes take effect without restarting.'
                )}
              </p>
            </div>

            {cosLogStatusUnavailable ? (
              <p className='text-destructive text-sm'>
                {t('Unable to load COSLOG status.')}
              </p>
            ) : null}

            {cosLogStatus && cosLogInactive ? (
              <p className='text-muted-foreground bg-muted rounded-md p-3 text-sm'>
                {t(
                  'COSLOG is disabled or was not initialized at startup. You can save a percentage now, but capture starts only after COSLOG_ENABLED is enabled and the service is restarted.'
                )}
              </p>
            ) : null}

            <FormField
              control={form.control}
              name='CosLogSamplePercent'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Payload sample percentage')}</FormLabel>
                  <div className='flex max-w-xs items-center gap-2'>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={100}
                        step={0.01}
                        name={field.name}
                        ref={field.ref}
                        value={field.value}
                        onBlur={field.onBlur}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <span className='text-muted-foreground'>%</span>
                  </div>
                  <FormDescription>
                    {t(
                      '0% stores none; 100% stores every eligible request. Selected records keep their complete current COSLOG payload.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex flex-wrap gap-2'>
              {COSLOG_SAMPLE_PRESETS.map((percent) => (
                <Button
                  key={percent}
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() =>
                    form.setValue('CosLogSamplePercent', percent, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                >
                  {percent}%
                </Button>
              ))}
            </div>

            {cosLogStatus ? (
              <div className='grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4'>
                <div className='bg-muted rounded-md p-3'>
                  <div className='text-muted-foreground'>
                    {t('Queue depth')}
                  </div>
                  <div className='font-medium'>
                    {cosLogStatus.queue_depth} / {cosLogStatus.queue_capacity}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t('Buffered')}: {cosLogStatus.buffered_entries} /{' '}
                    {cosLogStatus.flush_size}
                  </div>
                </div>
                <div className='bg-muted rounded-md p-3'>
                  <div className='text-muted-foreground'>
                    {t('Local usage')}
                  </div>
                  <div className='font-medium'>
                    {formatBytes(cosLogStatus.local_bytes)}
                  </div>
                </div>
                <div className='bg-muted rounded-md p-3'>
                  <div className='text-muted-foreground'>
                    {t('Last successful upload')}
                  </div>
                  <div className='font-medium'>{lastUploadText}</div>
                </div>
                <div className='bg-muted rounded-md p-3'>
                  <div className='text-muted-foreground'>{t('Dropped')}</div>
                  <div className='font-medium'>
                    {cosLogStatus.dropped_total}
                  </div>
                </div>
              </div>
            ) : null}
          </div>

          <div className='space-y-4 rounded-lg border p-4'>
            <div>
              <h4 className='text-sm font-medium'>{t('Clean history logs')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Remove all log entries created before the selected timestamp.'
                )}
              </p>
            </div>
            <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
            <div className='flex flex-wrap gap-3'>
              {quickSelectOptions.map((option) => (
                <Button
                  key={option.label}
                  type='button'
                  variant='outline'
                  onClick={() => setPurgeDate(option.getValue())}
                >
                  {t(option.label)}
                </Button>
              ))}
              <Button
                type='button'
                variant='destructive'
                onClick={handleRequestCleanLogs}
                disabled={isCleaning}
              >
                {isCleaning ? t('Cleaning...') : t('Clean logs')}
              </Button>
            </div>
          </div>

          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending ? t('Saving...') : t('Save log settings')}
          </Button>
        </form>
      </Form>
      <AlertDialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm log cleanup')}</AlertDialogTitle>
            <AlertDialogDescription>
              {formattedPurgeDate
                ? t(
                    'This will permanently remove all log entries created before {{date}}.',
                    { date: formattedPurgeDate }
                  )
                : t(
                    'This will permanently remove log entries before the selected timestamp.'
                  )}{' '}
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isCleaning}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleCleanLogs} disabled={isCleaning}>
              {isCleaning ? t('Cleaning...') : t('Delete logs')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
