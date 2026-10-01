<script lang="ts">
  import { onMount } from 'svelte'
  import { cubicOut } from 'svelte/easing'
  import X from '@lucide/svelte/icons/x'
  import Download from '@lucide/svelte/icons/download'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import type { Entry, Src } from '../lib/api'
  import { kind, thumbable, subtitleOf, subtitleLang, sidecars, stem } from '../lib/format'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'

  let {
    entry = $bindable(),
    entries,
    url,
    onclose,
    siblings = true,
    inline = false,
  }: { entry: Entry; entries: Entry[]; url: Src; onclose: () => void; siblings?: boolean; inline?: boolean } = $props()
  const k = $derived(kind(entry.name))
  const src = $derived(url(entry))
  const images = $derived(k === 'image' ? entries.filter((e) => !e.dir && kind(e.name) === 'image') : [])
  const subs = $derived(new Set([...sidecars(entries).values()].flat()))
  const list = $derived(inline ? [] : entries.filter((e) => !e.dir && kind(e.name) && (!subs.has(e.name) || e === entry)))
  const at = $derived(list.indexOf(entry))
  const songs = $derived(k === 'audio' ? list.filter((e) => kind(e.name) === 'audio') : [])
  const song = $derived(songs.indexOf(entry))
  const step = (d: number) => at >= 0 && list[at + d] && (entry = list[at + d])
  let pages = $state<Entry[] | null>(null)
  let rtl = $state(false)
  const folder = $derived('rtl:' + src.slice(0, src.lastIndexOf('/')))
  const pageURL: Src = (e, as) => (as === 'dl' ? url(entry, 'dl') : `${url(entry, 'zip-entry')}&e=${encodeURIComponent(e.name)}`)
  let frame = $state<HTMLIFrameElement>()
  const reader = $derived.by(() => {
    if (k !== 'book') return ''
    const u = new URL(url(entry, 'zip-entries'), location.origin)
    const tok = u.pathname.startsWith('/s/') ? u.pathname.split('/')[2] : ''
    const p = u.searchParams.get('p') ?? ''
    return '/reader#' + new URLSearchParams(tok ? { share: tok, p } : { vol: u.searchParams.get('vol') ?? '', p })
  })

  function fromReader(e: MessageEvent) {
    if (e.origin !== location.origin || !frame || e.source !== frame.contentWindow) return
    const kind = (e.data as { fb?: string } | null)?.fb
    if (kind === 'ready') {
      ready()
      frame.focus()
    }
    else if (kind === 'fail') failed()
    else if (kind === 'close') onclose()
  }
  const tracks = $derived(
    k === 'video' && siblings
      ? entries
          .filter((e) => !e.dir && subtitleOf(entry.name, e.name))
          .map((e) => ({ src: url(e) + '?vtt', lang: subtitleLang(entry.name, e.name) }))
      : [],
  )
  const spot = $derived('pos:' + src)
  let audioOnly = $state(false)
  let status = $state<'loading' | 'ready' | 'error'>('loading')
  let root = $state<HTMLDivElement>()
  let closer = $state<HTMLButtonElement>()

  $effect(() => {
    src
    audioOnly = false
    status = k === 'audio' || !k ? 'ready' : 'loading'
  })

  $effect(() => {
    if (k !== 'comic') return
    let stale = false
    pages = null
    rtl = load(folder) === '1'
    fetch(url(entry, 'zip-entries'))
      .then((r) => (r.ok ? r.json() : Promise.reject()))
      .then(
        ({ entries }: { entries: { name: string; size: number }[] }) => {
          if (stale) return
          pages = entries.map((e) => ({ name: e.name, size: e.size, dir: false, mtime: 0 }))
          if (!pages.length) status = 'error'
        },
        () => !stale && (status = 'error'),
      )
    return () => {
      stale = true
    }
  })

  function flip() {
    rtl = !rtl
    save(folder, rtl ? '1' : '')
  }

  onMount(() => {
    const vv = visualViewport
    const zoom = () => (zoomed = (vv?.scale ?? 1) > 1)
    zoom()
    vv?.addEventListener('resize', zoom)
    const back = inline ? null : (document.activeElement as HTMLElement | null)
    closer?.focus()
    return () => {
      vv?.removeEventListener('resize', zoom)
      back?.focus()
    }
  })

  function key(e: KeyboardEvent) {
    if (k === 'image' || (k === 'comic' && pages?.length) || inline || e.defaultPrevented) return
    if (e.key === 'Escape') onclose()
    else if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && k !== 'book' && k !== 'comic' && !e.metaKey && !e.ctrlKey && !e.altKey && !(e.target instanceof HTMLMediaElement))
      step(e.key === 'ArrowRight' ? 1 : -1)
    else if (e.key === 'Tab' && root) {
      const f = [...root.querySelectorAll<HTMLElement>('a[href], button, video, audio, iframe, pre, article')].filter((x) => x.offsetParent)
      const i = f.indexOf(document.activeElement as HTMLElement)
      const to = e.shiftKey ? (i <= 0 ? f.length - 1 : i - 1) : i === f.length - 1 ? 0 : i + 1
      e.preventDefault()
      f[to]?.focus()
    }
  }

  let start: { x: number; y: number } | null = null
  let off = $state({ x: 0, y: 0 })
  let zoomed = $state(false)

  function touchstart(e: TouchEvent) {
    const media = e.target instanceof HTMLMediaElement || (e.target as Element).closest?.('input') !== null
    const one = e.touches.length === 1 && k !== 'text' && k !== 'pdf' && k !== 'book' && !media && !zoomed && !inline
    start = one ? { x: e.touches[0].clientX, y: e.touches[0].clientY } : null
    off = { x: 0, y: 0 }
  }

  function touchmove(e: TouchEvent) {
    if (!start || e.touches.length !== 1) return touchcancel()
    const dx = e.touches[0].clientX - start.x
    const dy = e.touches[0].clientY - start.y
    swipe = Math.abs(dx) > Math.abs(dy) ? dx : 0
    off = { x: 0, y: swipe ? 0 : Math.max(0, dy) }
  }

  function touchend() {
    const { y } = off
    const x = swipe
    touchcancel()
    if (y > 100) onclose()
    else if (Math.abs(x) > 80) step(x < 0 ? 1 : -1)
  }

  let swipe = 0
  function touchcancel() {
    swipe = 0
    start = null
    off = { x: 0, y: 0 }
  }

  const still = matchMedia('(prefers-reduced-motion: reduce)')
  function pop(_: Element, { out = false } = {}) {
    return { duration: still.matches || inline ? 0 : out ? 160 : 220, easing: cubicOut, css: (k: number) => `opacity: ${k}; scale: ${out ? 1 : 0.98 + 0.02 * k}` }
  }

  const ready = () => (status = 'ready')
  const failed = () => (status = 'error')

</script>

<svelte:window onkeydown={key} onmessage={fromReader} />

{#if k === 'image' || (k === 'comic' && pages?.length)}
  {#await import('./Lightbox.svelte')}
    <div class="viewer" role="dialog" aria-modal="true" aria-label={entry.name}><div class="spinner" role="status" aria-label={t.loading}></div></div>
  {:then { default: Lightbox }}
    {#if k === 'image'}
      <Lightbox bind:entry {images} {url} {onclose} onedge={list.length > images.length ? step : undefined} />
    {:else if pages}
      {#key rtl}
        <Lightbox
          entry={pages[Math.min(Number(load(spot)) || 0, pages.length - 1)]}
          images={pages}
          url={pageURL}
          {onclose}
          onedge={list.length > 1 ? step : undefined}
          book={{ title: entry.name, rtl, onpage: (i) => save(spot, String(i)), onrtl: flip }}
        />
      {/key}
    {/if}
  {/await}
{:else}
<div
  in:pop|global
  out:pop|global={{ out: true }}
  class="viewer"
  class:inline
  role={inline ? 'region' : 'dialog'}
  aria-modal={inline ? undefined : 'true'}
  tabindex="-1"
  aria-label={entry.name}
  bind:this={root}
  ontouchstart={touchstart}
  ontouchmove={touchmove}
  ontouchend={touchend}
  ontouchcancel={touchcancel}
>
  {#if !inline}
    <header>
      <span class="title" title={entry.name}>{k === 'book' ? stem(entry.name) : entry.name}</span>
      {#if list.length > 1 && k !== 'book'}
        <button class="icon-btn" onclick={() => step(-1)} disabled={at <= 0} aria-label={t.prevFile}><ChevronLeft size={20} /></button>
        <button class="icon-btn" onclick={() => step(1)} disabled={at < 0 || at >= list.length - 1} aria-label={t.nextFile}><ChevronRight size={20} /></button>
      {/if}
      <a class="icon-btn" href={url(entry, 'dl')} download aria-label={t.download}><Download size={20} /></a>
      <button class="icon-btn" onclick={onclose} aria-label={t.close} bind:this={closer}><X size={20} /></button>
    </header>
  {/if}
  <div
    class="body"
    aria-busy={status === 'loading'}
    style:translate={off.x || off.y ? `${off.x}px ${off.y}px` : null}
    style:opacity={off.y ? Math.max(0.3, 1 - off.y / 400) : null}
  >
    {#if status === 'loading'}<div class="spinner" role="status" aria-label={t.loading}></div>{/if}
    {#if status === 'error'}
      <div class="viewer-error" role="alert">
        <CircleAlert size={40} aria-hidden="true" />
        <p>{k === 'video' ? t.cantPlay : k === 'comic' && pages ? t.noPages : t.previewFailed}</p>
        <a class="button primary" href={url(entry, 'dl')} download><Download size={18} />{t.download}</a>
      </div>
    {:else if k === 'video'}
      {#await import('./VideoPlayer.svelte') then { default: VideoPlayer }}
        {#key src}
          <VideoPlayer
            {src}
            size={entry.size}
            hls={(q) => `${url(entry, 'stream')}&q=${q}`}
            poster={thumbable(entry.name) ? url(entry, 'thumb') : undefined}
            {tracks}
            autoplay={!inline}
            {spot}
            bind:audio={audioOnly}
            meta={() => fetch(url(entry, 'meta')).then((r) => (r.ok ? r.json() : {}), () => ({}))}
            onready={ready}
            onfail={failed}
          />
        {/key}
      {/await}
    {:else if k === 'audio'}
      {#await import('./AudioPlayer.svelte') then { default: AudioPlayer }}
        {#key src}
          <AudioPlayer
            {src}
            name={entry.name}
            cover={url(entry, 'large')}
            meta={() => fetch(url(entry, 'meta')).then((r) => (r.ok ? r.json() : {}), () => ({}))}
            autoplay={!inline}
            {spot}
            onprev={songs[song - 1] ? () => (entry = songs[song - 1]) : undefined}
            onnext={songs[song + 1] ? () => (entry = songs[song + 1]) : undefined}
            onfail={failed}
          />
        {/key}
      {/await}
    {:else if k === 'pdf'}
      {#await import('./PdfView.svelte') then { default: PdfView }}
        {#key src}<PdfView {src} onready={ready} onfail={failed} />{/key}
      {/await}
    {:else if k === 'book'}
      <iframe class="book" src={reader} title={entry.name} bind:this={frame} onerror={failed}></iframe>
    {:else if k === 'text'}
      {#await import('./TextView.svelte') then { default: TextView }}
        {#key src}<TextView {entry} {url} onready={ready} onfail={failed} />{/key}
      {/await}
    {:else if k !== 'comic'}
      <div class="viewer-error">
        <p>{t.noPreview}</p>
        <a class="button primary" href={url(entry, 'dl')} download><Download size={18} />{t.download}</a>
      </div>
    {/if}
  </div>
</div>
{/if}
