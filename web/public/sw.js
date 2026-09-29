const SHELL = 'shell'
const STASH = 'share-target'
const MAX_FILES = 50
const BYPASS = /^\/(api|s|dav|raw|thumb|upload)(\/|$)/

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(Promise.all([caches.delete(STASH), self.clients.claim()])))

self.addEventListener('fetch', (e) => {
  const r = e.request
  const u = new URL(r.url)
  if (u.origin !== location.origin) return
  if (r.method === 'POST' && u.pathname === '/share-target') return e.respondWith(receive(r))
  if (r.method !== 'GET' || BYPASS.test(u.pathname)) return
  if (u.pathname.startsWith('/assets/')) e.respondWith(asset(r))
  else if (r.mode === 'navigate') e.respondWith(page(r))
})

async function asset(r) {
  const c = await caches.open(SHELL)
  const hit = await c.match(r)
  if (hit) return hit
  const res = await fetch(r)
  if (res.ok && !res.headers.get('Content-Type')?.startsWith('text/html')) await c.put(r, res.clone())
  return res
}

async function page(r) {
  try {
    const res = await fetch(r)
    if (res.ok && res.headers.get('Content-Type')?.startsWith('text/html')) {
      const c = await caches.open(SHELL)
      const html = await res.clone().text()
      const old = await c.match('/')
      if (!old || (await old.text()) !== html) for (const k of await c.keys()) await c.delete(k)
      await c.put('/', new Response(html, { headers: res.headers }))
    }
    return res
  } catch {
    return (await caches.match('/')) ?? Response.error()
  }
}

async function room() {
  try {
    const { quota, usage = 0 } = await navigator.storage.estimate()
    return quota ? (quota - usage) / 2 : Infinity
  } catch {
    return Infinity
  }
}

async function receive(r) {
  await caches.delete(STASH)
  const back = (status) => Response.redirect(`/?share-target${status ? `=${status}` : ''}`, 303)
  const limit = await room()
  if (Number(r.headers.get('Content-Length')) > limit) return back('too-large')
  let files
  try {
    files = (await r.formData()).getAll('files').filter((f) => f instanceof File)
  } catch {
    return back('failed')
  }
  if (files.length > MAX_FILES || files.reduce((n, f) => n + f.size, 0) > limit) return back('too-large')
  const c = await caches.open(STASH)
  const at = String(Date.now())
  for (const [i, f] of files.entries()) {
    const headers = { 'Content-Type': f.type || 'application/octet-stream', 'X-Name': encodeURIComponent(f.name), 'X-Modified': String(f.lastModified), 'X-At': at }
    await c.put(`/share-target/${i}`, new Response(f, { headers }))
  }
  return back('')
}
