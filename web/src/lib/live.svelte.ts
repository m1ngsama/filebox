import type { Notice } from './api'

// The server pushes what changed over one EventSource per tab; screens subscribe to the parts they show.
export const live = $state({ unread: 0, connected: false })

type FS = (vol: string, dir: string) => void
const folders = new Set<FS>()
const reconnects = new Set<() => void>()
const notices = new Set<(n: Notice) => void>()
const talk = new Set<(vol: string, path: string) => void>()
let source: EventSource | null = null
let dropped = false

export function connect() {
  if (source || typeof EventSource === 'undefined') return
  const es = (source = new EventSource('/api/events'))
  es.addEventListener('fs', (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { vol: string; dir?: string }
    for (const f of folders) f(d.vol, d.dir ?? '')
  })
  es.addEventListener('comment', (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { vol: string; dir?: string }
    for (const f of talk) f(d.vol, d.dir ?? '')
  })
  es.addEventListener('unread', (e) => (live.unread = JSON.parse((e as MessageEvent).data).unread ?? 0))
  es.addEventListener('notice', (e) => {
    const n = JSON.parse((e as MessageEvent).data).notice as Notice
    for (const f of notices) f(n)
  })
  es.onopen = () => {
    live.connected = true
    // Whatever changed while the stream was down was missed, so the screens reload what they show.
    if (dropped) for (const f of reconnects) f()
    dropped = false
  }
  es.onerror = () => {
    live.connected = false
    dropped = true
  }
}

export function disconnect() {
  source?.close()
  source = null
  live.connected = false
  live.unread = 0
}

const sub = <T>(set: Set<T>, f: T) => (set.add(f), () => void set.delete(f))
export const onFolder = (f: FS) => sub(folders, f)
export const onReconnect = (f: () => void) => sub(reconnects, f)
export const onNotice = (f: (n: Notice) => void) => sub(notices, f)
export const onComment = (f: (vol: string, path: string) => void) => sub(talk, f)
