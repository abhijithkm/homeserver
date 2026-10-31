// HomeServer service worker. It exists so browsers will offer a full app install over HTTPS.
// It caches nothing: an installed copy never shows stale pages and never keeps signed-in data on the
// device. Its only job is to replace the browser's error page with a plain explanation when the
// server cannot be reached.
const OFFLINE = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>HomeServer</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f2327;color:#e3eeec;font:16px/1.5 system-ui,sans-serif}main{max-width:26rem;padding:1.5rem}h1{font-size:1.4rem;margin:0 0 .5rem}p{color:#93aaa8}button{font:inherit;padding:.6rem 1.2rem;border:0;border-radius:10px;background:#3fcfb4;color:#05201a;cursor:pointer}</style></head><body><main><h1>HomeServer is not reachable</h1><p>Check that this device is on the same Wi-Fi or network cable as the computer that runs HomeServer, and that the computer is on.</p><button onclick="location.reload()">Try again</button></main></body></html>`

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))
self.addEventListener('fetch', (event) => {
  if (event.request.mode !== 'navigate') return
  event.respondWith(
    fetch(event.request).catch(
      () => new Response(OFFLINE, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } }),
    ),
  )
})
