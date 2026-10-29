import { useCallback, useEffect, useState } from 'react'
import * as api from './api'
import type { BrowseResult } from './types'
import { Dialog, ErrorText, Spinner } from './ui'

interface Props {
  /** Folder to open first (the typed path); falls back to the drive list. */
  initial: string
  onPick: (path: string) => void
  onCancel: () => void
}

/** Browses folders on the server so a share can be chosen instead of typed. */
export function FolderPicker({ initial, onPick, onCancel }: Props) {
  const [cur, setCur] = useState<BrowseResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const go = useCallback(async (path: string): Promise<boolean> => {
    setLoading(true)
    setError('')
    try {
      setCur(await api.browse(path))
      setLoading(false)
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not list this folder.')
      setLoading(false)
      return false
    }
  }, [])

  useEffect(() => {
    void (async () => {
      const start = initial.trim()
      if (!(start && (await go(start)))) await go('')
    })()
    // Only the first open uses the typed path.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [go])

  const atDrives = cur !== null && cur.path === ''
  return (
    <Dialog
      title="Choose a folder"
      onClose={onCancel}
      actions={
        <>
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" disabled={!cur || atDrives} onClick={() => cur && onPick(cur.path)}>
            Use this folder
          </button>
        </>
      }
    >
      <p className="picker-here">
        {cur ? (atDrives ? 'Drives on the server' : <code>{cur.path}</code>) : 'Folders on the server'}
      </p>
      {cur && !atDrives ? (
        <button type="button" className="btn btn-sm" onClick={() => void go(cur.parent)}>
          Up
        </button>
      ) : null}
      <ErrorText>{error}</ErrorText>
      {loading ? <Spinner label="Loading folders..." /> : null}
      {cur && !loading ? (
        <>
          {cur.entries.length === 0 ? <p className="empty">No sub-folders here.</p> : null}
          <ul className="picker-list">
            {cur.entries.map((e) => (
              <li key={e.path}>
                <button type="button" className="picker-item" disabled={!e.readable} onClick={() => void go(e.path)}>
                  {e.name}
                  {e.readable ? null : <span className="muted"> (no access)</span>}
                </button>
              </li>
            ))}
          </ul>
          {cur.truncated ? <p className="muted">Only the first folders are shown. Type the path instead.</p> : null}
        </>
      ) : null}
    </Dialog>
  )
}
