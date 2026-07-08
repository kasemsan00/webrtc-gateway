export interface ClientDiagnostic {
  id: number
  timestamp: string
  clientTraceId?: string
  authSubject?: string
  authRealm?: string
  preferredUsername?: string
  source: string
  level: string
  name: string
  appVersion?: string
  platform?: string
  deviceIdHash?: string
  data?: Record<string, unknown>
}

export interface ClientDiagnosticListResponse {
  items: Array<ClientDiagnostic>
  total: number
  page: number
  pageSize: number
}

export interface ClientDiagnosticListParams {
  page?: number
  pageSize?: number
  clientTraceId?: string
  authSubject?: string
  source?: string
  level?: string
  name?: string
}
