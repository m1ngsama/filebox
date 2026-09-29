import type * as tus from 'tus-js-client'
import { t } from './i18n'
import { errorText, session } from './api'
import { toast } from './toast.svelte'

export type Item = {
  id: number
  name: string
  dir: boolean
  total: number
  sent: number
  files: number
  ok: number
  state: 'queued' | 'uploading' | 'done' | 'error'
  error?: string
}

type Job = { item: Item; file: File; endpoint: string; meta: Record<string, string>; sent: number; ok: boolean; err: boolean }
type Group = { jobs: Job[]; ctl: AbortController; refresh: () => void }

const LIMIT = 3
const KEEP_FAILED = 200

export const uploads = $state<Item[]>([])
export const totals = $state({ files: 0, ok: 0, bytes: 0, sent: 0, speed: 0 })
const groups = new Map<number, Group>()
const pending: Job[] = []
const refreshers = new Set<() => void>()
let running = 0
let seq = 0
let uploaded = 0
let samples: [number, number][] = []
let ticker = 0
let debounce = 0

export function enqueue(files: { file: File; rel?: string }[], endpoint: string, meta: Record<string, string>, refresh: () => void) {
  const byTop = new Map<string, Item>()
  for (const { file, rel } of files) {
    const top = rel?.includes('/') ? rel.slice(0, rel.indexOf('/')) : ''
    let item = top ? byTop.get(top) : undefined
    if (!item) {
      uploads.push({ id: ++seq, name: top || file.name, dir: !!top, total: 0, sent: 0, files: 0, ok: 0, state: 'queued' })
      item = uploads[uploads.length - 1]
      groups.set(item.id, { jobs: [], ctl: new AbortController(), refresh })
      if (top) byTop.set(top, item)
    }
    item.total += file.size
    item.files++
    const job = { item, file, endpoint, meta: rel ? { ...meta, relativePath: rel } : meta, sent: 0, ok: false, err: false }
    groups.get(item.id)!.jobs.push(job)
    pending.push(job)
    totals.files++
    totals.bytes += file.size
  }
  pump()
}

function errorMessage(e: Error) {
  const res = (e as tus.DetailedError).originalResponse
  if (!res) return t.uploadFailed
  const known = errorText(res.getStatus())
  if (known) return known
  const body = res.getBody()?.trim() ?? ''
  try {
    const msg = JSON.parse(body).error
    if (msg) return msg as string
  } catch {}
  return `HTTP ${res.getStatus()} ${body.split('\n')[0]}`.trim()
}

function progress(job: Job, sent: number) {
  const d = sent - job.sent
  job.sent = sent
  job.item.sent += d
  totals.sent += d
}

async function run(job: Job, signal: AbortSignal) {
  const { file, endpoint, meta } = job
  const { Upload } = await import('tus-js-client')
  return new Promise<void>((resolve, reject) => {
    const metadata = Object.fromEntries(
      Object.entries({ ...meta, filename: file.name }).filter(([, v]) => v !== undefined && v !== ''),
    )
    const upload = new Upload(file, {
      endpoint,
      metadata,
      chunkSize: 32 << 20,
      retryDelays: [0, 1000, 3000, 5000, 10000, 20000, 30000],
      storeFingerprintForResuming: true,
      removeFingerprintOnSuccess: true,
      fingerprint: () =>
        Promise.resolve(['tus', endpoint, meta.vol, meta.dir, meta.relativePath ?? '', meta.overwrite ?? '', file.name, file.size, file.lastModified].join('|')),
      onProgress: (sent) => progress(job, sent),
      onSuccess: () => resolve(),
      onError: (e) => {
        if (endpoint === '/upload/' && (e as tus.DetailedError).originalResponse?.getStatus() === 401) session.lost()
        reject(new Error(errorMessage(e)))
      },
    })
    if (signal.aborted) return reject(new DOMException('aborted', 'AbortError'))
    signal.addEventListener(
      'abort',
      () => {
        upload.abort(true).catch(() => {})
        reject(new DOMException('aborted', 'AbortError'))
      },
      { once: true },
    )
    upload.findPreviousUploads().then((prev) => {
      const newest = prev.sort((a, b) => Date.parse(b.creationTime) - Date.parse(a.creationTime))[0]
      if (signal.aborted) return
      if (newest) upload.resumeFromPreviousUpload(newest)
      upload.start()
    })
  })
}

function settle(item: Item) {
  const g = groups.get(item.id)
  if (!g) return
  const busy = g.jobs.some((j) => !j.ok && !j.err)
  const failed = g.jobs.some((j) => j.err)
  item.state = busy ? (g.jobs.some((j) => j.sent > 0 || j.ok || j.err) ? 'uploading' : 'queued') : failed ? 'error' : 'done'
}

function fail(job: Job, message: string) {
  job.err = true
  job.item.error = message
  job.item.sent -= job.sent
  totals.sent -= job.sent
  totals.bytes -= job.file.size
  totals.files--
  job.sent = 0
}

async function work() {
  while (pending.length) {
    const job = pending.shift()!
    const g = groups.get(job.item.id)
    if (!g || job.ok || job.err) continue
    if (g.ctl.signal.aborted) {
      fail(job, t.cancelled)
      settle(job.item)
      continue
    }
    job.item.state = 'uploading'
    try {
      await run(job, g.ctl.signal)
      progress(job, job.file.size)
      job.ok = true
      job.item.ok++
      totals.ok++
      uploaded++
      refreshers.add(g.refresh)
      clearTimeout(debounce)
      debounce = setTimeout(flush, 600)
    } catch (e) {
      fail(job, g.ctl.signal.aborted ? t.cancelled : (e as Error).message)
    }
    settle(job.item)
  }
}

function flush() {
  for (const r of refreshers) r()
  refreshers.clear()
}

function sample() {
  const now = performance.now()
  samples.push([now, totals.sent])
  while (samples.length > 2 && now - samples[1][0] >= 5000) samples.shift()
  const [t0, b0] = samples[0]
  totals.speed = now - t0 > 500 ? Math.max(0, ((totals.sent - b0) * 1000) / (now - t0)) : 0
}

function pump() {
  if (!ticker) {
    samples = []
    sample()
    ticker = setInterval(sample, 1000)
  }
  while (running < LIMIT && pending.length) {
    running++
    work().finally(() => {
      running--
      if (!running && !pending.length) idle()
    })
  }
}

function idle() {
  clearInterval(ticker)
  ticker = 0
  clearTimeout(debounce)
  flush()
  for (let i = uploads.length - 1; i >= 0; i--) {
    const u = uploads[i]
    if (u.state !== 'done') continue
    groups.delete(u.id)
    uploads.splice(i, 1)
  }
  const failed = uploads.filter((u) => u.state === 'error')
  for (const u of failed.slice(0, Math.max(0, failed.length - KEEP_FAILED))) forget(u)
  Object.assign(totals, { files: 0, ok: 0, bytes: 0, sent: 0, speed: 0 })
  if (uploaded) toast(t.uploaded(uploaded))
  uploaded = 0
}

function forget(item: Item) {
  groups.delete(item.id)
  const i = uploads.indexOf(item)
  if (i >= 0) uploads.splice(i, 1)
}

export function cancel(item: Item) {
  const g = groups.get(item.id)
  if (!g) return
  g.ctl.abort()
  for (let i = pending.length - 1; i >= 0; i--) {
    if (pending[i].item !== item) continue
    fail(pending[i], t.cancelled)
    pending.splice(i, 1)
  }
  settle(item)
}

export function retry(item: Item) {
  const g = groups.get(item.id)
  if (!g) return
  g.ctl = new AbortController()
  item.error = undefined
  for (const j of g.jobs) {
    if (!j.err) continue
    j.err = false
    pending.push(j)
    totals.files++
    totals.bytes += j.file.size
  }
  settle(item)
  pump()
}

export function clearFailed() {
  for (const u of uploads.filter((u) => u.state === 'error')) forget(u)
}
