import { useCallback, useEffect, useRef, useState } from 'react'

export interface Loaded<T> {
  data: T | null
  error: string
  loading: boolean
  reload: () => void
  setData: (d: T | null) => void
}

/** Runs an async loader on mount and on reload(); ignores stale results. */
export function useLoad<T>(fn: () => Promise<T>): Loaded<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [n, setN] = useState(0)
  const fnRef = useRef(fn)
  fnRef.current = fn
  useEffect(() => {
    let live = true
    setLoading(true)
    setError('')
    fnRef.current().then(
      (d) => {
        if (!live) return
        setData(d)
        setLoading(false)
      },
      (e: unknown) => {
        if (!live) return
        setError(e instanceof Error ? e.message : 'Something went wrong.')
        setLoading(false)
      },
    )
    return () => {
      live = false
    }
  }, [n])
  const reload = useCallback(() => setN((x) => x + 1), [])
  return { data, error, loading, reload, setData }
}
