export type Role = 'admin' | 'writer' | 'reader'
export type ShareStatus = 'online' | 'offline' | 'denied' | 'disabled'

export interface User {
  id: number
  username: string
  role: Role
  disabled: boolean
  createdAt: number
}

export interface Session {
  user: User
  csrf: string
  version?: string
  tls?: boolean
}

export interface SetupStatus {
  needsSetup: boolean
  local: boolean
}

export interface Share {
  id: string
  name: string
  path?: string
  readOnly: boolean
  enabled: boolean
  status: ShareStatus
  problem?: string
  canWrite: boolean
}

export interface Entry {
  name: string
  isDir: boolean
  size: number
  modTime: number
  type: string
}

export interface ListResponse {
  path: string
  entries: Entry[]
  total: number
  offset: number
  limit: number
  canWrite: boolean
}

export type SortKey = 'name' | 'size' | 'type' | 'modified'

export interface Settings {
  port: number
  bindAddr: string
  maxUploadBytes: number
  maxTransfers: number
  sessionHours: number
  tlsMode: 'off' | 'selfsigned' | 'custom'
  tlsCert: string
  tlsKey: string
}

export interface Status {
  version: string
  uptimeSeconds: number
  pid: number
  goroutines: number
  heapBytes: number
  sysBytes: number
  activeTransfers: number
  maxTransfers: number
  listening: string
  tls: boolean
  urls: string[]
  shares: number
  sharesUnavailable: number
  os: string
}

export interface NetworkInfo {
  urls: string[]
  addresses: { interface: string; ip: string; private: boolean }[] | null
  port: number
  tls: boolean
  listening: string
}

export interface Transfer {
  ts: number
  user: string
  share: string
  path: string
  direction: 'upload' | 'download'
  bytes: number
  status: 'ok' | 'aborted' | 'error'
  ip: string
}

export interface TransfersResponse {
  active: number
  recent: Transfer[] | null
}

export interface ShareDiag {
  name: string
  status: ShareStatus
  problem?: string
  freeBytes?: number
  totalBytes?: number
  lowDisk?: boolean
}

export interface Diagnose {
  database: boolean
  listening: string
  port: number
  tls: boolean
  urls: string[]
  shares: ShareDiag[]
  hints: string[]
  dataDir: string
}

export interface AuditEvent {
  ts: number
  actor: string
  ip: string
  event: string
  detail: string
}
