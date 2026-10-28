import { useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import type { Role, Share, User } from './types'
import { formatUnix } from './format'
import { ConfirmDialog, Empty, ErrorText, Notice, PromptDialog, Spinner } from './ui'
import { ShareBadges, shareProblemText } from './Files'
import { useLoad } from './useLoad'

type ShareDlg = { kind: 'rename' | 'remove'; share: Share }

export function SharesAdmin() {
  const load = useLoad<Share[]>(api.listShares)
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [readOnly, setReadOnly] = useState(true)
  const [addError, setAddError] = useState('')
  const [adding, setAdding] = useState(false)
  const [dlg, setDlg] = useState<ShareDlg | null>(null)
  const [busy, setBusy] = useState(false)
  const [dlgError, setDlgError] = useState('')
  const [rowError, setRowError] = useState('')

  const add = async (e: FormEvent) => {
    e.preventDefault()
    setAdding(true)
    setAddError('')
    try {
      await api.addShare(name.trim(), path.trim(), readOnly)
      setName('')
      setPath('')
      setReadOnly(true)
      load.reload()
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Could not add the share.')
    }
    setAdding(false)
  }

  const patch = async (s: Share, p: { readOnly?: boolean; enabled?: boolean }) => {
    setRowError('')
    try {
      await api.updateShare(s.id, p)
      load.reload()
    } catch (err) {
      setRowError(err instanceof Error ? err.message : 'Could not update the share.')
    }
  }

  const closeDlg = () => {
    setDlg(null)
    setDlgError('')
    setBusy(false)
  }
  const run = async (op: () => Promise<unknown>) => {
    setBusy(true)
    setDlgError('')
    try {
      await op()
      closeDlg()
      load.reload()
    } catch (err) {
      setDlgError(err instanceof Error ? err.message : 'The action failed.')
      setBusy(false)
    }
  }

  const shares = load.data ?? []
  return (
    <div>
      <form className="card" onSubmit={add} aria-labelledby="addshare-h">
        <h2 id="addshare-h">Add a shared folder</h2>
        <div className="grid">
          <div>
            <label htmlFor="sh-name">Name</label>
            <input id="sh-name" value={name} onChange={(e) => setName(e.target.value)} required maxLength={64} />
          </div>
          <div>
            <label htmlFor="sh-path">Folder path on the server</label>
            <input id="sh-path" value={path} onChange={(e) => setPath(e.target.value)} placeholder="D:\Photos" required spellCheck={false} />
          </div>
        </div>
        <label className="check">
          <input type="checkbox" checked={readOnly} onChange={(e) => setReadOnly(e.target.checked)} />
          Read-only (people can browse and download but not change anything)
        </label>
        <ErrorText>{addError}</ErrorText>
        <button type="submit" className="btn btn-primary" disabled={adding}>
          {adding ? 'Adding...' : 'Add share'}
        </button>
      </form>

      <h2>Shared folders</h2>
      <ErrorText>{load.error || rowError}</ErrorText>
      {load.loading && !load.data ? <Spinner label="Loading shares..." /> : null}
      {load.data && shares.length === 0 ? <Empty>No folders are shared yet.</Empty> : null}
      <ul className="share-list">
        {shares.map((s) => {
          const problem = s.status === 'online' ? '' : shareProblemText(s)
          return (
            <li key={s.id} className="card share">
              <div className="share-main">
                <strong className="share-name">{s.name}</strong>
                <span className="badges">
                  <ShareBadges share={s} />
                </span>
              </div>
              <p className="path">
                <code>{s.path}</code>
              </p>
              {problem ? <p className="problem">{problem}</p> : null}
              <div className="row-actions">
                <button type="button" className="btn btn-sm" onClick={() => setDlg({ kind: 'rename', share: s })} aria-label={`Rename share ${s.name}`}>
                  Rename
                </button>
                <button type="button" className="btn btn-sm" onClick={() => void patch(s, { enabled: !s.enabled })} aria-label={`${s.enabled ? 'Disable' : 'Enable'} share ${s.name}`}>
                  {s.enabled ? 'Disable' : 'Enable'}
                </button>
                <button type="button" className="btn btn-sm" onClick={() => void patch(s, { readOnly: !s.readOnly })} aria-label={`Make ${s.name} ${s.readOnly ? 'read-write' : 'read-only'}`}>
                  {s.readOnly ? 'Make read-write' : 'Make read-only'}
                </button>
                <button type="button" className="btn btn-sm btn-danger-quiet" onClick={() => setDlg({ kind: 'remove', share: s })} aria-label={`Remove share ${s.name}`}>
                  Remove
                </button>
              </div>
            </li>
          )
        })}
      </ul>

      {dlg?.kind === 'rename' && (
        <PromptDialog
          title={`Rename share ${dlg.share.name}`}
          label="Share name"
          initial={dlg.share.name}
          submitLabel="Rename"
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onSubmit={(n) => void run(() => api.updateShare(dlg.share.id, { name: n }))}
        />
      )}
      {dlg?.kind === 'remove' && (
        <ConfirmDialog
          title="Remove this share?"
          danger
          confirmLabel="Remove share"
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onConfirm={() => void run(() => api.deleteShare(dlg.share.id))}
          message={
            <p>
              Stop sharing <strong>{dlg.share.name}</strong>? Your files are <strong>not</strong> deleted; the folder on the server stays exactly as it is. People simply lose access through HomeServer.
            </p>
          }
        />
      )}
    </div>
  )
}

type UserDlg = { kind: 'reset' | 'delete'; user: User }

export function UsersAdmin({ currentUserId }: { currentUserId: number }) {
  const load = useLoad<User[]>(api.listUsers)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('reader')
  const [addError, setAddError] = useState('')
  const [adding, setAdding] = useState(false)
  const [dlg, setDlg] = useState<UserDlg | null>(null)
  const [busy, setBusy] = useState(false)
  const [dlgError, setDlgError] = useState('')
  const [rowError, setRowError] = useState('')
  const [notice, setNotice] = useState('')

  const add = async (e: FormEvent) => {
    e.preventDefault()
    setAdding(true)
    setAddError('')
    try {
      await api.addUser(username.trim(), password, role)
      setUsername('')
      setPassword('')
      setRole('reader')
      load.reload()
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Could not add the user.')
    }
    setAdding(false)
  }

  const patch = async (u: User, p: { role?: Role; disabled?: boolean }) => {
    setRowError('')
    setNotice('')
    try {
      await api.updateUser(u.id, p)
      load.reload()
    } catch (err) {
      setRowError(err instanceof Error ? err.message : 'Could not update the user.')
    }
  }

  const closeDlg = () => {
    setDlg(null)
    setDlgError('')
    setBusy(false)
  }
  const run = async (op: () => Promise<unknown>, done?: string) => {
    setBusy(true)
    setDlgError('')
    try {
      await op()
      closeDlg()
      if (done) setNotice(done)
      load.reload()
    } catch (err) {
      setDlgError(err instanceof Error ? err.message : 'The action failed.')
      setBusy(false)
    }
  }

  const users = load.data ?? []
  return (
    <div>
      <form className="card" onSubmit={add} aria-labelledby="adduser-h">
        <h2 id="adduser-h">Add a user</h2>
        <div className="grid">
          <div>
            <label htmlFor="u-name">Username</label>
            <input id="u-name" value={username} onChange={(e) => setUsername(e.target.value)} required autoComplete="off" />
          </div>
          <div>
            <label htmlFor="u-pass">Password</label>
            <input id="u-pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="new-password" />
          </div>
          <div>
            <label htmlFor="u-role">Role</label>
            <select id="u-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
              <option value="reader">Reader (browse and download)</option>
              <option value="writer">Writer (also upload and edit)</option>
              <option value="admin">Admin (full control)</option>
            </select>
          </div>
        </div>
        <ErrorText>{addError}</ErrorText>
        <button type="submit" className="btn btn-primary" disabled={adding}>
          {adding ? 'Adding...' : 'Add user'}
        </button>
      </form>

      <h2>Users</h2>
      <ErrorText>{load.error || rowError}</ErrorText>
      {notice ? <Notice kind="ok">{notice}</Notice> : null}
      {load.loading && !load.data ? <Spinner label="Loading users..." /> : null}
      {load.data && users.length === 0 ? <Empty>No users.</Empty> : null}
      {users.length > 0 && (
        <div className="table-wrap">
          <table className="data">
            <caption className="sr-only">Users</caption>
            <thead>
              <tr>
                <th scope="col">Username</th>
                <th scope="col">Role</th>
                <th scope="col">Status</th>
                <th scope="col">Created</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => {
                const me = u.id === currentUserId
                return (
                  <tr key={u.id}>
                    <td>
                      {u.username} {me ? <span className="hint">(you)</span> : null}
                    </td>
                    <td>
                      <select aria-label={`Role for ${u.username}`} value={u.role} disabled={me} onChange={(e) => void patch(u, { role: e.target.value as Role })}>
                        <option value="admin">admin</option>
                        <option value="writer">writer</option>
                        <option value="reader">reader</option>
                      </select>
                    </td>
                    <td>
                      <span className={`badge badge-${u.disabled ? 'disabled' : 'online'}`}>{u.disabled ? 'disabled' : 'active'}</span>
                    </td>
                    <td>{formatUnix(u.createdAt)}</td>
                    <td>
                      <span className="row-actions">
                        <button type="button" className="btn btn-sm" onClick={() => setDlg({ kind: 'reset', user: u })} aria-label={`Reset password for ${u.username}`}>
                          Reset password
                        </button>
                        {!me && (
                          <>
                            <button type="button" className="btn btn-sm" onClick={() => void patch(u, { disabled: !u.disabled })} aria-label={`${u.disabled ? 'Enable' : 'Disable'} ${u.username}`}>
                              {u.disabled ? 'Enable' : 'Disable'}
                            </button>
                            <button type="button" className="btn btn-sm btn-danger-quiet" onClick={() => setDlg({ kind: 'delete', user: u })} aria-label={`Delete ${u.username}`}>
                              Delete
                            </button>
                          </>
                        )}
                      </span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {dlg?.kind === 'reset' && (
        <PromptDialog
          title={`Reset password for ${dlg.user.username}`}
          label="New password"
          type="password"
          submitLabel="Reset password"
          busy={busy}
          error={dlgError}
          hint="They will be signed out everywhere and must use the new password."
          onCancel={closeDlg}
          onSubmit={(pw) => void run(() => api.resetUserPassword(dlg.user.id, pw), `Password reset for ${dlg.user.username}.`)}
        />
      )}
      {dlg?.kind === 'delete' && (
        <ConfirmDialog
          title="Delete this user?"
          danger
          confirmLabel="Delete user"
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onConfirm={() => void run(() => api.deleteUser(dlg.user.id))}
          message={
            <p>
              Delete the account <strong>{dlg.user.username}</strong>? They will no longer be able to sign in. Files are not affected.
            </p>
          }
        />
      )}
    </div>
  )
}
