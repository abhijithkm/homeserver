import { useEffect, useState } from 'react'

/** Tiny hash router. Routes: #/files, #/files/<shareId>/<path...>, #/admin/<tab>, #/account */
export interface Route {
  page: 'files' | 'admin' | 'account'
  shareId?: string
  path?: string
  tab?: string
}

export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter((s) => s !== '')
  const safe = (s: string) => {
    try {
      return decodeURIComponent(s)
    } catch {
      return s
    }
  }
  if (parts[0] === 'admin') return { page: 'admin', tab: parts[1] ? safe(parts[1]) : 'dashboard' }
  if (parts[0] === 'account') return { page: 'account' }
  if (parts[0] === 'files' && parts[1]) {
    return { page: 'files', shareId: safe(parts[1]), path: parts.slice(2).map(safe).join('/') }
  }
  return { page: 'files' }
}

export function filesHash(shareId: string, path = ''): string {
  const segs = path ? path.split('/').map(encodeURIComponent).join('/') : ''
  return `#/files/${encodeURIComponent(shareId)}${segs ? '/' + segs : ''}`
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash))
  useEffect(() => {
    const on = () => setRoute(parseHash(window.location.hash))
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return route
}

export function navigate(hash: string): void {
  if (window.location.hash === hash) return
  window.location.hash = hash
}
