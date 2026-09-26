import * as tus from 'tus-js-client'
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

function errorMessage(e: Error | tus.DetailedError) {
  const body = (e as tus.DetailedError).originalResponse?.getBody()
  if (body) {
    try {
      const parsed = JSON.parse(body).error
      if (parsed) return parsed as string
    } catch {}
  }
  return e.message
}

function run({ item, file, endpoint, meta }: Job) {
  return new Promise<void>((resolve, reject) => {
    const metadata = Object.fromEntries(
      Object.entries({ ...meta, filename: file.name }).filter(([, v]) => v !== undefined && v !== ''),
    )
    const upload = new tus.Upload(file, {
      endpoint,
      metadata,
      chunkSize: 32 << 20,
      retryDelays: [0, 1000, 3000, 5000, 10000, 20000, 30000],
      storeFingerprintForResuming: true,
      removeFingerprintOnSuccess: true,
      fingerprint: () =>
        Promise.resolve(['tus', endpoint, meta.vol, meta.dir, meta.relativePath ?? '', file.name, file.size, file.lastModified].join('|')),
      onProgress: (sent) => (item.sent = sent),
      onSuccess: () => resolve(),
      onError: (e) => reject(new Error(errorMessage(e))),
    })
    item.ctl.signal.addEventListener(
      'abort',
      () => {
        upload.abort(true).catch(() => {})
        reject(new DOMException('aborted', 'AbortError'))
      },
      { once: true },
    )
    upload.findPreviousUploads().then((prev) => {
      const newest = prev.sort((a, b) => Date.parse(b.creationTime) - Date.parse(a.creationTime))[0]
      if (newest) upload.resumeFromPreviousUpload(newest)
      upload.start()
    })
  })
}

async function pump() {
  if (running) return
  running = true
  while (pending.length) {
    const j = pending.shift()!
    if (j.item.ctl.signal.aborted) continue
    j.item.state = 'uploading'
    try {
      await run(j)
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
