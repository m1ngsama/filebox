import { tusUpload } from './tus'
import { t } from './i18n'

export type Item = {
  id: number
  name: string
  total: number
  sent: number
  state: 'queued' | 'uploading' | 'done' | 'error'
  error?: string
  ctl: AbortController
}

type Job = { item: Item; file: File; endpoint: string; meta: Record<string, string>; done: () => void }

export const uploads = $state<Item[]>([])
const pending: Job[] = []
let running = false
let seq = 0

export function enqueue(files: { file: File; rel?: string }[], endpoint: string, meta: Record<string, string>, done: () => void) {
  for (const { file, rel } of files) {
    uploads.push({ id: ++seq, name: rel || file.name, total: file.size, sent: 0, state: 'queued', ctl: new AbortController() })
    const item = uploads[uploads.length - 1]
    pending.push({ item, file, endpoint, meta: rel ? { ...meta, relativePath: rel } : meta, done })
  }
  pump()
}

async function pump() {
  if (running) return
  running = true
  while (pending.length) {
    const j = pending.shift()!
    if (j.item.ctl.signal.aborted) continue
    j.item.state = 'uploading'
    try {
      await tusUpload(j.endpoint, j.file, j.meta, (n) => (j.item.sent = n), j.item.ctl.signal)
      j.item.sent = j.item.total
      j.item.state = 'done'
      j.done()
    } catch (e) {
      j.item.state = 'error'
      j.item.error = j.item.ctl.signal.aborted ? t.cancelled : (e as Error).message
    }
  }
  running = false
}

export function cancel(item: Item) {
  item.ctl.abort()
  if (item.state === 'queued') {
    item.state = 'error'
    item.error = t.cancelled
  }
}

export function clearDone() {
  for (let i = uploads.length - 1; i >= 0; i--) if (uploads[i].state === 'done' || uploads[i].state === 'error') uploads.splice(i, 1)
}
