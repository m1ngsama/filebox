<script lang="ts">
  import Play from '@lucide/svelte/icons/play'
  import Pause from '@lucide/svelte/icons/pause'
  import SkipForward from '@lucide/svelte/icons/skip-forward'
  import X from '@lucide/svelte/icons/x'
  import ChevronDown from '@lucide/svelte/icons/chevron-down'
  import Music from '@lucide/svelte/icons/music'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { player } from '../lib/player.svelte'
  import { stem } from '../lib/format'
  import { t } from '../lib/i18n'

  let art = $state(false)
  const track = $derived(player.track)
  const title = $derived(player.tags.title || stem(track?.name ?? ''))
  const still = matchMedia('(prefers-reduced-motion: reduce)')

  $effect(() => {
    track?.cover
    art = false
  })
</script>

<svelte:window onkeydown={(e) => player.expanded && e.key === 'Escape' && !e.defaultPrevented && (player.expanded = false)} />

{#if track && player.expanded}
  <div class="viewer" role="dialog" aria-modal="true" aria-label={t.nowPlaying} transition:fly|global={{ y: 40, duration: still.matches ? 0 : 240, easing: cubicOut }}>
    <header>
      <button class="icon-btn" onclick={() => (player.expanded = false)} aria-label={t.collapse}><ChevronDown size={22} /></button>
      <span class="title">{t.nowPlaying}</span>
    </header>
    <div class="body">
      {#await import('./AudioPlayer.svelte') then { default: AudioPlayer }}<AudioPlayer />{/await}
    </div>
  </div>
{:else if track && !player.viewing}
  <div class="mini" role="region" aria-label={t.nowPlaying} transition:fly|global={{ y: 24, duration: still.matches ? 0 : 200, easing: cubicOut }}>
    <button class="open" onclick={() => (player.expanded = true)} aria-label={`${t.nowPlaying}: ${title}`}>
      <span class="thumb">
        {#if track.cover}{#key track.cover}<img src={track.cover} alt="" class:ok={art} onload={() => (art = true)} onerror={() => (art = false)} />{/key}{/if}
        {#if !art}<Music size={18} aria-hidden="true" />{/if}
      </span>
      <span class="words">
        <span class="name">{title}</span>
        {#if player.tags.artist}<span class="who">{player.tags.artist}</span>{/if}
      </span>
    </button>
    <button class="icon-btn" onclick={() => player.toggle()} aria-label={player.paused ? t.play : t.pause}>
      {#if player.paused}<Play size={20} fill="currentColor" />{:else}<Pause size={20} fill="currentColor" />{/if}
    </button>
    <button class="icon-btn" onclick={() => player.next()} disabled={!player.hasNext} aria-label={t.nextTrack}><SkipForward size={20} fill="currentColor" /></button>
    <button class="icon-btn" onclick={() => player.stop()} aria-label={t.stopPlaying}><X size={18} /></button>
    <span class="line" style:scale={player.total ? `${player.now / player.total} 1` : '0 1'}></span>
  </div>
{/if}

<style>
  .mini {
    position: fixed;
    left: 50%;
    bottom: calc(var(--dock) + var(--space-3));
    translate: -50% 0;
    z-index: 35;
    display: flex;
    align-items: center;
    gap: var(--space-0);
    width: min(440px, calc(100vw - 2 * var(--space-3)));
    height: 60px;
    padding: 0 var(--space-1) 0 var(--space-1-5);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--panel) 86%, transparent);
    backdrop-filter: blur(20px) saturate(1.6);
    -webkit-backdrop-filter: blur(20px) saturate(1.6);
    border: 1px solid var(--line);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .open {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-3);
    height: 100%;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    text-align: start;
    cursor: pointer;
  }
  .thumb {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: var(--radius-sm);
    background: var(--hover);
    color: var(--muted);
    overflow: hidden;
  }
  .thumb img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    opacity: 0;
    transition: opacity var(--dur-2) var(--ease-out);
  }
  .thumb img.ok {
    opacity: 1;
  }
  .words {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .name,
  .who {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .name {
    font-weight: 600;
  }
  .who {
    color: var(--muted);
    font-size: var(--text-sm);
  }
  :global(.shell) ~ .mini {
    left: calc(50% + 124px);
  }
  @media (max-width: 767px) {
    :global(.shell) ~ .mini {
      left: 50%;
    }
  }
  .line {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 2px;
    background: var(--accent);
    transform-origin: left;
    transition: scale 0.25s linear;
  }
</style>
