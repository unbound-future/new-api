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

export type DecimalValue = string | number

export interface BillingReportRow {
  id: number
  bill_date: string
  user_id: number
  username: string
  user_group: string
  third_party_group: string
  channel_id: number
  channel_name: string
  channel_tag: string
  upstream_url: string
  model_name: string
  token_id: number
  token_name: string
  billing_mode: string
  matched_tier: string
  pricing_breakdown_known: boolean
  group_ratio: DecimalValue
  group_ratio_known: boolean
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  call_count: number
  original_input: DecimalValue
  original_output: DecimalValue
  original_cache_read: DecimalValue
  original_cache_write: DecimalValue
  original_other: DecimalValue
  original_total: DecimalValue
  adjusted_input: DecimalValue
  adjusted_output: DecimalValue
  adjusted_cache_read: DecimalValue
  adjusted_cache_write: DecimalValue
  adjusted_other: DecimalValue
  adjusted_total: DecimalValue
  original_input_unit: DecimalValue
  original_output_unit: DecimalValue
  original_cache_read_unit: DecimalValue
  original_cache_write_unit: DecimalValue
  adjusted_input_unit: DecimalValue
  adjusted_output_unit: DecimalValue
  adjusted_cache_read_unit: DecimalValue
  adjusted_cache_write_unit: DecimalValue
  created_at: number
  updated_at: number
}

export interface BillingReportTotals {
  call_count: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  original_total: DecimalValue
  adjusted_total: DecimalValue
}

export interface BillingReportListData {
  items: BillingReportRow[]
  total: number
  page: number
  page_size: number
  totals: BillingReportTotals
}

export interface BillingReportFilters {
  start_date: string
  end_date: string
  username: string
  user_group: string
  third_party_group: string
  channel_tag: string
  channel_name: string
  upstream_url: string
  model_name: string
  token_name: string
}

export interface BillingReportState {
  auto_enabled: boolean
  initialized: boolean
  live_cursor_id: number
  history_date: string
  history_cursor_id: number
  history_cutoff_id: number
  last_auto_run_at: number
  last_synced_at: number
  last_source_log_at: number
  processed_logs: number
  status: 'idle' | 'syncing' | 'rebuilding' | 'error' | string
  last_error: string
  updated_at: number
}

export interface BillingReportJob {
  id: number
  start_date: string
  end_date: string
  current_date: string
  cursor_id: number
  cutoff_id: number
  status: 'pending' | 'running' | 'completed' | 'failed' | string
  processed_logs: number
  processed_days: number
  total_days: number
  error_message: string
  created_at: number
  started_at: number
  finished_at: number
  updated_at: number
}

export interface BillingReportStatus {
  enabled: boolean
  state: BillingReportState
  active_job?: BillingReportJob
  pending_jobs: number
  auto_interval_seconds: number
  batch_size: number
  backfill_start_date: string
}

export interface ApiEnvelope<T> {
  success: boolean
  message: string
  data: T
}
