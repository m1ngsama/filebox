import { load, save } from './storage'
import { parse, decode, type Line } from './lyrics'

export type Track = { name: string; src: string; cover?: string; meta: string; lyrics?: string }
type Tags = { title?: string; artist?: string; album?: string; lyrics?: string }

const speeds = [1, 1.25, 1.5, 2, 0.75]

class Player {
  queue = $state.raw<Track[]>([])
  i = $state(-1)
  paused = $state(true)
  now = $state(0)
  total = $state(0)
  tags = $state.raw<Tags>({})
  rate = $state(Number(load('audio-rate')) || 1)
  lines = $state.raw<Line[]>([])
  words = $state('')
  viewing = $state(0)
  expanded = $state(false)
  #el: HTMLAudioElement | null = null
  #saved = 0

  get track(): Track | undefined {
    return this.queue[this.i]
  }
  get long() {
    return this.total > 600
  }
  get hasPrev() {
    return this.i > 0
  }
  get hasNext() {
    return this.i >= 0 && this.i < this.queue.length - 1
  }

  #audio() {
    if (this.#el) return this.#el
    const a = new Audio()
    a.preload = 'metadata'
    a.addEventListener('loadedmetadata', () => {
      this.total = a.duration
      a.playbackRate = this.long ? this.rate : 1
      const at = Number(load(this.#spot()))
      if (at > 5 && at < a.duration - 5) a.currentTime = at
    })
    a.addEventListener('timeupdate', () => {
      this.now = a.currentTime
      if (Math.abs(a.currentTime - this.#saved) > 5) save(this.#spot(), String((this.#saved = Math.floor(a.currentTime))))
    })
    a.addEventListener('play', () => (this.paused = false))
    a.addEventListener('pause', () => {
      this.paused = true
      if (!a.ended && this.track) save(this.#spot(), String(Math.floor(a.currentTime)))
    })
    a.addEventListener('ended', () => {
      save(this.#spot(), '')
      if (this.hasNext) this.go(this.i + 1, true)
    })
    a.addEventListener('error', () => this.onerror?.())
    const ms = navigator.mediaSession
    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => a.play()],
      ['pause', () => a.pause()],
      ['previoustrack', () => this.prev()],
      ['nexttrack', () => this.next()],
      ['seekbackward', (d) => this.seek(this.now - (d.seekOffset ?? 15))],
      ['seekforward', (d) => this.seek(this.now + (d.seekOffset ?? 15))],
      ['seekto', (d) => d.seekTime !== undefined && this.seek(d.seekTime)],
    ]
    for (const [k, h] of handlers) {
      try {
        ms?.setActionHandler(k, h)
      } catch {}
    }
    return (this.#el = a)
  }

  onerror?: () => void

  #spot() {
    return 'pos:' + (this.track?.src ?? '')
  }

  play(queue: Track[], i: number, autoplay = true) {
    if (this.track?.src === queue[i]?.src) {
      this.queue = queue
      this.i = i
      return
    }
    this.queue = queue
    this.go(i, autoplay)
  }

  go(i: number, autoplay = true) {
    const t = this.queue[i]
    if (!t) return
    const a = this.#audio()
    this.i = i
    this.now = this.total = this.#saved = 0
    this.tags = {}
    this.lines = []
    this.words = ''
    a.src = t.src
    if (t.lyrics)
      fetch(t.lyrics)
        .then((r) => (r.ok ? r.arrayBuffer() : Promise.reject()))
        .then((b) => this.track?.src === t.src && (this.lines = parse(decode(b))), () => {})
    if (autoplay) a.play().catch(() => {})
    this.#session()
    fetch(t.meta)
      .then((r) => (r.ok ? r.json() : {}))
      .then((m: Tags) => {
        if (this.track?.src !== t.src) return
        this.tags = m
        if (m.lyrics && !this.lines.length) {
          const timed = parse(m.lyrics)
          if (timed.length) this.lines = timed
          else this.words = m.lyrics
        }
        this.#session()
      }, () => {})
  }

  #session() {
    const t = this.track
    if (!t || !navigator.mediaSession || typeof MediaMetadata === 'undefined') return
    navigator.mediaSession.metadata = new MediaMetadata({
      title: this.tags.title || t.name.replace(/\.[^.]+$/, ''),
      artist: this.tags.artist ?? '',
      album: this.tags.album ?? '',
      artwork: t.cover ? [{ src: new URL(t.cover, location.href).href }] : [],
    })
  }

  toggle() {
    const a = this.#el
    if (!a) return
    if (a.paused) a.play().catch(() => {})
    else a.pause()
  }

  seek(to: number) {
    const a = this.#el
    if (!a || !Number.isFinite(a.duration)) return
    a.currentTime = Math.min(Math.max(0, to), a.duration)
    this.now = a.currentTime
  }

  prev() {
    if (this.now > 3) this.seek(0)
    else if (this.hasPrev) this.go(this.i - 1, !this.paused)
  }

  next() {
    if (this.hasNext) this.go(this.i + 1, !this.paused)
  }

  faster() {
    this.rate = speeds[(speeds.indexOf(this.rate) + 1) % speeds.length]
    save('audio-rate', String(this.rate))
    if (this.#el) this.#el.playbackRate = this.rate
  }

  stop() {
    const a = this.#el
    if (a) {
      a.pause()
      a.removeAttribute('src')
      a.load()
    }
    this.queue = []
    this.i = -1
    this.expanded = false
    if (navigator.mediaSession) navigator.mediaSession.metadata = null
  }
}

export const player = new Player()
