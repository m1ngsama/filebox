export function size(n: number): string {
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024
    i++
  }
  return `${i ? n.toFixed(n < 10 ? 1 : 0) : n} ${u[i]}`
}

export type Sort = 'name' | 'size' | 'mtime'
const collator = new Intl.Collator('zh-CN', { numeric: true })

export function arrange<T extends { name: string; dir: boolean; size: number; mtime: number }>(list: T[], filter: string, sort: Sort, desc: boolean) {
  const f = filter.toLowerCase()
  const d = desc ? -1 : 1
  const cmp = (a: T, b: T) => (sort === 'size' ? a.size - b.size : sort === 'mtime' ? a.mtime - b.mtime : collator.compare(a.name, b.name))
  return list
    .filter((e) => e.name.toLowerCase().includes(f))
    .sort((a, b) => (a.dir !== b.dir ? (a.dir ? -1 : 1) : d * cmp(a, b) || collator.compare(a.name, b.name)))
}

export const date = (ms: number) =>
  new Date(ms).toLocaleString('zh-CN', { dateStyle: 'short', timeStyle: 'short' })

const rtf = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
const steps: [Intl.RelativeTimeFormatUnit, number][] = [['second', 60], ['minute', 60], ['hour', 24], ['day', 30], ['month', 12], ['year', Infinity]]

export function ago(ms: number) {
  let v = (ms - Date.now()) / 1000
  for (const [unit, n] of steps) {
    if (Math.abs(v) < n) return rtf.format(Math.round(v), unit)
    v /= n
  }
  return ''
}

const ext = (n: string) => (n.includes('.') ? n.slice(n.lastIndexOf('.') + 1).toLowerCase() : '')

const groups: Record<string, string> = {
  image: 'jpg jpeg png gif webp avif bmp svg ico',
  video: 'mp4 m4v webm mov mkv ogv',
  audio: 'mp3 m4a aac flac wav ogg opus',
  pdf: 'pdf',
  text: 'txt md log json yaml yml toml ini conf cfg csv tsv go js ts py rs c h cpp java kt sh fish zsh srt ass vtt xml html css sql',
}
const kinds = new Map(Object.entries(groups).flatMap(([k, v]) => v.split(' ').map((e) => [e, k] as const)))

export const kind = (n: string) => (kinds.get(ext(n)) ?? '') as 'image' | 'video' | 'audio' | 'pdf' | 'text' | ''

const thumbs = new Set('jpg jpeg png gif webp bmp tif tiff heic avif mp4 m4v mkv mov avi webm ts flv wmv mpg mpeg'.split(' '))
export const thumbable = (n: string) => thumbs.has(ext(n))
