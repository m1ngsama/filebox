<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import Play from '@lucide/svelte/icons/play'
  import Pause from '@lucide/svelte/icons/pause'
  import SkipBack from '@lucide/svelte/icons/skip-back'
  import SkipForward from '@lucide/svelte/icons/skip-forward'
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw'
  import RotateCw from '@lucide/svelte/icons/rotate-cw'
  import Music from '@lucide/svelte/icons/music'
  import { clock, stem } from '../lib/format'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'

  let {
    src,
    name,
    cover,
    meta,
    autoplay,
    spot,
    onprev,
    onnext,
    onfail,
  }: {
    src: string
    name: string
    cover?: string
    meta: () => Promise<{ title?: string; artist?: string; album?: string; duration?: number }>
    autoplay: boolean
    spot: string
    onprev?: () => void
    onnext?: () => void
    onfail: () => void
  } = $props()

  const speeds = [1, 1.25, 1.5, 2, 0.75]
  let audio = $state<HTMLAudioElement>()
  let main = $state<HTMLButtonElement>()
  let tags = $state<{ title?: string; artist?: string; album?: string }>({})
  let art = $state(false)
  let paused = $state(true)
  let now = $state(0)
  let total = $state(0)
  let dragging = $state(false)
  let rate = $state(Number(load('audio-rate')) || 1)
  let last = 0

  const title = $derived(tags.title || stem(name))
  const byline = $derived([tags.artist, tags.album].filter(Boolean).join(' — '))
  const long = $derived(total > 600)

  untrack(() => meta()).then((m) => (tags = m))

  function toggle() {
    if (!audio) return
    if (audio.paused) audio.play().catch(() => {})
    else audio.pause()
  }

  function seek(to: number) {
    if (!audio || !Number.isFinite(audio.duration)) return
    audio.currentTime = Math.min(Math.max(0, to), audio.duration)
    now = audio.currentTime
  }

  function loaded() {
    const a = audio!
    total = a.duration
    a.playbackRate = long ? rate : 1
    const at = Number(load(spot))
    if (at > 5 && at < a.duration - 5) a.currentTime = at
  }

  function tick() {
    if (!audio || dragging) return
    now = audio.currentTime
    if (Math.abs(now - last) > 5) save(spot, String(Math.floor((last = now))))
  }

  function ended() {
    save(spot, '')
    onnext?.()
  }

  function faster() {
    rate = speeds[(speeds.indexOf(rate) + 1) % speeds.length]
    save('audio-rate', String(rate))
    if (audio) audio.playbackRate = rate
  }

  function key(e: KeyboardEvent) {
    if (e.key !== ' ' || e.defaultPrevented || (e.target as Element).closest?.('input, button, a, textarea')) return
    e.preventDefault()
    toggle()
  }

  $effect(() => {
    const ms = navigator.mediaSession
    if (!ms || typeof MediaMetadata === 'undefined') return
    ms.metadata = new MediaMetadata({ title, artist: tags.artist ?? '', album: tags.album ?? '', artwork: art && cover ? [{ src: new URL(cover, location.href).href }] : [] })
  })

  onMount(() => {
    const ms = navigator.mediaSession
    const handlers: [MediaSessionAction, MediaSessionActionHandler | null][] = [
      ['play', () => audio?.play()],
      ['pause', () => audio?.pause()],
      ['previoustrack', onprev ? () => onprev() : null],
      ['nexttrack', onnext ? () => onnext() : null],
      ['seekbackward', (d) => seek(now - (d.seekOffset ?? 15))],
      ['seekforward', (d) => seek(now + (d.seekOffset ?? 15))],
      ['seekto', (d) => d.seekTime !== undefined && seek(d.seekTime)],
    ]
    for (const [a, h] of handlers) {
      try {
        ms?.setActionHandler(a, h)
      } catch {}
    }
    main?.focus()
    if (autoplay) audio?.play().catch(() => {})
    return () => {
      if (audio) save(spot, audio.ended ? '' : String(Math.floor(audio.currentTime)))
      for (const [a] of handlers) {
        try {
          ms?.setActionHandler(a, null)
        } catch {}
      }
    }
  })
</script>

<svelte:window onkeydown={key} />

<div class="audio" class:paused style:--art={art && cover ? `url("${cover}")` : null}>
  <div class="art" class:ok={art}>
    {#if cover}<img src={cover} alt="" onload={() => (art = true)} onerror={() => (art = false)} />{/if}
    {#if !art}<Music size={64} strokeWidth={1.25} aria-hidden="true" />{/if}
  </div>
  <div class="now">
    <h2 title={title}>{title}</h2>
    {#if byline}<p title={byline}>{byline}</p>{/if}
  </div>
  <div class="scrub">
    <input
      type="range"
      min="0"
      max={total || 0}
      step="any"
      value={now}
      aria-label={t.position}
      aria-valuetext={`${clock(now)} / ${clock(total)}`}
      style:--done={total ? `${(now / total) * 100}%` : '0%'}
      oninput={(e) => {
        dragging = true
        now = Number(e.currentTarget.value)
      }}
      onchange={(e) => {
        dragging = false
        seek(Number(e.currentTarget.value))
      }}
    />
    <div class="times"><span>{clock(now)}</span><span>-{clock(Math.max(0, total - now))}</span></div>
  </div>
  <div class="controls">
    {#if long}
      <button type="button" class="ctl small" onclick={faster} aria-label={t.playbackSpeed} title={t.playbackSpeed}><span class="rate">{rate}×</span></button>
      <button type="button" class="ctl" onclick={() => seek(now - 15)} aria-label={t.skipBack} title={t.skipBack}><RotateCcw size={30} strokeWidth={1.75} /><span class="sec" aria-hidden="true">15</span></button>
    {:else}
      <button type="button" class="ctl" onclick={() => onprev?.()} disabled={!onprev} aria-label={t.prevTrack} title={t.prevTrack}><SkipBack size={26} fill="currentColor" /></button>
    {/if}
    <button type="button" class="ctl main" bind:this={main} onclick={toggle} aria-label={paused ? t.play : t.pause}>
      {#if paused}<Play size={30} fill="currentColor" />{:else}<Pause size={30} fill="currentColor" />{/if}
    </button>
    {#if long}
      <button type="button" class="ctl" onclick={() => seek(now + 15)} aria-label={t.skipForward} title={t.skipForward}><RotateCw size={30} strokeWidth={1.75} /><span class="sec" aria-hidden="true">15</span></button>
      <button type="button" class="ctl small" onclick={() => onnext?.()} disabled={!onnext} aria-label={t.nextTrack} title={t.nextTrack}><SkipForward size={20} fill="currentColor" /></button>
    {:else}
      <button type="button" class="ctl" onclick={() => onnext?.()} disabled={!onnext} aria-label={t.nextTrack} title={t.nextTrack}><SkipForward size={26} fill="currentColor" /></button>
    {/if}
  </div>
  <audio
    bind:this={audio}
    {src}
    preload="metadata"
    onloadedmetadata={loaded}
    ontimeupdate={tick}
    onplay={() => (paused = false)}
    onpause={() => {
      paused = true
      save(spot, String(Math.floor(audio!.currentTime)))
    }}
    onended={ended}
    onerror={onfail}
  ></audio>
</div>

<style>
  .audio {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-5);
    padding: var(--space-6) var(--space-6) calc(var(--space-8) + env(safe-area-inset-bottom));
    overflow: hidden;
    isolation: isolate;
  }
  .audio::before {
    content: '';
    position: absolute;
    inset: -20%;
    z-index: -1;
    background: var(--art, none) center / cover;
    filter: blur(60px) saturate(1.4);
    opacity: 0.45;
    transition: opacity var(--dur-3) var(--ease-out);
  }
  .art {
    position: relative;
    width: min(340px, 72vw, 42vh);
    aspect-ratio: 1;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: var(--radius-lg);
    background: linear-gradient(145deg, rgb(255 255 255 / 0.14), rgb(255 255 255 / 0.04));
    color: var(--viewer-icon);
    box-shadow: 0 24px 60px rgb(0 0 0 / 0.45);
    overflow: hidden;
    transition: scale var(--dur-3) var(--ease-out);
  }
  .audio.paused .art.ok {
    scale: 0.88;
  }
  .art img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    opacity: 0;
    transition: opacity var(--dur-2) var(--ease-out);
  }
  .art.ok img {
    opacity: 1;
  }
  .now {
    width: min(340px, 100%);
    text-align: center;
  }
  .now h2 {
    margin: 0;
    font-size: var(--text-lg);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .now p {
    margin: var(--space-1) 0 0;
    color: var(--viewer-icon);
    opacity: 0.8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .scrub {
    width: min(340px, 100%);
  }
  .scrub input {
    display: block;
    width: 100%;
    height: 20px;
    margin: 0;
    padding: 0;
    border: 0;
    background: none;
    appearance: none;
    cursor: pointer;
  }
  .scrub input::-webkit-slider-runnable-track {
    height: 4px;
    border-radius: 2px;
    background: linear-gradient(to right, var(--viewer-fg) var(--done), rgb(255 255 255 / 0.22) var(--done));
    transition: height var(--dur-1) var(--ease-out);
  }
  .scrub input::-moz-range-track {
    height: 4px;
    border-radius: 2px;
    background: linear-gradient(to right, var(--viewer-fg) var(--done), rgb(255 255 255 / 0.22) var(--done));
  }
  .scrub input::-webkit-slider-thumb {
    appearance: none;
    width: 12px;
    height: 12px;
    margin-top: -4px;
    border-radius: 50%;
    background: var(--viewer-fg);
    scale: 0;
    transition: scale var(--dur-1) var(--ease-out);
  }
  .scrub input::-moz-range-thumb {
    width: 12px;
    height: 12px;
    border: 0;
    border-radius: 50%;
    background: var(--viewer-fg);
  }
  .scrub input:is(:active, :focus-visible)::-webkit-slider-thumb {
    scale: 1;
  }
  @media (hover: hover) and (pointer: fine) {
    .scrub input:hover::-webkit-slider-thumb {
      scale: 1;
    }
    .ctl:not(:disabled, .main):hover {
      background: var(--viewer-hover);
    }
  }
  .times {
    display: flex;
    justify-content: space-between;
    margin-top: var(--space-1);
    color: var(--viewer-icon);
    opacity: 0.75;
    font-size: var(--text-xs);
    font-variant-numeric: tabular-nums;
  }
  .controls {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .ctl {
    display: grid;
    place-items: center;
    width: 52px;
    height: 52px;
    border: 0;
    border-radius: 50%;
    background: none;
    color: var(--viewer-fg);
    cursor: pointer;
    transition:
      scale var(--dur-1) var(--ease-out),
      background var(--dur-1) var(--ease-out);
  }
  .ctl:active:not(:disabled) {
    scale: 0.9;
  }
  .ctl:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .ctl.small {
    width: 44px;
    height: 44px;
  }
  .ctl.main {
    width: 68px;
    height: 68px;
    background: var(--viewer-fg);
    color: var(--viewer-bg);
  }
  .ctl > :global(*) {
    grid-area: 1 / 1;
  }
  .sec {
    padding-top: 1px;
    font-size: 9px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .rate {
    font-size: var(--text-sm);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  @media (prefers-reduced-motion: reduce) {
    .art,
    .ctl {
      transition: none;
    }
  }
</style>
