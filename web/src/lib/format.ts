import { locale, t } from './i18n'

const decimals = [0, 1].map((d) => new Intl.NumberFormat(locale, { minimumFractionDigits: d, maximumFractionDigits: d, useGrouping: false }))

export function size(n: number): string {
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024
    i++
  }
  return `${decimals[i && n < 10 ? 1 : 0].format(n)} ${u[i]}`
}

const maxItems = 1000

export const amount = (e: { dir: boolean; size: number; items?: number }) =>
  e.dir ? (e.items == null ? '' : t.runCount(e.items, e.items >= maxItems)) : size(e.size)

export type Sort = 'name' | 'size' | 'mtime'
export const sorts: [Sort, string][] = [['name', t.name], ['size', t.size], ['mtime', t.mtime]]
export const flip = (sort: Sort, desc: boolean, k: Sort) => (sort === k ? !desc : k !== 'name')
const collator = new Intl.Collator(locale, { numeric: true })

type Row = { name: string; dir: boolean; size: number; mtime: number }
const keys = new WeakMap<Row[], { rank: Int32Array; lower: string[] }>()

function keysOf(list: Row[]) {
  let k = keys.get(list)
  if (!k) {
    const rank = new Int32Array(list.length)
    list
      .map((_, i) => i)
      // Dropping this presort makes the collator sort of 300k names about 4x slower.
      .sort((a, b) => (list[a].name < list[b].name ? -1 : list[a].name > list[b].name ? 1 : 0))
      .sort((a, b) => collator.compare(list[a].name, list[b].name))
      .forEach((i, r) => (rank[i] = r))
    k = { rank, lower: list.map((e) => e.name.toLowerCase()) }
    keys.set(list, k)
  }
  return k
}

export function arrange<T extends Row>(list: T[], filter: string, sort: Sort, desc: boolean) {
  const { rank, lower } = keysOf(list)
  const f = filter.toLowerCase()
  const d = desc ? -1 : 1
  const key = sort === 'size' ? (i: number) => list[i].size : sort === 'mtime' ? (i: number) => list[i].mtime : (i: number) => rank[i]
  const idx: number[] = []
  for (let i = 0; i < list.length; i++) if (lower[i].includes(f)) idx.push(i)
  return idx
    .sort((a, b) => (list[a].dir !== list[b].dir ? (list[a].dir ? -1 : 1) : d * (key(a) - key(b)) || rank[a] - rank[b]))
    .map((i) => list[i])
}

const dtf = [new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }), new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'medium' })]
export const date = (ms: number, seconds = false) => dtf[+seconds].format(ms)

const moment = new Intl.DateTimeFormat(locale, { dateStyle: 'long', timeStyle: 'short' })
export const when = (ms: number) => moment.format(ms)

export function clock(sec: number) {
  const s = Math.round(sec)
  const [h, m] = [Math.floor(s / 3600), Math.floor((s % 3600) / 60)]
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

export type Meta = Partial<Record<keyof typeof t.meta, string>> & { width?: number; height?: number }

export function metaRows(m: Meta) {
  const r: [string, string][] = []
  if (m.width && m.height) r.push([t.meta.dimensions, `${m.width} × ${m.height}`])
  for (const k of ['taken', 'camera', 'lens', 'focal', 'aperture', 'shutter', 'iso', 'gps'] as const) {
    const v = m[k]
    const at = k === 'taken' && v ? new Date(v.replace(' ', 'T')).getTime() : NaN
    if (v) r.push([t.meta[k], Number.isNaN(at) ? v : date(at)])
  }
  return r
}

const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
const steps: [Intl.RelativeTimeFormatUnit, number][] = [['minute', 60], ['hour', 24], ['day', 30], ['month', 12], ['year', Infinity]]

export function ago(ms: number) {
  let v = (ms - Date.now()) / 60000
  if (Math.abs(v) < 1) return t.justNow
  for (const [unit, n] of steps) {
    if (Math.abs(v) < n) return rtf.format(Math.round(v), unit)
    v /= n
  }
  return ''
}

const browsers: [RegExp, string][] = [[/Edg/, 'Edge'], [/OPR\//, 'Opera'], [/Firefox\/|FxiOS/, 'Firefox'], [/Chrome\/|CriOS/, 'Chrome'], [/Safari\//, 'Safari']]
const systems: [RegExp, string][] = [[/iPhone/, 'iOS'], [/iPad/, 'iPadOS'], [/Android/, 'Android'], [/Mac OS X/, 'macOS'], [/Windows/, 'Windows'], [/CrOS/, 'ChromeOS'], [/Linux/, 'Linux']]
const pick = (ua: string, l: [RegExp, string][]) => l.find(([r]) => r.test(ua))?.[1]

export const device = (ua: string) => [pick(ua, browsers), pick(ua, systems)].filter(Boolean).join(' · ') || ua

const ext = (n: string) => (n.includes('.') ? n.slice(n.lastIndexOf('.') + 1).toLowerCase() : '')

const groups: Record<string, string> = {
  image: 'jpg jpeg png gif webp avif bmp svg ico heic heif tif tiff',
  video: 'mp4 m4v webm mov mkv ogv',
  audio: 'mp3 m4a aac flac wav ogg opus',
  pdf: 'pdf',
  book: 'epub',
  comic: 'cbz',
  text: 'txt md markdown log json yaml yml toml ini conf cfg csv tsv go js ts py rs c h cpp java kt sh fish zsh srt ass vtt xml html css sql',
}
const kinds = new Map(Object.entries(groups).flatMap(([k, v]) => v.split(' ').map((e) => [e, k] as const)))

export const kind = (n: string) => (kinds.get(ext(n)) ?? '') as 'image' | 'video' | 'audio' | 'pdf' | 'book' | 'comic' | 'text' | ''

const looks: Record<string, string> = {
  code: 'go js ts py rs c h cpp java kt sh fish zsh sql html css json yaml yml toml xml',
  archive: 'zip tar gz tgz bz2 xz zst 7z rar',
  sheet: 'csv tsv xls xlsx ods numbers',
}
const icons = new Map(Object.entries(looks).flatMap(([k, v]) => v.split(' ').map((e) => [e, k] as const)))

export type Look = ReturnType<typeof kind> | 'code' | 'archive' | 'sheet'
export const look = (n: string) => (icons.get(ext(n)) ?? kind(n)) as Look

export const visual = (n: string) => /^(image|video)$/.test(kind(n))

export const stem = (n: string) => (n.lastIndexOf('.') > 0 ? n.slice(0, n.lastIndexOf('.')) : n)

const playable = /\.(srt|vtt)$/i
const nfcStem = (n: string) => stem(n.normalize('NFC'))

export const subtitleOf = (video: string, name: string) => {
  const v = nfcStem(video)
  const s = nfcStem(name)
  return playable.test(name) && (s === v || s.startsWith(`${v}.`))
}

export const subtitleLang = (video: string, name: string) => nfcStem(name).slice(nfcStem(video).length + 1)

export const subtitleRename = (video: string, to: string, name: string) => stem(to) + name.normalize('NFC').slice(nfcStem(video).length)

export function sidecars(list: Row[]) {
  const videos = new Map<string, string[]>()
  for (const e of list) if (!e.dir && kind(e.name) === 'video') videos.set(nfcStem(e.name), [...(videos.get(nfcStem(e.name)) ?? []), e.name])
  const of = new Map<string, string[]>()
  if (!videos.size) return of
  for (const e of list) {
    if (e.dir || !playable.test(e.name)) continue
    for (let s = nfcStem(e.name), prev = ''; s !== prev; prev = s, s = stem(s))
      for (const v of videos.get(s) ?? []) of.set(v, [...(of.get(v) ?? []), e.name])
  }
  return of
}

const mostly = (es: Row[], f: (n: string) => boolean) => es.length > 0 && es.filter((e) => !e.dir && f(e.name)).length >= 0.6 * es.length
const bound = (n: string) => /^(pdf|epub|cbz)$/.test(ext(n))
export const mostlyMedia = (es: Row[]) => mostly(es, visual)
export const mostlyBooks = (es: Row[]) => mostly(es, bound)
export const prefersGrid = (es: Row[]) => mostly(es, (n) => visual(n) || bound(n))

const graphemes = new Intl.Segmenter(locale, { granularity: 'grapheme' })
const split = (s: string) => Array.from(graphemes.segment(s), (x) => x.segment)

export function ends(n: string): [string, string] {
  const c = split(n)
  const x = /(\.tar)?\.[^.]+$/i.exec(n)
  const e = x && x.index > 0 ? split(x[0]).length : 0
  const k = Math.min(c.length - 1, e + 4)
  return k > 0 && (e || c.length > 4) ? [c.slice(0, -k).join(''), c.slice(-k).join('')] : [n, '']
}

export const parent = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '')
export const base = (p: string) => p.slice(p.lastIndexOf('/') + 1)
export const child = (dir: string, n: string) => (dir ? `${dir}/${n}` : n)

export const fallback = (urls: (string | null | false | undefined)[], failures: number) => urls.filter(Boolean)[failures] || null

export const rawThumb = (e: { name: string; size: number; dir: boolean }) => !e.dir && kind(e.name) === 'image' && !converted(e.name) && e.size < 2 << 20

const thumbs = new Set('jpg jpeg png gif webp bmp tif tiff heic heif avif pdf cr2 cr3 nef arw dng mp4 m4v mkv mov avi webm ts flv wmv mpg mpeg epub cbz mp3 m4a aac flac ogg opus wav'.split(' '))
export const thumbable = (n: string) => thumbs.has(ext(n))

const foreign = new Set(['heic', 'heif', 'tif', 'tiff'])
const safari = /^((?!chrome|chromium|android|crios|fxios|edg).)*safari/i.test(navigator.userAgent)
export const converted = (n: string) => !safari && foreign.has(ext(n))

type WeekLocale = Intl.Locale & { getWeekInfo?: () => { firstDay: number }; weekInfo?: { firstDay: number } }
const week = new Intl.Locale(locale) as WeekLocale
const weekStart = ((week.getWeekInfo?.() ?? week.weekInfo)?.firstDay ?? 1) % 7

export function days(now = new Date()) {
  const d = new Date(now)
  d.setHours(0, 0, 0, 0)
  const today = d.getTime()
  d.setDate(d.getDate() - 1)
  const yesterday = d.getTime()
  d.setTime(today)
  d.setDate(d.getDate() - ((now.getDay() - weekStart + 7) % 7))
  const week = d.getTime()
  return (ms: number) => (ms >= today ? t.today : ms >= yesterday ? t.yesterday : ms >= week ? t.thisWeek : t.earlier)
}

export const SEP = '\u00a0› '
export function place(vol: string, path = '', max = Infinity) {
  const segs = [vol, ...path.split('/').filter((s) => s && s !== '.')]
  const shown = segs.length > max ? [segs[0], '…', ...segs.slice(2 - max)] : segs
  return shown.map((s) => `\u2068${s}\u2069`).join(SEP)
}
export const placeOf = (target: string) => {
  const i = target.indexOf(':/')
  return i < 0 ? target : place(target.slice(0, i), target.slice(i + 2))
}

export const lapsed = (s: { expires: number }, now = Date.now()) => !!s.expires && s.expires * 1000 <= now
export const byLapse = <T extends { expires: number }>(xs: T[]) => [...xs].sort((a, b) => +lapsed(a) - +lapsed(b))

export const shareSummary = (s: { mode: 'read' | 'upload' | 'drop'; expires: number; views: number; has_password: boolean }, now = Date.now()) =>
  [
    t.modes[s.mode],
    !s.expires ? t.forever : lapsed(s, now) ? t.expired : t.expiresIn(s.expires - now / 1000),
    t.visits(s.views),
    s.has_password && t.hasPassword,
  ].filter((x) => typeof x === 'string')
