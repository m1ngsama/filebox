<script lang="ts">
  import { onMount } from 'svelte'
  import Play from '@lucide/svelte/icons/play'
  import Pause from '@lucide/svelte/icons/pause'
  import SkipBack from '@lucide/svelte/icons/skip-back'
  import SkipForward from '@lucide/svelte/icons/skip-forward'
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw'
  import RotateCw from '@lucide/svelte/icons/rotate-cw'
  import Music from '@lucide/svelte/icons/music'
  import { clock, stem } from '../lib/format'
  import { player } from '../lib/player.svelte'
  import { load, save } from '../lib/storage'
  import MicVocal from '@lucide/svelte/icons/mic-vocal'
  import { t } from '../lib/i18n'

  let { onfail }: { onfail?: () => void } = $props()

  let main = $state<HTMLButtonElement>()
  let art = $state(false)
  let dragging = $state(false)
  let drag = $state(0)

  const track = $derived(player.track)
  const cover = $derived(track?.cover)
  const title = $derived(player.tags.title || stem(track?.name ?? ''))
  const byline = $derived([player.tags.artist, player.tags.album].filter(Boolean).join(' — '))
  const now = $derived(dragging ? drag : player.now)
  const hasWords = $derived(player.lines.length > 0 || !!player.words)
  let showWords = $state(load('lyrics') !== '0')
  const singing = $derived(hasWords && showWords)
  let box = $state<HTMLDivElement>()
  let hold = 0
  const cur = $derived.by(() => {
    const ls = player.lines
    let lo = 0
    let hi = ls.length - 1
    let at = -1
    while (lo <= hi) {
      const mid = (lo + hi) >> 1
      if (ls[mid].at <= player.now + 0.2) {
        at = mid
        lo = mid + 1
      } else hi = mid - 1
    }
    return at
  })

  $effect(() => {
    const el = box?.children[cur] as HTMLElement | undefined
    if (!el || !box || performance.now() < hold) return
    box.scrollTo({ top: el.offsetTop - box.clientHeight * 0.4, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' })
  })

  const held = () => (hold = performance.now() + 3000)

  function flip() {
    showWords = !showWords
    save('lyrics', showWords ? '1' : '0')
  }
  const total = $derived(player.total)

  $effect(() => {
    cover
    art = false
  })

  function key(e: KeyboardEvent) {
    if (e.key !== ' ' || e.defaultPrevented || (e.target as Element).closest?.('input, button, a, textarea')) return
    e.preventDefault()
    player.toggle()
  }

  onMount(() => {
    player.viewing++
    player.onerror = onfail
    main?.focus()
    return () => {
      player.viewing--
      if (player.onerror === onfail) player.onerror = undefined
    }
  })
</script>

<svelte:window onkeydown={key} />

<div class="audio" class:paused={player.paused} class:words={singing} style:--art={art && cover ? `url("${cover}")` : null}>
  <div class="stage">
  <div class="art" class:ok={art}>
    {#if cover}{#key cover}<img src={cover} alt="" onload={() => (art = true)} onerror={() => (art = false)} />{/key}{/if}
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
        drag = Number(e.currentTarget.value)
      }}
      onchange={(e) => {
        dragging = false
        player.seek(Number(e.currentTarget.value))
      }}
    />
    <div class="times"><span>{clock(now)}</span><span>-{clock(Math.max(0, total - now))}</span></div>
  </div>
  <div class="controls">
    {#if player.long}
      <button type="button" class="ctl small" onclick={() => player.faster()} aria-label={t.playbackSpeed} title={t.playbackSpeed}><span class="rate">{player.rate}×</span></button>
      <button type="button" class="ctl" onclick={() => player.seek(player.now - 15)} aria-label={t.skipBack} title={t.skipBack}><RotateCcw size={30} strokeWidth={1.75} /><span class="sec" aria-hidden="true">15</span></button>
    {:else}
      <button type="button" class="ctl" onclick={() => player.prev()} disabled={!player.hasPrev && player.now <= 3} aria-label={t.prevTrack} title={t.prevTrack}><SkipBack size={26} fill="currentColor" /></button>
    {/if}
    <button type="button" class="ctl main" bind:this={main} onclick={() => player.toggle()} aria-label={player.paused ? t.play : t.pause}>
      {#if player.paused}<Play size={30} fill="currentColor" />{:else}<Pause size={30} fill="currentColor" />{/if}
    </button>
    {#if player.long}
      <button type="button" class="ctl" onclick={() => player.seek(player.now + 15)} aria-label={t.skipForward} title={t.skipForward}><RotateCw size={30} strokeWidth={1.75} /><span class="sec" aria-hidden="true">15</span></button>
      <button type="button" class="ctl small" onclick={() => player.next()} disabled={!player.hasNext} aria-label={t.nextTrack} title={t.nextTrack}><SkipForward size={20} fill="currentColor" /></button>
    {:else}
      <button type="button" class="ctl" onclick={() => player.next()} disabled={!player.hasNext} aria-label={t.nextTrack} title={t.nextTrack}><SkipForward size={26} fill="currentColor" /></button>
    {/if}
    {#if hasWords}
      <button type="button" class="ctl small" aria-pressed={showWords} onclick={flip} aria-label={showWords ? t.showCover : t.showLyrics} title={showWords ? t.showCover : t.showLyrics}><MicVocal size={20} /></button>
    {/if}
  </div>
  </div>
  {#if singing}
    <div class="lyrics" bind:this={box} onwheel={held} ontouchmove={held} role="region" aria-label={t.lyrics}>
      {#if player.lines.length}
        {#each player.lines as l, i (i)}
          <button type="button" class="line" class:on={i === cur} class:past={i < cur} onclick={() => player.seek(l.at)}>{l.text || '♪'}</button>
        {/each}
      {:else}
        <p class="plain">{player.words}</p>
      {/if}
    </div>
  {/if}
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
  .stage {
    display: contents;
  }
  .lyrics {
    position: relative;
    flex: 1 1 0;
    min-height: 0;
    width: min(560px, 100%);
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
    padding: 30% 0;
    mask-image: linear-gradient(transparent, #000 18%, #000 82%, transparent);
  }
  .line {
    display: block;
    width: 100%;
    margin: 0;
    padding: var(--space-2) var(--space-1);
    border: 0;
    border-radius: var(--radius-md);
    background: none;
    color: var(--viewer-fg);
    font-size: 22px;
    font-weight: 700;
    line-height: 1.3;
    text-align: start;
    opacity: 0.3;
    cursor: pointer;
    transition: opacity var(--dur-3) var(--ease-out);
  }
  .line.on {
    opacity: 1;
  }
  .plain {
    margin: 0;
    white-space: pre-line;
    font-size: 18px;
    line-height: 1.7;
    color: var(--viewer-fg);
    opacity: 0.85;
  }
  .words .art {
    display: none;
  }
  .words .lyrics {
    order: -1;
  }
  @media (min-width: 900px) {
    .audio.words {
      flex-direction: row;
      gap: clamp(32px, 6vw, 96px);
    }
    .words .stage {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: var(--space-5);
      width: 340px;
      flex: none;
    }
    .words .art {
      display: grid;
    }
    .words .lyrics {
      flex: 0 1 560px;
      height: min(72vh, 620px);
    }
    .line {
      font-size: 28px;
    }
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
    .line:hover {
      opacity: 0.7;
      background: rgb(255 255 255 / 0.06);
    }
    .line.on:hover {
      opacity: 1;
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
  .ctl[aria-pressed='true'] {
    background: var(--viewer-hover);
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
