import { useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import { ErrorText, Notice } from './ui'

export function SetupScreen({ local, onDone }: { local: boolean; onDone: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (!local) {
    return (
      <main className="center-page">
        <div className="card narrow">
          <h1>HomeServer setup</h1>
          <Notice kind="warn">
            HomeServer has not been set up yet. For safety, the first administrator can only be created from the
            computer that runs the server. On that computer, open <strong>http://localhost:{window.location.port || '8686'}</strong> in a
            browser and finish setup there. Then come back to this device and sign in.
          </Notice>
        </div>
      </main>
    )
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== confirm) {
      setError('The two passwords do not match.')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.setup(username.trim(), password)
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed.')
      setBusy(false)
    }
  }

  return (
    <main className="center-page">
      <form className="card narrow" onSubmit={submit} aria-labelledby="setup-title">
        <h1 id="setup-title">Welcome to HomeServer</h1>
        <p>Create the administrator account. You will use it to choose which folders to share and to add other people.</p>
        <label htmlFor="su-user">Administrator username</label>
        <input id="su-user" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required autoFocus />
        <label htmlFor="su-pass">Password</label>
        <input id="su-pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required />
        <label htmlFor="su-pass2">Repeat password</label>
        <input id="su-pass2" type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
        <p className="hint">Use a long password you do not use anywhere else. Everyone on your network can reach the sign-in page.</p>
        <ErrorText>{error}</ErrorText>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Creating...' : 'Create administrator'}
        </button>
      </form>
    </main>
  )
}

export function LoginScreen({ expired, onDone }: { expired?: boolean; onDone: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.login(username.trim(), password)
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign-in failed.')
      setBusy(false)
    }
  }

  return (
    <main className="center-page">
      <form className="card narrow" onSubmit={submit} aria-labelledby="login-title">
        <h1 id="login-title">Sign in to HomeServer</h1>
        {expired ? <Notice kind="info">Your session ended. Please sign in again.</Notice> : null}
        <label htmlFor="li-user">Username</label>
        <input id="li-user" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required autoFocus />
        <label htmlFor="li-pass">Password</label>
        <input id="li-pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        <ErrorText>{error}</ErrorText>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </main>
  )
}
