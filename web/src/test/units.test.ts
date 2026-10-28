import { describe, expect, it } from 'vitest'
import { canPreview, formatBytes, formatUptime, joinBytes, joinPath, parentPath, splitBytes } from '../format'
import { filesHash, parseHash } from '../router'

describe('format', () => {
  it('formats bytes and uptime', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(5 * 1024 ** 3)).toBe('5.0 GB')
    expect(formatUptime(59)).toBe('59s')
    expect(formatUptime(3725)).toBe('1h 2m')
    expect(formatUptime(90000)).toBe('1d 1h 0m')
  })
  it('splits and joins upload limits', () => {
    expect(splitBytes(0)).toEqual({ value: '0', unit: 'GB' })
    expect(splitBytes(2 * 1024 ** 3)).toEqual({ value: '2', unit: 'GB' })
    expect(splitBytes(512 * 1024 ** 2)).toEqual({ value: '512', unit: 'MB' })
    expect(joinBytes('1.5', 'GB')).toBe(1.5 * 1024 ** 3)
    expect(joinBytes('-1', 'MB')).toBeNaN()
    expect(joinBytes('abc', 'MB')).toBeNaN()
  })
  it('handles paths and previews', () => {
    expect(joinPath('', 'a')).toBe('a')
    expect(joinPath('x/y', 'a')).toBe('x/y/a')
    expect(parentPath('x/y')).toBe('x')
    expect(parentPath('x')).toBe('')
    expect(canPreview('PNG')).toBe(true)
    expect(canPreview('txt')).toBe(false)
  })
})

describe('router', () => {
  it('parses and builds hashes', () => {
    expect(parseHash('')).toEqual({ page: 'files' })
    expect(parseHash('#/admin/users')).toEqual({ page: 'admin', tab: 'users' })
    expect(parseHash('#/admin')).toEqual({ page: 'admin', tab: 'dashboard' })
    expect(parseHash('#/account')).toEqual({ page: 'account' })
    expect(parseHash('#/files/s1/my%20docs/x')).toEqual({ page: 'files', shareId: 's1', path: 'my docs/x' })
    expect(filesHash('s1', 'my docs/x#1')).toBe('#/files/s1/my%20docs/x%231')
    expect(parseHash(filesHash('s1', 'a b/c#d')).path).toBe('a b/c#d')
  })
})
