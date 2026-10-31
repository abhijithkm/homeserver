import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { InstallCard } from '../Install'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function firePrompt(outcome: 'accepted' | 'dismissed') {
  const prompt = vi.fn().mockResolvedValue(undefined)
  const event = Object.assign(new Event('beforeinstallprompt', { cancelable: true }), { prompt, userChoice: Promise.resolve({ outcome }) })
  act(() => {
    window.dispatchEvent(event)
  })
  return { prompt, event }
}

describe('install card', () => {
  it('shows an Install app button once the browser allows installing, and confirms afterwards', async () => {
    render(<InstallCard />)
    expect(screen.queryByRole('button', { name: 'Install app' })).not.toBeInTheDocument()
    const { prompt, event } = firePrompt('accepted')
    expect(event.defaultPrevented).toBe(true) // the browser's own bar stays away
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Install app' }))
    expect(prompt).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('HomeServer is running as an app on this device.')).toBeInTheDocument()
  })

  it('explains the browser menu and why a full install needs HTTPS on a plain HTTP address', () => {
    render(<InstallCard />)
    expect(screen.getByText(/Install app or Add to Home screen/)).toBeInTheDocument()
    expect(screen.getByText(/plain HTTP, so Chrome and Edge add a shortcut/)).toBeInTheDocument()
  })

  it('gives the Safari steps on an iPhone', () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1')
    render(<InstallCard />)
    expect(screen.getByText(/tap the Share button, then choose Add to Home Screen/)).toBeInTheDocument()
  })

  it('says so when HomeServer already runs as an installed app', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('standalone'), media: q }))
    render(<InstallCard />)
    expect(screen.getByText('HomeServer is running as an app on this device.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Install app' })).not.toBeInTheDocument()
  })
})
