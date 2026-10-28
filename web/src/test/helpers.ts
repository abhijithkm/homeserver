import { vi } from 'vitest'
import { setCsrf, onUnauthorized } from '../api'
import type { Entry, Share, User } from '../types'

export interface Call {
  method: string
  path: string
  query: URLSearchParams
  url: string
  body: any
  headers: Record<string, string>
}
export type Reply = { status?: number; json?: unknown }
export type Handler = Reply | ((c: Call) => Reply | Promise<Reply>)

/** Installs a fake fetch. Routes are keyed "METHOD /path" (query ignored). */
export function mockFetch(routes: Record<string, Handler>) {
  const calls: Call[] = []
  const table = { ...routes }
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const u = new URL(url, 'http://localhost')
    const method = (init?.method ?? 'GET').toUpperCase()
    let body: any
    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = init.body
      }
    }
    const call: Call = { method, path: u.pathname, query: u.searchParams, url, body, headers: (init?.headers ?? {}) as Record<string, string> }
    calls.push(call)
    const h = table[`${method} ${u.pathname}`]
    const r: Reply = h === undefined ? { status: 404, json: { error: `no mock for ${method} ${u.pathname}` } } : typeof h === 'function' ? await h(call) : h
    const status = r.status ?? 200
    const text = r.json === undefined ? '' : JSON.stringify(r.json)
    return { ok: status >= 200 && status < 300, status, text: async () => text } as unknown as Response
  })
  vi.stubGlobal('fetch', fn)
  return {
    calls,
    set: (k: string, h: Handler) => {
      table[k] = h
    },
    find: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path),
  }
}

export const admin: User = { id: 1, username: 'alice', role: 'admin', disabled: false, createdAt: 1700000000 }
export const writer: User = { id: 2, username: 'bob', role: 'writer', disabled: false, createdAt: 1700000000 }
export const reader: User = { id: 3, username: 'carol', role: 'reader', disabled: false, createdAt: 1700000000 }

export const session = (user: User) => ({ json: { user, csrf: 'csrf-token-1', version: '1.0.0', tls: false } })

export const share = (o: Partial<Share> = {}): Share => ({
  id: 's1', name: 'Photos', readOnly: false, enabled: true, status: 'online', canWrite: true, ...o,
})

export const folder = (name: string): Entry => ({ name, isDir: true, size: 0, modTime: 1700000000000, type: 'folder' })
export const file = (name: string, size = 1234, type = 'txt'): Entry => ({ name, isDir: false, size, modTime: 1700000000000, type })

export const listing = (entries: Entry[], o: Record<string, unknown> = {}) => ({
  json: { path: '', entries, total: entries.length, offset: 0, limit: 100, canWrite: true, ...o },
})

/** Resets module-level API state between tests. */
export function resetApi() {
  setCsrf('')
  onUnauthorized(null)
}

// ---- fake XMLHttpRequest ----
export class FakeXHR {
  static all: FakeXHR[] = []
  method = ''
  url = ''
  headers: Record<string, string> = {}
  body: unknown = null
  status = 0
  responseText = ''
  aborted = false
  upload: { onprogress: ((e: { loaded: number; total: number; lengthComputable: boolean }) => void) | null } = { onprogress: null }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  ontimeout: (() => void) | null = null
  constructor() {
    FakeXHR.all.push(this)
  }
  open(m: string, u: string) {
    this.method = m
    this.url = u
  }
  setRequestHeader(k: string, v: string) {
    this.headers[k] = v
  }
  send(b: unknown) {
    this.body = b
  }
  abort() {
    this.aborted = true
    this.onabort?.()
  }
  get query() {
    return new URL(this.url, 'http://localhost').searchParams
  }
  progress(loaded: number, total: number) {
    this.upload.onprogress?.({ loaded, total, lengthComputable: true })
  }
  respond(status: number, json?: unknown) {
    this.status = status
    this.responseText = json === undefined ? '' : JSON.stringify(json)
    this.onload?.()
  }
  static install() {
    FakeXHR.all = []
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
  }
}
