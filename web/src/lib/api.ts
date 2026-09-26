import { t } from './i18n'

export const errorText = (status: number): string | undefined =>
  t.errors[status] ?? (status >= 500 ? t.serverError : undefined)

export class HttpError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export type Entry = { name: string; dir: boolean; size: number; mtime: number }
export type Me = { name: string; vols: string[] }
export type Loc = { vol: string; path: string }
export type Share = { id: number; token: string; vol: string; path: string; mode: 'read' | 'upload' | 'drop'; has_password: boolean; expires: number; created: number; hits: number }
export type Token = { id: number; label: string; readonly: boolean; created: number; last_used: number }
export type TrashItem = { id: string; name: string; path: string; dir: boolean; size: number; deleted: number }
export type ShareInfo = { locked: true; mode: Share['mode'] } | { locked: false; mode: Share['mode']; name: string; dir: boolean; size?: number }

export const session = { lost: () => {} }

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
  const r = await fetch(url, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await r.text()
  if (r.status === 401 && url.startsWith('/api/') && url !== '/api/login') session.lost()
  if (!r.ok) {
    let msg = r.statusText
    try {
      msg = JSON.parse(text).error ?? msg
    } catch {}
    throw new HttpError(r.status, errorText(r.status) ?? msg)
  }
  return (text ? JSON.parse(text) : undefined) as T
}

export const enc = (p: string) => p.split('/').filter(Boolean).map(encodeURIComponent).join('/')
export const filesURL = (vol: string, path: string) => `/files/${encodeURIComponent(vol)}/${enc(path)}${path ? '/' : ''}`
export const rawURL = (vol: string, path: string, dl = false) => `/raw/${encodeURIComponent(vol)}/${enc(path)}${dl ? '?dl' : ''}`
export const thumbURL = (vol: string, path: string) => `/thumb/${encodeURIComponent(vol)}/${enc(path)}`
export const validShareToken = (tok: string) => /^[A-Za-z0-9_-]{22}$/.test(tok)
export const shareURL = (tok: string) => `/s/${encodeURIComponent(tok)}`
export const shareLink = (tok: string) => location.origin + shareURL(tok)
export const shareRawURL = (tok: string, path: string, dl = false) => `${shareURL(tok)}/raw/${enc(path)}${dl ? '?dl' : ''}`
export const shareThumbURL = (tok: string, path: string) => `${shareURL(tok)}/thumb/${enc(path)}`
const q = (o: Record<string, string>) => new URLSearchParams(o).toString()

export type JobStatus = { state: 'running' | 'done' | 'error'; code?: string; total: number; done: number }
const jobCodes: Record<string, number> = { exists: 409, notfound: 404, nospace: 507 }

export const api = {
  me: () => req<Me>('GET', '/api/me'),
  login: (name: string, password: string) => req<void>('POST', '/api/login', { name, password }),
  logout: () => req<void>('POST', '/api/logout'),
  ls: (vol: string, path: string) => req<{ entries: Entry[] }>('GET', `/api/ls?${q({ vol, path })}`),
  mkdir: (vol: string, path: string) => req<void>('POST', '/api/mkdir', { vol, path }),
  mv: (src: Loc, dst: Loc) => req<{ job: string } | undefined>('POST', '/api/mv', { src, dst }),
  cp: (src: Loc, dst: Loc) => req<{ job: string }>('POST', '/api/cp', { src, dst }),
  rm: (vol: string, paths: string[]) => req<void>('POST', '/api/rm', { vol, paths }),
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
  emptyTrash: (vol: string) => req<void>('POST', '/api/trash/empty', { vol }),
  tokens: () => req<{ tokens: Token[] }>('GET', '/api/tokens'),
  newToken: (label: string, readonly: boolean) => req<{ token: string }>('POST', '/api/tokens', { label, readonly }),
  delToken: (id: number) => req<void>('DELETE', `/api/tokens/${id}`),
  shares: () => req<{ shares: Share[] }>('GET', '/api/shares'),
  newShare: (s: { vol: string; path: string; mode: string; password: string; expires_in: number }) =>
    req<{ id: number; token: string }>('POST', '/api/shares', s),
  delShare: (id: number) => req<void>('DELETE', `/api/shares/${id}`),
  shareInfo: (tok: string) => req<ShareInfo>('GET', `${shareURL(tok)}/info`),
  unlock: (tok: string, password: string) => req<void>('POST', `${shareURL(tok)}/unlock`, { password }),
  shareLs: (tok: string, path: string) => req<{ entries: Entry[] }>('GET', `${shareURL(tok)}/ls?${q({ path })}`),
}
