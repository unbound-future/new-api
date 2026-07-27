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
import { api } from '@/lib/api'
import type {
  ApiEnvelope,
  BillingReportFilters,
  BillingReportJob,
  BillingReportListData,
  BillingReportStatus,
} from './types'

function buildParams(
  filters: BillingReportFilters,
  page?: number,
  pageSize?: number
) {
  const params = new URLSearchParams()
  if (page) params.set('p', String(page))
  if (pageSize) params.set('page_size', String(pageSize))
  Object.entries(filters).forEach(([key, value]) => {
    if (value.trim()) params.set(key, value.trim())
  })
  return params
}

export async function getBillingReport(
  filters: BillingReportFilters,
  page: number,
  pageSize: number
) {
  const response = await api.get<ApiEnvelope<BillingReportListData>>(
    `/api/billing-report/?${buildParams(filters, page, pageSize)}`
  )
  return response.data.data
}

export async function getBillingReportStatus() {
  const response = await api.get<ApiEnvelope<BillingReportStatus>>(
    '/api/billing-report/status'
  )
  return response.data.data
}

export async function updateBillingReportAuto(enabled: boolean) {
  const response = await api.put('/api/billing-report/auto', { enabled })
  return response.data
}

export async function rebuildBillingReport(startDate: string, endDate: string) {
  const response = await api.post<ApiEnvelope<BillingReportJob>>(
    '/api/billing-report/rebuild',
    {
      start_date: startDate,
      end_date: endDate,
    }
  )
  return response.data.data
}

export async function exportBillingReport(filters: BillingReportFilters) {
  const response = await api.get(
    `/api/billing-report/export?${buildParams(filters)}`,
    {
      responseType: 'blob',
      disableDuplicate: true,
    } as Record<string, unknown>
  )
  const disposition = String(response.headers['content-disposition'] || '')
  const nameMatch = disposition.match(/filename="?([^";]+)"?/)
  const filename = nameMatch?.[1] || 'billing_usage.xlsx'
  const url = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}
