<script lang="ts">
  import { onMount } from 'svelte'
  import X from '@lucide/svelte/icons/x'
  import Download from '@lucide/svelte/icons/download'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import type { Entry, Src } from '../lib/api'
  import { kind, look, thumbable, subtitleOf, subtitleLang } from '../lib/format'
  import { load, save } from '../lib/storage'
  import '../lib/render.css'
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
  const list = $derived(inline ? [] : entries.filter((e) => !e.dir && kind(e.name)))
  const at = $derived(list.indexOf(entry))
  const step = (d: number) => at >= 0 && list[at + d] && (entry = list[at + d])
  let pages = $state<Entry[] | null>(null)
  let rtl = $state(false)
  const folder = $derived('rtl:' + src.slice(0, src.lastIndexOf('/')))
  const pageURL: Src = (e, as) => (as === 'dl' ? url(entry, 'dl') : `${url(entry, 'zip-entry')}&e=${encodeURIComponent(e.name)}`)
  const LIMIT = 1 << 20
  const tracks = $derived(
    k === 'video' && siblings
      ? entries
          .filter((e) => !e.dir && subtitleOf(entry.name, e.name))
          .map((e) => ({ src: url(e) + '?vtt', lang: subtitleLang(entry.name, e.name) }))
      : [],
  )
  const spot = $derived('pos:' + src)
  let last = 0
  let audioOnly = $state(false)
  const md = $derived(/\.(md|markdown)$/i.test(entry.name))
  const rich = $derived(k === 'text' && (md || look(entry.name) === 'code'))
  let text = $state<string | null>(null)
  let html = $state<string | null>(null)
  let partial = $state(false)
  let plain = $state('')
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

  $effect(() => {
    if (k !== 'text') return
    let stale = false
    const r = rich
    text = html = null
    fetch(r ? url(entry, 'render') : src, r ? {} : { headers: { Range: `bytes=0-${LIMIT - 1}` } })
      .then((res) => {
        if (!res.ok) throw new Error(String(res.status))
        if (stale) return res.text()
        partial = r ? res.headers.has('X-Truncated') : Number(res.headers.get('Content-Range')?.split('/')[1] ?? 0) > LIMIT
        plain = res.headers.get('X-Plain') ?? ''
        return res.text()
      })
      .then(
        (s) => {
          if (stale) return
          if (r) html = s
          else text = s
          status = 'ready'
        },
        () => !stale && (status = 'error'),
      )
    return () => {
      stale = true
    }
  })

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
    else if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && k !== 'book' && !e.metaKey && !e.ctrlKey && !e.altKey && !(e.target instanceof HTMLMediaElement))
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
    const one = e.touches.length === 1 && k !== 'text' && k !== 'pdf' && k !== 'book' && !zoomed && !inline
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

  const ready = () => (status = 'ready')
  const failed = () => (status = 'error')

  async function resume(e: Event) {
    const m = e.currentTarget as HTMLVideoElement
    if (k === 'video' && !m.videoWidth) {
      const meta: { width?: number } = await fetch(url(entry, 'meta')).then((r) => (r.ok ? r.json() : {}), () => ({}))
      if (meta.width) return failed()
      audioOnly = true
    }
    const at = Number(load(spot))
    if (at > 5 && at < m.duration - 5) m.currentTime = at
    ready()
  }

  function track(e: Event) {
    const now = (e.currentTarget as HTMLMediaElement).currentTime
    if (e.type !== 'timeupdate' || Math.abs(now - last) > 5) save(spot, String(Math.floor((last = now))))
  }
</script>

<svelte:window onkeydown={key} />

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
          book={{ title: entry.name, rtl, onpage: (i) => save(spot, String(i)), onrtl: flip }}
        />
      {/key}
    {/if}
  {/await}
{:else}
<div
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
      <span class="title">{entry.name}</span>
      {#if list.length > 1}
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
      <!-- svelte-ignore a11y_media_has_caption -->
      <video
        {src}
        poster={thumbable(entry.name) ? url(entry, 'thumb') : undefined}
        controls
        autoplay={!inline}
        playsinline
        preload="metadata"
        class:audio-only={audioOnly}
        onloadedmetadata={resume}
        ontimeupdate={track}
        onpause={track}
        onended={() => save(spot, '')}
        onerror={failed}
      >
        {#each tracks as s, i (s.src)}
          <track kind="subtitles" src={s.src} label={s.lang || t.subtitles} srclang={/^[a-z]{2,3}(-[a-z0-9]+)*$/i.test(s.lang) ? s.lang : undefined} default={i === 0} />
        {/each}
      </video>
    {:else if k === 'audio'}
      <audio {src} controls autoplay={!inline} preload="metadata" onloadedmetadata={resume} ontimeupdate={track} onpause={track} onended={() => save(spot, '')} onerror={failed}></audio>
    {:else if k === 'pdf'}
      <iframe src={src} title={entry.name} onload={ready}></iframe>
    {:else if k === 'book'}
      {#await import('./Reader.svelte') then { default: Reader }}
        {#key src}<Reader list={url(entry, 'zip-entries')} entry={url(entry, 'zip-entry')} {spot} onready={ready} onfail={failed} />{/key}
      {/await}
    {:else if k === 'text'}
      {#if html !== null}
        <article class="doc" class:code={!md} tabindex="-1">{@html html}</article>
      {:else if text !== null}<pre tabindex="-1">{text}</pre>{/if}
      {#if plain}<p class="hint">{plain === 'large' ? t.tooLarge : t.tooComplex}</p>{/if}
      {#if partial}<p class="hint">{t.truncated}</p>{/if}
    {:else if k !== 'comic'}
      <div class="viewer-error">
        <p>{t.noPreview}</p>
        <a class="button primary" href={url(entry, 'dl')} download><Download size={18} />{t.download}</a>
      </div>
    {/if}
  </div>
</div>
{/if}
