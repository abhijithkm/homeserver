import { useEffect, useState } from 'react'
import { Notice } from './ui'

interface InstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

function runsAsApp(): boolean {
  const standalone = typeof window.matchMedia === 'function' && window.matchMedia('(display-mode: standalone)').matches
  return standalone || (navigator as Navigator & { standalone?: boolean }).standalone === true
}

function isIos(): boolean {
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

/** Offers "Install app" where the browser allows it, and says what to do where it does not. */
export function InstallCard() {
  const [prompt, setPrompt] = useState<InstallPromptEvent | null>(null)
  const [installed, setInstalled] = useState(runsAsApp)

  useEffect(() => {
    const onPrompt = (e: Event) => {
      e.preventDefault() // keep the browser's own mini-bar away; we show our button
      setPrompt(e as InstallPromptEvent)
    }
    const onInstalled = () => {
      setPrompt(null)
      setInstalled(true)
    }
    window.addEventListener('beforeinstallprompt', onPrompt)
    window.addEventListener('appinstalled', onInstalled)
    return () => {
      window.removeEventListener('beforeinstallprompt', onPrompt)
      window.removeEventListener('appinstalled', onInstalled)
    }
  }, [])

  const install = async () => {
    if (!prompt) return
    await prompt.prompt()
    const choice = await prompt.userChoice
    setPrompt(null)
    if (choice.outcome === 'accepted') setInstalled(true)
  }

  return (
    <section className="card narrow-form" aria-labelledby="install-h">
      <h2 id="install-h">Install on this device</h2>
      {installed ? (
        <Notice kind="ok">HomeServer is running as an app on this device.</Notice>
      ) : prompt ? (
        <>
          <p>Add HomeServer to this device to open it like an app, without the browser&apos;s address bar.</p>
          <button type="button" className="btn btn-primary" onClick={() => void install()}>
            Install app
          </button>
        </>
      ) : isIos() ? (
        <p>In Safari, tap the Share button, then choose Add to Home Screen.</p>
      ) : (
        <>
          <p>Open your browser menu and choose Install app or Add to Home screen.</p>
          {!window.isSecureContext ? (
            <p className="hint">
              This address uses plain HTTP, so Chrome and Edge add a shortcut instead of a full app. A full install needs HTTPS; the administrator can turn it on under Settings.
            </p>
          ) : null}
        </>
      )}
    </section>
  )
}
