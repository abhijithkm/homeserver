import { useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import type { User } from './types'
import { ErrorText, Notice } from './ui'

export function AccountPage({ user }: { user: User }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setDone(false)
    if (next !== again) {
      setError('The new passwords do not match.')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.changePassword(current, next)
      setDone(true)
      setCurrent('')
      setNext('')
      setAgain('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not change the password.')
    }
    setBusy(false)
  }

  const roleText =
    user.role === 'admin'
      ? 'Administrator: can manage shares, users and settings, and read and write files.'
      : user.role === 'writer'
        ? 'Writer: can download, upload and organise files in read-write shares.'
        : 'Reader: can browse and download files only.'

  return (
    <section aria-labelledby="acct-h">
      <h1 id="acct-h">Account</h1>
      <div className="card">
        <dl className="kv">
          <dt>Username</dt>
          <dd>{user.username}</dd>
          <dt>Role</dt>
          <dd>
            {user.role} <span className="hint">{roleText}</span>
          </dd>
        </dl>
      </div>
      <form className="card narrow-form" onSubmit={submit} aria-labelledby="pw-h">
        <h2 id="pw-h">Change password</h2>
        <label htmlFor="pw-cur">Current password</label>
        <input id="pw-cur" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
        <label htmlFor="pw-new">New password</label>
        <input id="pw-new" type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" required />
        <label htmlFor="pw-new2">Repeat new password</label>
        <input id="pw-new2" type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" required />
        <ErrorText>{error}</ErrorText>
        {done ? <Notice kind="ok">Password changed. Your other sessions were signed out.</Notice> : null}
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Saving...' : 'Change password'}
        </button>
      </form>
    </section>
  )
}
