export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '-'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

export function formatDate(ms: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

export function formatUnix(sec: number): string {
  return formatDate(sec * 1000)
}

export function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${sec % 60}s`
  return `${sec}s`
}

export type ByteUnit = 'KB' | 'MB' | 'GB'
const MULT: Record<ByteUnit, number> = { KB: 1024, MB: 1024 ** 2, GB: 1024 ** 3 }

/** Split a byte count into a friendly number + unit (0 = unlimited). */
export function splitBytes(n: number): { value: string; unit: ByteUnit } {
  if (n <= 0) return { value: '0', unit: 'GB' }
  for (const u of ['GB', 'MB', 'KB'] as ByteUnit[]) {
    const v = n / MULT[u]
    if (v >= 1 && Number.isInteger(v)) return { value: String(v), unit: u }
  }
  return { value: String(Math.round((n / MULT.KB) * 100) / 100), unit: 'KB' }
}

export function joinBytes(value: string, unit: ByteUnit): number {
  const n = Number(value)
  if (!Number.isFinite(n) || n < 0) return NaN
  return Math.round(n * MULT[unit])
}

/** Join path segments of a share-relative path. */
export function joinPath(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name
}

export function parentPath(p: string): string {
  const i = p.lastIndexOf('/')
  return i < 0 ? '' : p.slice(0, i)
}

/** The command that lets the service account read a folder (run it as administrator). */
export function grantCommand(path: string, readOnly: boolean): string {
  return `homeserver.exe grant "${path}"${readOnly ? ' --readonly' : ''}`
}

/** The folder named in a server message such as: run: homeserver.exe grant "D:\Photos" ... (or '' if none). */
export function grantPathFromError(message: string): string {
  return /homeserver\.exe grant "([^"]+)"/.exec(message)?.[1] ?? ''
}

const PREVIEW = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'mp4', 'webm', 'mp3', 'ogg', 'wav', 'm4a', 'pdf'])
export function canPreview(type: string): boolean {
  return PREVIEW.has(type.toLowerCase())
}
