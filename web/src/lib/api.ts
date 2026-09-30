import type { PublicKeyCredentialCreationOptionsJSON, PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { t } from './i18n'
import { child, parent, base } from './format'

export const errorText = (status: number): string | undefined =>
  (t.errors as Record<number, string>)[status] ?? (status >= 500 ? t.serverError : undefined)

export class HttpError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export type Entry = { name: string; dir: boolean; size: number; mtime: number }
export type Me = { name: string; vols: string[] }
export type RecentFile = Entry & Loc
export type Favorite = RecentFile & { missing: boolean }
export type Usage = { name: string; fs?: string; used: number; free: number; total: number }
export type Loc = { vol: string; path: string }
export type ContentHit = Loc & { name: string; size: number; mtime: number; snippet: string[] }
export type Progress = { done: number; total: number }
export type Move = { from: Loc; to: Loc }
export type Share = { id: number; token: string; vol: string; path: string; mode: 'read' | 'upload' | 'drop'; has_password: boolean; expires: number; created: number; hits: number; views: number; note: string; max_upload: number; dir: boolean }
export type ShareEdit = Partial<{ mode: Share['mode']; password: string; expires_in: number; note: string; max_upload: number }>
export type Activity = { id: number; at: number; kind: string; share_id: number; target: string; visitor: string; name: string; size: number }
export type Token = { id: number; label: string; readonly: boolean; created: number; last_used: number }
export type Session = { id: number; user_agent: string; ip: string; created: number; last_used: number; current: boolean }
export type Passkey = { id: number; name: string; created: number; last_used: number }
type Begun<T> = { ceremony: string; options: T }
export type Version = { id: string; size: number; mtime: number; created: number; source: string }
export type TrashItem = { id: string; name: string; path: string; dir: boolean; size: number; deleted: number }
export type ShareInfo =
  | { locked: true; mode: Share['mode'] }
  | { locked: false; mode: Share['mode']; name: string; dir: boolean; size?: number; note: string; expires: number; max_upload: number }

export const session = { lost: () => {} }

async function req<T>(method: string, url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  try {
    const r = await fetch(url, {
      method,
      signal,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    return await settle<T>(r, url)
  } catch (e) {
    throw (e as Error).name === 'TimeoutError' ? new HttpError(0, t.timedOut) : e
  }
}

async function settle<T>(r: Response, url: string): Promise<T> {
  const text = await r.text()
  if (r.status === 401 && url.startsWith('/api/') && url !== '/api/login') session.lost()
  if (!r.ok) {
    let body: { error?: string; retry_after?: number } = {}
    try {
      body = JSON.parse(text)
    } catch {}
    const wait = body.retry_after
    const limited = wait && (body.error === 'login paused' ? t.loginPaused(wait) : t.tooMany(wait))
    throw new HttpError(r.status, limited || errorText(r.status) || body.error || r.statusText)
  }
  return (text ? JSON.parse(text) : undefined) as T
}

let early: { url: string; res: Promise<Response>; stop: AbortController } | undefined

export function prefetchLs(vol: string, path: string) {
  const url = lsURL(vol, path)
  const stop = new AbortController()
  const res = fetch(url, { signal: stop.signal })
  res.catch((e) => {
    if (e?.name !== 'AbortError') throw e
  })
  early = { url, stop, res }
}

export function dropPrefetch() {
  early?.stop.abort()
  early = undefined
}

function take(url: string, signal?: AbortSignal) {
  const e = early
  early = undefined
  if (e?.url !== url) {
    e?.stop.abort()
    return fetch(url, { signal })
  }
  if (signal?.aborted) e.stop.abort()
  signal?.addEventListener('abort', () => e.stop.abort(), { once: true })
  return e.res
}

async function list(url: string, signal?: AbortSignal, onchunk?: (entries: Entry[]) => void): Promise<Entry[]> {
  const r = await take(url, signal)
  if (!r.ok || !r.body || !r.headers.get('Content-Type')?.includes('ndjson')) return (await settle<{ entries: Entry[] }>(r, url)).entries
  const out: Entry[] = []
  const rd = r.body.pipeThrough(new TextDecoderStream()).getReader()
  let buf = ''
  try {
    for (;;) {
      const { value, done } = await rd.read()
      if (done) {
        if (buf.trim()) throw new HttpError(500, t.serverError)
        return out
      }
      const lines = (buf + value).split('\n')
      buf = lines.pop()!
      for (const l of lines) {
        const m: { entries?: Entry[]; error?: string } = JSON.parse(l)
        if (m.error || !m.entries) throw new HttpError(500, t.serverError)
        for (const e of m.entries) out.push(e)
      }
      if (lines.length) onchunk?.(out)
    }
  } catch (e) {
    rd.cancel().catch(() => {})
    throw e instanceof SyntaxError ? new HttpError(500, t.serverError) : e
  }
}

export const enc = (p: string) => p.split('/').filter(Boolean).map(encodeURIComponent).join('/')
export const filesURL = (vol: string, path: string) => `/files/${encodeURIComponent(vol)}/${enc(path)}${path ? '/' : ''}`
export const selectURL = (vol: string, path: string) => `${filesURL(vol, parent(path))}?select=${encodeURIComponent(base(path))}`
export const rawURL = (vol: string, path: string, dl = false) => `/raw/${encodeURIComponent(vol)}/${enc(path)}${dl ? '?dl' : ''}`
export const thumbURL = (vol: string, path: string) => `/thumb/${encodeURIComponent(vol)}/${enc(path)}`
export const validShareToken = (tok: string) => /^[A-Za-z0-9_-]{22}$/.test(tok)
export const shareURL = (tok: string) => `/s/${encodeURIComponent(tok)}`
export const shareLink = (tok: string) => location.origin + shareURL(tok)
export const shareRawURL = (tok: string, path: string, dl = false) => `${shareURL(tok)}/raw/${enc(path)}${dl ? '?dl' : ''}`
export const shareThumbURL = (tok: string, path: string) => `${shareURL(tok)}/thumb/${enc(path)}`
const q = (o: Record<string, string>) => new URLSearchParams(o).toString()
const lsURL = (vol: string, path: string) => `/api/ls?${q({ vol, path })}`
export type As = 'dl' | 'thumb' | 'render' | 'meta'
export type Src = (e: Entry, as?: As) => string
export const fileURL = (vol: string, p: string, as?: As) =>
  as === 'thumb' ? thumbURL(vol, p) : as === 'render' || as === 'meta' ? `/api/${as}?${q({ vol, p })}` : rawURL(vol, p, as === 'dl')
export const shareFileURL = (tok: string, p: string, as?: As) =>
  as === 'thumb' ? shareThumbURL(tok, p) : as === 'render' || as === 'meta' ? `${shareURL(tok)}/${as}?${q({ p })}` : shareRawURL(tok, p, as === 'dl')
export const versionURL = (vol: string, id: string, dl = false) => `/api/versions/raw?${q({ vol, id })}${dl ? '&dl' : ''}`
const zipQuery = (paths: string[], name: string) => new URLSearchParams([...paths.map((p) => ['p', p]), ['name', name]]).toString()
export const zipURL = (vol: string, paths: string[], name: string) => `/api/zip?vol=${encodeURIComponent(vol)}&${zipQuery(paths, name)}`
export const shareZipURL = (tok: string, paths: string[], name: string) => `${shareURL(tok)}/zip?${zipQuery(paths, name)}`
export function saveURL(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = ''
  a.click()
}

export type JobStatus = { state: 'running' | 'done' | 'error'; code?: string; total: number; done: number }
const jobCodes: Record<string, number> = { exists: 409, notfound: 404, nospace: 507 }

export const api = {
  me: () => req<Me>('GET', '/api/me'),
  loginInfo: () => req<{ single: boolean }>('GET', '/api/login'),
  login: (name: string, password: string) => req<void>('POST', '/api/login', { name, password }),
  logout: () => req<void>('POST', '/api/logout'),
  ls: (vol: string, path: string, signal?: AbortSignal, onchunk?: (entries: Entry[]) => void) => list(lsURL(vol, path), signal, onchunk),
  search: (q: string, signal: AbortSignal) =>
    req<{ entries: RecentFile[]; content: ContentHit[]; indexing: Progress | null; scanning: boolean }>(
      'GET',
      `/api/search?${new URLSearchParams({ q })}`,
      undefined,
      signal,
    ),
  vols: () => req<{ vols: Usage[] }>('GET', '/api/vols'),
  exists: (vol: string, path: string) =>
    fetch(lsURL(vol, path)).then(
      (r) => (r.body?.cancel(), r.ok),
      () => false,
    ),
  size: (vol: string, path: string) => req<{ size: number; files: number; scanning: boolean }>('GET', `/api/size?${q({ vol, path })}`),
  favorites: () => req<{ entries: Favorite[] }>('GET', '/api/favorites'),
  star: (vol: string, paths: string[], star: boolean) => req<void>('POST', '/api/favorites', { vol, paths, star }),
  recent: () => req<{ entries: Omit<RecentFile, 'dir'>[]; scanning: boolean }>('GET', '/api/recent'),
  mkdir: (vol: string, path: string, signal?: AbortSignal) => req<Entry>('POST', '/api/mkdir', { vol, path }, signal),
  mv: (src: Loc, dst: Loc, signal?: AbortSignal) => req<{ job: string } | undefined>('POST', '/api/mv', { src, dst }, signal),
  cp: (src: Loc, dst: Loc) => req<{ job: string }>('POST', '/api/cp', { src, dst }),
  rm: (vol: string, paths: string[], signal?: AbortSignal) =>
    req<{ trashed: { path: string; id: string }[]; failed: { path: string; status: number; error: string }[] }>('POST', '/api/rm', { vol, paths }, signal),
  async move(from: Loc, to: Loc, onprogress?: (s: JobStatus) => void) {
    const r = await api.mv(from, to)
    if (r?.job) await api.waitJob(r.job, onprogress)
  },
  async transfer(vol: string, dir: string, names: string[], to: Loc, copy: boolean, onstatus?: (i: number, name: string, s?: JobStatus) => void) {
    const done: Move[] = []
    try {
      for (const [i, n] of names.entries()) {
        const show = (s?: JobStatus) => onstatus?.(i, n, s)
        show()
        const m = { from: { vol, path: child(dir, n) }, to: { vol: to.vol, path: child(to.path, n) } }
        if (copy) await api.waitJob((await api.cp(m.from, m.to)).job, show)
        else await api.move(m.from, m.to, show)
        done.push(m)
      }
      return { done }
    } catch (e) {
      return { done, error: e as Error }
    }
  },
  async waitJob(id: string, onprogress?: (s: JobStatus) => void) {
    for (;;) {
      const s = await req<JobStatus>('GET', `/api/jobs/${id}`)
      onprogress?.(s)
      if (s.state === 'error') throw new Error(errorText(jobCodes[s.code ?? ''] ?? 500))
      if (s.state === 'done') return
      await new Promise((r) => setTimeout(r, 1000))
    }
  },
  trash: (vol: string) => req<{ items: TrashItem[] }>('GET', `/api/trash?${q({ vol })}`),
  restore: (vol: string, id: string) => req<void>('POST', '/api/trash/restore', { vol, id }),
  restoreMany: (vol: string, ids: string[]) => req<{ failed: { id: string; status: number; error: string }[] }>('POST', '/api/trash/restore', { vol, ids }),
  emptyTrash: (vol: string) => req<void>('POST', '/api/trash/empty', { vol }),
  purge: (vol: string, ids: string[]) => req<void>('POST', '/api/trash/delete', { vol, ids }),
  versions: (vol: string, p: string) => req<{ versions: Version[] }>('GET', `/api/versions?${q({ vol, p })}`),
  restoreVersion: (vol: string, id: string) => req<{ path: string; prev: string }>('POST', '/api/versions/restore', { vol, id }),
  delVersion: (vol: string, id: string) => req<void>('POST', '/api/versions/delete', { vol, id }),
  tokens: () => req<{ tokens: Token[] }>('GET', '/api/tokens'),
  newToken: (label: string, readonly: boolean) => req<{ token: string }>('POST', '/api/tokens', { label, readonly }),
  delToken: (id: number) => req<void>('DELETE', `/api/tokens/${id}`),
  sessions: () => req<{ sessions: Session[] }>('GET', '/api/sessions'),
  delSession: (id: number) => req<void>('DELETE', `/api/sessions/${id}`),
  revokeOtherSessions: () => req<void>('POST', '/api/sessions/revoke-others'),
  passkeysEnabled: () => req<{ enabled: boolean }>('GET', '/api/passkeys/enabled'),
  passkeys: () => req<{ passkeys: Passkey[] }>('GET', '/api/passkeys'),
  passkeyRegisterBegin: () => req<Begun<PublicKeyCredentialCreationOptionsJSON>>('POST', '/api/passkeys/register/begin'),
  passkeyRegisterFinish: (ceremony: string, name: string, response: unknown) =>
    req<{ id: number }>('POST', '/api/passkeys/register/finish', { ceremony, name, response }),
  passkeyLoginBegin: () => req<Begun<PublicKeyCredentialRequestOptionsJSON>>('POST', '/api/passkeys/login/begin'),
  passkeyLoginFinish: (ceremony: string, response: unknown) => req<void>('POST', '/api/passkeys/login/finish', { ceremony, response }),
  renamePasskey: (id: number, name: string) => req<void>('PATCH', `/api/passkeys/${id}`, { name }),
  delPasskey: (id: number) => req<void>('DELETE', `/api/passkeys/${id}`),
  shares: () => req<{ shares: Share[] }>('GET', '/api/shares'),
  newShare: (s: { vol: string; path: string; mode: string; password: string; expires_in: number }) =>
    req<{ id: number; token: string; existing?: boolean }>('POST', '/api/shares', s),
  editShare: (id: number, s: ShareEdit) => req<void>('PATCH', `/api/shares/${id}`, s),
  delShare: (id: number) => req<void>('DELETE', `/api/shares/${id}`),
  activity: (o: { kind: string; share: string; before: string }) =>
    req<{ events: Activity[]; more: boolean }>('GET', `/api/activity?${q(Object.fromEntries(Object.entries(o).filter(([, v]) => v)))}`),
  shareInfo: (tok: string) => req<ShareInfo>('GET', `${shareURL(tok)}/info`),
  unlock: (tok: string, password: string) => req<void>('POST', `${shareURL(tok)}/unlock`, { password }),
  shareLs: (tok: string, path: string, signal?: AbortSignal) => list(`${shareURL(tok)}/ls?${q({ path })}`, signal),
}
