<script lang="ts">
  import { onMount } from 'svelte'
  import X from '@lucide/svelte/icons/x'
  import Download from '@lucide/svelte/icons/download'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import type { Entry } from '../lib/api'
  import { kind } from '../lib/format'
  import { t } from '../lib/i18n'

  let {
    entry = $bindable(),
    entries,
    url,
    onclose,
  }: { entry: Entry; entries: Entry[]; url: (e: Entry, dl?: boolean) => string; onclose: () => void } = $props()
  const k = $derived(kind(entry.name))
  const src = $derived(url(entry))
  const images = $derived(k === 'image' ? entries.filter((e) => !e.dir && kind(e.name) === 'image') : [])
  const at = $derived(images.findIndex((e) => e.name === entry.name))
  const LIMIT = 1 << 20
  let text = $state<string | null>(null)
  let partial = $state(false)
  let status = $state<'loading' | 'ready' | 'error'>('loading')
  let root = $state<HTMLDivElement>()
  let closer = $state<HTMLButtonElement>()

  $effect(() => {
    src
    status = k === 'audio' || !k ? 'ready' : 'loading'
  })

  $effect(() => {
    if (k !== 'text') return
    let stale = false
    text = null
    fetch(src, { headers: { Range: `bytes=0-${LIMIT - 1}` } })
      .then((r) => {
        if (!r.ok) throw new Error(String(r.status))
        if (!stale) partial = Number(r.headers.get('Content-Range')?.split('/')[1] ?? 0) > LIMIT
        return r.text()
      })
      .then(
        (s) => {
          if (stale) return
          text = s
          status = 'ready'
        },
        () => !stale && (status = 'error'),
      )
    return () => {
      stale = true
    }
  })

  onMount(() => {
    const back = document.activeElement as HTMLElement | null
    closer?.focus()
    return () => back?.focus()
  })

  function step(d: number) {
    if (images.length > 1) entry = images[(at + d + images.length) % images.length]
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
    else if (e.key === 'ArrowLeft') step(-1)
    else if (e.key === 'ArrowRight') step(1)
    else if (e.key === 'Tab' && root) {
      const f = [...root.querySelectorAll<HTMLElement>('a[href], button, video, audio, iframe, pre')].filter((x) => x.offsetParent)
      const i = f.indexOf(document.activeElement as HTMLElement)
      const to = e.shiftKey ? (i <= 0 ? f.length - 1 : i - 1) : i === f.length - 1 ? 0 : i + 1
      e.preventDefault()
      f[to]?.focus()
    }
  }

  const ready = () => (status = 'ready')
  const failed = () => (status = 'error')
</script>

<svelte:window onkeydown={key} />

<div class="viewer" role="dialog" aria-modal="true" aria-label={entry.name} bind:this={root}>
  <header>
    <span class="title">{entry.name}</span>
    {#if images.length > 1}<span class="hint">{at + 1} / {images.length}</span>{/if}
    <a class="icon-btn" href={url(entry, true)} download aria-label={t.download}><Download size={20} /></a>
    <button class="icon-btn" onclick={onclose} aria-label={t.close} bind:this={closer}><X size={20} /></button>
  </header>
  <div class="body" aria-busy={status === 'loading'}>
    {#if status === 'loading'}<div class="spinner" role="status" aria-label={t.loading}></div>{/if}
    {#if status === 'error'}
      <div class="viewer-error" role="alert">
        <CircleAlert size={40} aria-hidden="true" />
        <p>{k === 'video' ? t.cantPlay : t.previewFailed}</p>
        <a class="button primary" href={url(entry, true)} download><Download size={18} />{t.download}</a>
      </div>
    {:else if k === 'image'}
      <img src={src} alt={entry.name} class:dim={status === 'loading'} onload={ready} onerror={failed} />
    {:else if k === 'video'}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video src={src} controls autoplay playsinline preload="metadata" onloadedmetadata={ready} onerror={failed}></video>
    {:else if k === 'audio'}
      <audio src={src} controls autoplay onerror={failed}></audio>
    {:else if k === 'pdf'}
      <iframe src={src} title={entry.name} onload={ready}></iframe>
    {:else if k === 'text'}
      {#if text !== null}<pre tabindex="-1">{text}</pre>{/if}
      {#if partial}<p class="hint">{t.truncated}</p>{/if}
    {:else}
      <div class="viewer-error">
        <p>{t.noPreview}</p>
        <a class="button primary" href={url(entry, true)} download><Download size={18} />{t.download}</a>
      </div>
    {/if}
    {#if k === 'image' && images.length > 1}
      <button class="icon-btn step prev" onclick={() => step(-1)} aria-label={t.prev}><ChevronLeft size={28} /></button>
      <button class="icon-btn step next" onclick={() => step(1)} aria-label={t.next}><ChevronRight size={28} /></button>
    {/if}
  </div>
</div>
