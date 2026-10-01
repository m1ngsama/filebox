<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'
  import type HlsType from 'hls.js'

  type Mode = 'direct' | '1080' | '720' | '480'
  let {
    src,
    size,
    hls,
    poster,
    tracks,
    autoplay,
    spot,
    audio = $bindable(false),
    meta,
    onready,
    onfail,
  }: {
    src: string
    size: number
    hls: (q: string) => string
    poster?: string
    tracks: { src: string; lang: string }[]
    autoplay: boolean
    spot: string
    audio?: boolean
    meta: () => Promise<{ width?: number; height?: number; duration?: number }>
    onready: () => void
    onfail: () => void
  } = $props()

  const ladder: Mode[] = ['direct', '1080', '720', '480']
  const saved = (load('video-quality') as 'auto' | Mode | null) ?? 'auto'
  let pick = $state<'auto' | Mode>(saved)
  let mode = $state<Mode>(saved === 'auto' ? 'direct' : saved)
  let video = $state<HTMLVideoElement>()
  let engine: HlsType | undefined
  let stalls: number[] = []
  let last = 0
  let started = false
  let seekWatch = 0
  const info = untrack(() => meta())
  const rates = { '1080': 5e6, '720': 2.5e6, '480': 1e6 }
  const nearby = /^(localhost|127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|\[|[^.]+$)|\.local$/.test(location.hostname)

  async function initial(): Promise<Mode> {
    const m = await Promise.race([info, new Promise<Awaited<typeof info>>((r) => setTimeout(() => r({}), 1500))])
    const speed = Number(load('net-speed')) || (nearby ? Infinity : 8e6)
    const need = (bps: number) => bps * 1.5 < speed
    if (!m.duration || need((size * 8) / m.duration)) return 'direct'
    return (['1080', '720'] as const).find((q) => need(rates[q])) ?? '480'
  }

  function measure(list: PerformanceObserverEntryList) {
    for (const e of list.getEntries() as PerformanceResourceTiming[]) {
      const secs = (e.responseEnd - e.responseStart) / 1000
      if (!e.name.includes('/stream/seg') || e.encodedBodySize < 2e5 || secs < 0.05) continue
      const old = Number(load('net-speed'))
      const now = (e.encodedBodySize * 8) / secs
      save('net-speed', String(Math.round(old ? old * 0.7 + now * 0.3 : now)))
    }
  }

  function seeking() {
    clearTimeout(seekWatch)
    seekWatch = setTimeout(() => {
      if (pick === 'auto' && video && video.readyState < 3) degrade()
    }, 4000)
  }

  async function play(next: Mode, at = video?.currentTime ?? 0, resume = !(video?.paused ?? !autoplay)) {
    if (!video) return
    engine?.destroy()
    engine = undefined
    mode = next
    stalls = []
    started = false
    const url = next === 'direct' ? src : hls(next)
    if (next === 'direct' || video.canPlayType('application/vnd.apple.mpegurl')) video.src = url
    else {
      const Hls = (await import('hls.js/light')).default
      if (!Hls.isSupported()) return onfail()
      const h = new Hls({ startPosition: at, maxBufferLength: 30 })
      h.on(Hls.Events.ERROR, (_, d) => d.fatal && degrade())
      h.loadSource(url)
      h.attachMedia(video)
      engine = h
    }
    video.addEventListener(
      'loadedmetadata',
      () => {
        if (at > 1) video!.currentTime = at
        if (resume) video!.play().catch(() => {})
      },
      { once: true },
    )
  }

  function degrade() {
    if (pick !== 'auto') return onfail()
    const next = ladder[ladder.indexOf(mode) + 1]
    if (!next) return onfail()
    play(next)
    toast(t.smoother(next), { kind: 'info' })
  }

  function stalled() {
    if (!started || pick !== 'auto' || video?.seeking) return
    const now = performance.now()
    stalls = [...stalls.filter((s) => now - s < 30_000), now]
    if (stalls.length >= 2) degrade()
  }

  async function loaded() {
    const m = video!
    if (!m.videoWidth && mode === 'direct') {
      if ((await info).width) return degrade()
      audio = true
    }
    const at = Number(load(spot))
    if (at > 5 && at < m.duration - 5 && m.currentTime < 1) m.currentTime = at
    onready()
  }

  function track() {
    const now = video!.currentTime
    if (Math.abs(now - last) > 5) save(spot, String(Math.floor((last = now))))
  }

  function choose(e: Event) {
    pick = (e.currentTarget as HTMLSelectElement).value as 'auto' | Mode
    save('video-quality', pick)
    play(pick === 'auto' ? mode : pick)
  }

  onMount(() => {
    const watch = new PerformanceObserver(measure)
    watch.observe({ type: 'resource' })
    if (pick === 'auto') initial().then((m) => play(m, 0, autoplay))
    else play(mode, 0, autoplay)
    return () => {
      watch.disconnect()
      clearTimeout(seekWatch)
      engine?.destroy()
    }
  })
</script>

<div class="player">
  <!-- svelte-ignore a11y_media_has_caption -->
  <video
    bind:this={video}
    {poster}
    controls
    playsinline
    preload="metadata"
    class:audio-only={audio}
    onloadedmetadata={loaded}
    onplaying={() => {
      started = true
      clearTimeout(seekWatch)
    }}
    onseeking={seeking}
    onwaiting={stalled}
    ontimeupdate={track}
    onpause={() => save(spot, String(Math.floor(video!.currentTime)))}
    onended={() => save(spot, '')}
    onerror={() => (mode === 'direct' && !engine ? degrade() : onfail())}
  >
    {#each tracks as s, i (s.src)}
      <track kind="subtitles" src={s.src} label={s.lang || t.subtitles} srclang={/^[a-z]{2,3}(-[a-z0-9]+)*$/i.test(s.lang) ? s.lang : undefined} default={i === 0} />
    {/each}
  </video>
  {#if !audio}
    <label class="quality">
      <span class="sr">{t.quality}</span>
      <select value={pick} onchange={choose}>
        <option value="auto">{pick === 'auto' ? t.autoQuality(mode) : t.qualities.auto}</option>
        {#each ladder as q (q)}<option value={q}>{t.qualities[q]}</option>{/each}
      </select>
    </label>
  {/if}
</div>

<style>
  .player {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    height: 100%;
  }
  .quality {
    position: absolute;
    top: var(--space-2);
    right: var(--space-2);
  }
  .quality select {
    border: 0;
    border-radius: var(--radius-full);
    padding: var(--space-1) var(--space-3);
    background: color-mix(in srgb, var(--viewer-bg) 70%, transparent);
    color: var(--viewer-fg);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    font-size: var(--text-sm);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
