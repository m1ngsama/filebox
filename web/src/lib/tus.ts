const CHUNK = 32 << 20
const H = { 'Tus-Resumable': '1.0.0' }

const b64 = (s: string) => btoa(Array.from(new TextEncoder().encode(s), (c) => String.fromCharCode(c)).join(''))
const store = {
  get: (k: string) => { try { return localStorage.getItem(k) } catch { return null } },
  set: (k: string, v: string) => { try { localStorage.setItem(k, v) } catch {} },
  del: (k: string) => { try { localStorage.removeItem(k) } catch {} },
}

class NetError extends Error {}

async function fail(r: Response): Promise<never> {
  let msg = `${r.status}`
  try {
    msg = (await r.json()).error ?? msg
  } catch {}
  throw new Error(msg)
}

async function head(loc: string, signal: AbortSignal): Promise<number | null> {
  const r = await fetch(loc, { method: 'HEAD', headers: H, signal })
  return r.ok ? Number(r.headers.get('Upload-Offset')) : null
}

function patch(loc: string, offset: number, chunk: Blob, progress: (n: number) => void, signal: AbortSignal) {
  return new Promise<number>((resolve, reject) => {
    const x = new XMLHttpRequest()
    x.open('PATCH', loc)
    x.setRequestHeader('Tus-Resumable', '1.0.0')
    x.setRequestHeader('Upload-Offset', String(offset))
    x.setRequestHeader('Content-Type', 'application/offset+octet-stream')
    x.upload.onprogress = (e) => progress(offset + e.loaded)
    x.onload = () => {
      if (x.status === 204 || x.status === 409) resolve(Number(x.getResponseHeader('Upload-Offset')))
      else {
        let msg = String(x.status)
        try {
          msg = JSON.parse(x.responseText).error ?? msg
        } catch {}
        reject(new Error(msg))
      }
    }
    x.onerror = () => reject(new NetError('network'))
    x.onabort = () => reject(new DOMException('aborted', 'AbortError'))
    signal.addEventListener('abort', () => x.abort(), { once: true })
    x.send(chunk)
  })
}

export async function tusUpload(
  endpoint: string,
  file: File,
  meta: Record<string, string>,
  progress: (sent: number) => void,
  signal: AbortSignal,
) {
  const key = ['tus', endpoint, file.name, file.size, file.lastModified, meta.vol, meta.dir, meta.relativePath].join('|')
  let loc = store.get(key)
  let offset = 0
  if (loc) {
    const o = await head(loc, signal)
    if (o === null) {
      store.del(key)
      loc = null
    } else offset = o
  }
  if (!loc) {
    const md = Object.entries({ ...meta, filename: file.name })
      .filter(([, v]) => v !== undefined && v !== '')
      .map(([k, v]) => `${k} ${b64(v)}`)
      .join(',')
    const r = await fetch(endpoint, {
      method: 'POST',
      headers: { ...H, 'Upload-Length': String(file.size), 'Upload-Metadata': md },
      signal,
    })
    if (!r.ok) await fail(r)
    loc = r.headers.get('Location')!
    if (file.size === 0) return
    store.set(key, loc)
  }
  progress(offset)
  let retries = 0
  while (offset < file.size) {
    try {
      offset = await patch(loc, offset, file.slice(offset, offset + CHUNK), progress, signal)
      retries = 0
    } catch (e) {
      if (!(e instanceof NetError) || retries >= 6) throw e
      await new Promise((r) => setTimeout(r, 1000 * 2 ** retries++))
      offset = (await head(loc, signal)) ?? 0
    }
  }
  store.del(key)
}
