const BASE_URL = '/api'
const ADMIN_KEY_STORAGE_KEY = 'fusionapi_admin_api_key'

function getStoredAdminKey(): string {
  if (typeof window === 'undefined') return ''
  try {
    return (window.localStorage.getItem(ADMIN_KEY_STORAGE_KEY) || '').trim()
  } catch {
    return ''
  }
}

function setStoredAdminKey(key: string) {
  if (typeof window === 'undefined') return
  try {
    const value = key.trim()
    if (value) {
      window.localStorage.setItem(ADMIN_KEY_STORAGE_KEY, value)
    } else {
      window.localStorage.removeItem(ADMIN_KEY_STORAGE_KEY)
    }
  } catch {
    // ignore localStorage errors
  }
}

export interface Source {
  id: string
  name: string
  type: 'newapi' | 'cpa' | 'openai' | 'anthropic' | 'custom'
  base_url: string
  api_key?: string
  priority: number
  weight: number
  enabled: boolean
  capabilities: {
    function_calling: boolean
    extended_thinking: boolean
    vision: boolean
    models: string[]
  }
  cpa?: {
    providers: string[]
    account_mode: string
    auto_detect: boolean
  }
  status?: {
    state: 'healthy' | 'unhealthy' | 'removed'
    latency: number
    balance: number
    last_error: string
    model_providers?: Record<string, string>
  }
}

export interface RequestLog {
  id: string
  timestamp: string
  source_id: string
  source_name: string
  model: string
  has_tools: boolean
  has_thinking: boolean
  stream: boolean
  success: boolean
  status_code: number
  latency_ms: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  error: string
  failover_from: string
  client_ip?: string
  client_tool?: string
  api_key_id?: string
  fc_compat_used?: boolean
}

export interface KeyDailyUsage {
  date: string
  request_count: number
  success_count: number
  fail_count: number
  total_tokens: number
  avg_latency_ms: number
}

export interface APIKey {
  id: string
  key: string
  name: string
  enabled: boolean
  limits: KeyLimits
  allowed_tools: string[]
  created_at: string
  last_used_at: string
  daily_usage?: number
}

export interface KeyLimits {
  rpm: number
  daily_quota: number
  concurrent: number
  tool_quotas?: Record<string, number>
}

export interface ToolStats {
  tool: string
  request_count: number
  last_used_at: string
}

export interface Stats {
  daily: {
    date: string
    total_requests: number
    success_rate: number
    total_tokens: number
    avg_latency_ms: number
  }[]
  sources: {
    source_id: string
    source_name: string
    request_count: number
    success_rate: number
    avg_latency_ms: number
    total_tokens: number
  }[]
}

export interface SystemStatus {
  total_sources: number
  healthy_sources: number
  unhealthy_sources: number
  disabled_sources: number
  routing_strategy: string
  failover_enabled: boolean
}

async function request<T>(url: string, options?: RequestInit, retried = false): Promise<T> {
  const headers = new Headers(options?.headers || {})
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const adminKey = getStoredAdminKey()
  if (adminKey) {
    headers.set('Authorization', `Bearer ${adminKey}`)
  }

  const res = await fetch(BASE_URL + url, {
    ...options,
    headers
  })

  if (res.status === 401 && !retried && typeof window !== 'undefined') {
    const input = window.prompt('管理 API 需要 admin_api_key，请输入（仅保存在当前浏览器）', adminKey || '')
    if (input !== null) {
      setStoredAdminKey(input)
      return request<T>(url, options, true)
    }
  }

  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: { message: 'Request failed' } }))
    throw new Error(error.error?.message || 'Request failed')
  }

  return res.json()
}

// Sources API
export const sourcesApi = {
  list: () => request<{ data: Source[] }>('/sources').then(r => r.data),

  get: (id: string) => request<{ data: Source }>(`/sources/${id}`).then(r => r.data),

  create: (source: Partial<Source>) =>
    request<{ data: Source }>('/sources', {
      method: 'POST',
      body: JSON.stringify(source)
    }).then(r => r.data),

  update: (id: string, source: Partial<Source>) =>
    request<{ data: Source }>(`/sources/${id}`, {
      method: 'PUT',
      body: JSON.stringify(source)
    }).then(r => r.data),

  delete: (id: string) =>
    request<{ message: string }>(`/sources/${id}`, { method: 'DELETE' }),

  test: (id: string) =>
    request<{ success: boolean; error?: string }>(`/sources/${id}/test`, { method: 'POST' }),

  balance: (id: string) =>
    request<{ success: boolean; balance?: number; error?: string }>(`/sources/${id}/balance`)
}

// Status API
export const statusApi = {
  get: () => request<SystemStatus>('/status'),
  health: () => request<{ data: any[] }>('/health').then(r => r.data)
}

// Logs API
export const logsApi = {
  list: (params?: {
    source_id?: string
    model?: string
    success?: boolean
    client_tool?: string
    fc_compat?: boolean
    limit?: number
    offset?: number
  }) => {
    const query = new URLSearchParams()
    if (params) {
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined) query.append(key, String(value))
      })
    }
    const url = '/logs' + (query.toString() ? '?' + query.toString() : '')
    return request<{ data: RequestLog[] }>(url).then(r => r.data || [])
  }
}

// Stats API
export const statsApi = {
  get: () => request<Stats>('/stats')
}

// Config API
export const configApi = {
  get: () => request<any>('/config'),
  update: (config: any) => request<{ message: string; restart_required?: boolean }>('/config', {
    method: 'PUT',
    body: JSON.stringify(config)
  })
}

// Keys API
export const keysApi = {
  list: () => request<{ data: APIKey[] }>('/keys').then(r => r.data || []),

  get: (id: string) => request<{ data: APIKey }>(`/keys/${id}`).then(r => r.data),

  create: (data: { name: string; limits?: KeyLimits; allowed_tools?: string[] }) =>
    request<{ data: APIKey }>('/keys', {
      method: 'POST',
      body: JSON.stringify(data)
    }).then(r => r.data),

  update: (id: string, data: Partial<APIKey>) =>
    request<{ data: APIKey }>(`/keys/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data)
    }).then(r => r.data),

  delete: (id: string) =>
    request<{ message: string }>(`/keys/${id}`, { method: 'DELETE' }),

  rotate: (id: string) =>
    request<{ data: APIKey }>(`/keys/${id}/rotate`, { method: 'POST' }).then(r => r.data),

  block: (id: string) =>
    request<{ data: APIKey }>(`/keys/${id}/block`, { method: 'PUT' }).then(r => r.data),

  unblock: (id: string) =>
    request<{ data: APIKey }>(`/keys/${id}/unblock`, { method: 'PUT' }).then(r => r.data),

  getUsage: (id: string, days?: number) =>
    request<{ data: KeyDailyUsage[] }>(`/keys/${id}/usage?days=${days || 7}`).then(r => r.data || []),
}

// Tools API
export const toolsApi = {
  stats: () => request<{ data: ToolStats[] }>('/tools/stats').then(r => r.data || [])
}

export const adminAuthApi = {
  get: () => getStoredAdminKey(),
  set: (key: string) => setStoredAdminKey(key),
  clear: () => setStoredAdminKey('')
}
