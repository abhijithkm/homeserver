import { useCallback, useEffect, useState } from 'react'
import * as api from './api'
import type { SetupStatus, User } from './types'
import { useRoute } from './router'
import { BrandMark, Icon, Spinner } from './ui'
import { LoginScreen, SetupScreen } from './Auth'
import { FilesPage } from './Files'
import { AdminPage } from './Admin'
import { AccountPage } from './Account'

type Phase =
  | { kind: 'loading' }
  | { kind: 'error'; message: string }
  | { kind: 'setup'; status: SetupStatus }
  | { kind: 'login'; expired?: boolean }
  | { kind: 'app'; user: User; tls: boolean; version: string }

export default function App() {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' })
  const route = useRoute()

  const boot = useCallback(async () => {
    setPhase({ kind: 'loading' })
    try {
      const s = await api.getSession()
      setPhase({ kind: 'app', user: s.user, tls: !!s.tls, version: s.version ?? '' })
      return
    } catch (e) {
      if (!(e instanceof api.ApiError) || e.status !== 401) {
        setPhase({ kind: 'error', message: e instanceof Error ? e.message : 'Cannot reach the server.' })
        return
      }
    }
    try {
      const st = await api.setupStatus()
      setPhase(st.needsSetup ? { kind: 'setup', status: st } : { kind: 'login' })
    } catch (e) {
      setPhase({ kind: 'error', message: e instanceof Error ? e.message : 'Cannot reach the server.' })
    }
  }, [])

  useEffect(() => {
    void boot()
  }, [boot])

  // Any 401 from the API means the session ended.
  useEffect(() => {
    api.onUnauthorized(() => {
      api.setCsrf('')
      setPhase((p) => (p.kind === 'app' ? { kind: 'login', expired: true } : p))
    })
    return () => api.onUnauthorized(null)
  }, [])

  if (phase.kind === 'loading') {
    return (
      <main className="center-page">
        <Spinner label="Starting HomeServer..." />
      </main>
    )
  }
  if (phase.kind === 'error') {
    return (
      <main className="center-page">
        <div className="card narrow">
          <BrandMark large />
          <h1>HomeServer</h1>
          <p role="alert" className="error">
            {phase.message}
          </p>
          <button type="button" className="btn btn-primary" onClick={() => void boot()}>
            Try again
          </button>
        </div>
      </main>
    )
  }
  if (phase.kind === 'setup') return <SetupScreen local={phase.status.local} onDone={() => void boot()} />
  if (phase.kind === 'login') return <LoginScreen expired={phase.expired} onDone={() => void boot()} />

  const { user } = phase
  const isAdmin = user.role === 'admin'
  const page = route.page === 'admin' && !isAdmin ? 'files' : route.page

  const logout = async () => {
    try {
      await api.logout()
    } catch {
      /* the cookie may already be gone */
    }
    api.setCsrf('')
    window.location.hash = ''
    void boot()
  }

  return (
    <div className="shell">
      <a className="skip" href="#main">
        Skip to content
      </a>
      <div className="rail">
        <header className="brand">
          <BrandMark />
          <span className="brand-name">HomeServer</span>
        </header>
        <nav aria-label="Main" className="nav">
          <a href="#/files" aria-current={page === 'files' ? 'page' : undefined}>
            <Icon name="folder" />
            <span>Files</span>
          </a>
          {isAdmin ? (
            <a href="#/admin/dashboard" aria-current={page === 'admin' ? 'page' : undefined}>
              <Icon name="shield" />
              <span>Admin</span>
            </a>
          ) : null}
          <a href="#/account" aria-current={page === 'account' ? 'page' : undefined}>
            <Icon name="user" />
            <span>Account</span>
          </a>
        </nav>
        <div className="acct">
          <span className="who" title={`Signed in as ${user.username}`}>
            <span className="avatar" aria-hidden="true">
              {user.username.slice(0, 1).toUpperCase()}
            </span>
            <span className="who-text">
              <span className="who-name">{user.username}</span>
              <span className="badge badge-role">{user.role}</span>
            </span>
          </span>
          <button type="button" className="btn btn-sm btn-logout" onClick={() => void logout()}>
            <Icon name="logout" />
            <span>Log out</span>
          </button>
        </div>
        {phase.version ? <footer className="foot">HomeServer {phase.version}</footer> : null}
      </div>
      <main id="main" tabIndex={-1} className="content">
        {page === 'files' && <FilesPage user={user} route={route} />}
        {page === 'admin' && isAdmin && <AdminPage tls={phase.tls} tab={route.tab ?? 'dashboard'} currentUserId={user.id} />}
        {page === 'account' && <AccountPage user={user} />}
      </main>
    </div>
  )
}
