import type {
  AuditEvent, BrowseResult, Diagnose, ListResponse, NetworkInfo, Role, Session, Settings, Share, SetupStatus,
  SortKey, Status, TransfersResponse, UpdateResult, User,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

let csrf = ''
let unauthorizedHandler: (() => void) | null = null

export function setCsrf(token: string): void {
  csrf = token
}
export function getCsrf(): string {
  return csrf
}
/** The app registers a callback that runs whenever the server answers 401. */
export function onUnauthorized(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

interface Options {
  body?: unknown
  /** Do not treat a 401 as "session expired" (used by login / session probe). */
  quiet401?: boolean
}

function fallbackMessage(status: number): string {
  if (status === 413) return 'The file is larger than the server allows.'
  if (status === 503) return 'The server is busy or the folder is unavailable. Try again shortly.'
  if (status === 403) return 'You do not have permission to do that.'
  if (status === 404) return 'Not found.'
  return `The server returned an error (${status}).`
}

export async function request<T>(method: string, url: string, opts: Options = {}): Promise<T> {
  const headers: Record<string, string> = {}
  let body: string | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  if (method !== 'GET' && method !== 'HEAD') headers['X-CSRF-Token'] = csrf
  let res: Response
  try {
    res = await fetch(url, { method, headers, body, credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, 'Cannot reach the server. Check your connection.')
  }
  let data: unknown = null
  const text = await res.text()
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    if (res.status === 401 && !opts.quiet401) unauthorizedHandler?.()
    const msg = (data as { error?: string } | null)?.error
    throw new ApiError(res.status, msg || fallbackMessage(res.status))
  }
  return data as T
}

const get = <T,>(url: string, o?: Options) => request<T>('GET', url, o)
const post = <T,>(url: string, body?: unknown, o?: Options) => request<T>('POST', url, { ...o, body: body ?? {} })

const q = (params: Record<string, string | number | undefined>): string => {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') p.set(k, String(v))
  const s = p.toString()
  return s ? '?' + s : ''
}

// ---- auth ----
export const setupStatus = () => get<SetupStatus>('/api/setup-status', { quiet401: true })
export async function setup(username: string, password: string): Promise<Session> {
  const s = await post<Session>('/api/setup', { username, password }, { quiet401: true })
  setCsrf(s.csrf)
  return s
}
export async function login(username: string, password: string): Promise<Session> {
  const s = await post<Session>('/api/login', { username, password }, { quiet401: true })
  setCsrf(s.csrf)
  return s
}
export async function getSession(): Promise<Session> {
  const s = await get<Session>('/api/session', { quiet401: true })
  setCsrf(s.csrf)
  return s
}
export const logout = () => post<{ status: string }>('/api/logout')
export async function changePassword(current: string, next: string): Promise<Session> {
  const s = await post<Session>('/api/password', { current, new: next })
  setCsrf(s.csrf)
  return s
}

// ---- shares & files ----
export const listShares = () => get<Share[]>('/api/shares')
export const addShare = (name: string, path: string, readOnly: boolean) =>
  post<Share>('/api/shares', { name, path, readOnly })
export const updateShare = (id: string, patch: { name?: string; readOnly?: boolean; enabled?: boolean }) =>
  request<Share>('PATCH', `/api/shares/${encodeURIComponent(id)}`, { body: patch })
export const deleteShare = (id: string) => request<{ status: string }>('DELETE', `/api/shares/${encodeURIComponent(id)}`)

export interface ListParams {
  path: string
  sort: SortKey
  desc: boolean
  q: string
  offset: number
  limit: number
}
export const listDir = (id: string, p: ListParams) =>
  get<ListResponse>(
    `/api/shares/${encodeURIComponent(id)}/list` +
      q({ path: p.path, sort: p.sort, desc: p.desc ? 1 : undefined, q: p.q, offset: p.offset || undefined, limit: p.limit }),
  )

export const downloadUrl = (id: string, path: string, inline = false) =>
  `/api/shares/${encodeURIComponent(id)}/download` + q({ path, inline: inline ? 1 : undefined })

const shareOp = (id: string, op: string, body: Record<string, string>) =>
  post<{ status: string }>(`/api/shares/${encodeURIComponent(id)}/${op}`, body)
export const mkdir = (id: string, parent: string, name: string) => shareOp(id, 'mkdir', { path: parent, name })
export const renameItem = (id: string, path: string, name: string) => shareOp(id, 'rename', { path, name })
export const moveItem = (id: string, path: string, dest: string) => shareOp(id, 'move', { path, dest })
export const deleteItem = (id: string, path: string) => shareOp(id, 'delete', { path })

// ---- upload (XMLHttpRequest for progress) ----
export type ConflictPolicy = 'fail' | 'rename' | 'replace'

export interface UploadHandle {
  promise: Promise<{ name: string; size: number }>
  abort: () => void
}

export function uploadFile(
  shareId: string,
  dir: string,
  file: File,
  conflict: ConflictPolicy,
  onProgress: (loaded: number, total: number) => void,
): UploadHandle {
  const xhr = new XMLHttpRequest()
  const url =
    `/api/shares/${encodeURIComponent(shareId)}/upload` + q({ path: dir, name: file.name, conflict })
  const promise = new Promise<{ name: string; size: number }>((resolve, reject) => {
    xhr.open('POST', url)
    xhr.setRequestHeader('X-CSRF-Token', csrf)
    xhr.upload.onprogress = (e) => onProgress(e.loaded, e.lengthComputable ? e.total : file.size)
    xhr.onerror = () => reject(new ApiError(0, 'Network error during upload. Check your connection and retry.'))
    xhr.ontimeout = () => reject(new ApiError(0, 'The upload timed out.'))
    xhr.onabort = () => reject(new ApiError(-1, 'Canceled'))
    xhr.onload = () => {
      let data: { name?: string; size?: number; error?: string } | null = null
      try {
        data = JSON.parse(xhr.responseText)
      } catch {
        data = null
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve({ name: data?.name ?? file.name, size: data?.size ?? file.size })
        return
      }
      if (xhr.status === 401) unauthorizedHandler?.()
      reject(new ApiError(xhr.status, data?.error || fallbackMessage(xhr.status)))
    }
    xhr.send(file)
  })
  return { promise, abort: () => xhr.abort() }
}

// ---- network ----
export const getNetwork = () => get<NetworkInfo>('/api/network')
export const qrUrl = (i: number) => `/api/qr.png?i=${i}`

// ---- admin ----
export const getStatus = () => get<Status>('/api/admin/status')
export const getUpdate = () => get<UpdateResult>('/api/admin/update')
export const browse = (path: string) => get<BrowseResult>('/api/admin/browse' + q({ path }))
export const getDiagnose =() => get<Diagnose>('/api/admin/diagnose')
export const getSettings = () => get<Settings>('/api/admin/settings')
export const saveSettings = (s: Settings) =>
  request<{ settings: Settings; restartRequired: boolean }>('PUT', '/api/admin/settings', { body: s })
export const listUsers = () => get<User[]>('/api/admin/users')
export const addUser = (username: string, password: string, role: Role) =>
  post<User>('/api/admin/users', { username, password, role })
export const updateUser = (id: number, patch: { role?: Role; disabled?: boolean }) =>
  request<User>('PATCH', `/api/admin/users/${id}`, { body: patch })
export const deleteUser = (id: number) => request<{ status: string }>('DELETE', `/api/admin/users/${id}`)
export const resetUserPassword = (id: number, password: string) =>
  post<{ status: string }>(`/api/admin/users/${id}/password`, { password })
export const getAudit = () => get<AuditEvent[]>('/api/admin/audit')
export const getTransfers = () => get<TransfersResponse>('/api/admin/transfers')
export const getLogs = (lines = 200) => get<{ lines: string[] }>('/api/admin/logs' + q({ lines }))
export const backupUrl = '/api/admin/backup'
export const restartServer = () => post<{ status: string }>('/api/admin/restart')

export async function healthz(): Promise<boolean> {
  try {
    const r = await fetch('/healthz', { cache: 'no-store' })
    return r.ok
  } catch {
    return false
  }
}
