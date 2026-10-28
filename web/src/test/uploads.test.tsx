import { act, render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import { FakeXHR, file, listing, mockFetch, resetApi, session, share, writer } from './helpers'

beforeEach(() => {
  resetApi()
  FakeXHR.install()
})
afterEach(() => vi.unstubAllGlobals())

function open() {
  window.location.hash = '#/files/s1'
  const m = mockFetch({
    'GET /api/session': session(writer),
    'GET /api/shares': { json: [share()] },
    'GET /api/shares/s1/list': listing([file('old.txt')]),
  })
  render(<App />)
  return m
}
const pick = async (...files: File[]) => {
  const input = (await screen.findByLabelText('Choose files')) as HTMLInputElement
  await userEvent.upload(input, files)
}
const lastXhr = () => FakeXHR.all[FakeXHR.all.length - 1]

describe('uploads', () => {
  it('sends the raw file with CSRF header, shows progress, then success and refreshes the list', async () => {
    const m = open()
    const f = new File(['hello'], 'hello.txt', { type: 'text/plain' })
    await pick(f)
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    const x = lastXhr()
    expect(x.method).toBe('POST')
    expect(x.url).toContain('/api/shares/s1/upload')
    expect(x.query.get('name')).toBe('hello.txt')
    expect(x.query.get('conflict')).toBe('fail')
    expect(x.query.get('path')).toBeNull()
    expect(x.headers['X-CSRF-Token']).toBe('csrf-token-1')
    expect(x.body).toBe(f) // raw File, not FormData
    const before = m.find('GET', '/api/shares/s1/list').length

    act(() => x.progress(50, 100))
    const bar = await screen.findByRole('progressbar', { name: /hello.txt/ })
    expect(bar).toHaveAttribute('value', '50')
    expect(screen.getByText('50%')).toBeInTheDocument()

    act(() => x.respond(201, { name: 'hello.txt', size: 5 }))
    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
    await waitFor(() => expect(m.find('GET', '/api/shares/s1/list').length).toBeGreaterThan(before))
  })

  it('uploads several files with at most two at a time', async () => {
    open()
    await pick(new File(['a'], 'a.txt'), new File(['b'], 'b.txt'), new File(['c'], 'c.txt'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(2))
    act(() => FakeXHR.all[0].respond(201, { name: 'a.txt', size: 1 }))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(3))
    expect(FakeXHR.all[2].query.get('name')).toBe('c.txt')
  })

  it('also accepts dropped files', async () => {
    open()
    const zone = await screen.findByTestId('dropzone')
    const f = new File(['x'], 'dropped.bin')
    fireEvent.drop(zone, { dataTransfer: { files: [f], items: [] } })
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    expect(lastXhr().query.get('name')).toBe('dropped.bin')
  })

  it('offers Cancel / Keep both / Replace on a name conflict', async () => {
    open()
    const user = userEvent.setup()
    await pick(new File(['hello'], 'old.txt'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    act(() => lastXhr().respond(409, { error: 'an item with that name already exists' }))
    const dlg = await screen.findByRole('dialog', { name: /already exists/ })
    expect(within(dlg).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()

    await user.click(within(dlg).getByRole('button', { name: 'Keep both' }))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(2))
    expect(lastXhr().query.get('conflict')).toBe('rename')
    act(() => lastXhr().respond(409, { error: 'exists' }))

    const dlg2 = await screen.findByRole('dialog')
    await user.click(within(dlg2).getByRole('button', { name: 'Replace' }))
    // Replace needs a second, explicit confirmation.
    const confirm = await screen.findByRole('dialog', { name: 'Replace existing file?' })
    expect(FakeXHR.all).toHaveLength(2)
    await user.click(within(confirm).getByRole('button', { name: 'Replace file' }))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(3))
    expect(lastXhr().query.get('conflict')).toBe('replace')
  })

  it('cancelling the conflict dialog cancels that file', async () => {
    open()
    const user = userEvent.setup()
    await pick(new File(['hello'], 'old.txt'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    act(() => lastXhr().respond(409, { error: 'exists' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByText('Canceled')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(FakeXHR.all).toHaveLength(1)
  })

  it('shows the server message on failure and retries', async () => {
    open()
    const user = userEvent.setup()
    await pick(new File(['big'], 'big.iso'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    act(() => lastXhr().respond(413, { error: 'file exceeds the upload limit of 100 bytes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('big.iso: file exceeds the upload limit of 100 bytes')
    await user.click(screen.getByRole('button', { name: 'Retry upload of big.iso' }))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(2))
    act(() => lastXhr().respond(503, { error: 'the server is busy with other transfers; try again shortly' }))
    expect(await screen.findByText(/server is busy/)).toBeInTheDocument()
  })

  it('reports network errors', async () => {
    open()
    await pick(new File(['x'], 'x.txt'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    act(() => lastXhr().onerror?.())
    expect(await screen.findByRole('alert')).toHaveTextContent(/Network error during upload/)
  })

  it('cancels an upload in progress', async () => {
    open()
    const user = userEvent.setup()
    await pick(new File(['x'], 'slow.bin'))
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1))
    act(() => lastXhr().progress(10, 100))
    await user.click(await screen.findByRole('button', { name: 'Cancel upload of slow.bin' }))
    expect(lastXhr().aborted).toBe(true)
    expect(await screen.findByText('Canceled')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry upload of slow.bin' })).toBeInTheDocument()
  })
})
