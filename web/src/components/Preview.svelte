<script lang="ts">
  import X from '@lucide/svelte/icons/x'
  import Download from '@lucide/svelte/icons/download'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
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

  $effect(() => {
    if (k !== 'text') return
    let stale = false
    text = null
    fetch(src, { headers: { Range: `bytes=0-${LIMIT - 1}` } })
      .then((r) => {
        if (!stale) partial = Number(r.headers.get('Content-Range')?.split('/')[1] ?? 0) > LIMIT
        return r.text()
      })
      .then((s) => !stale && (text = s))
    return () => {
      stale = true
    }
  })

  function step(d: number) {
    if (images.length > 1) entry = images[(at + d + images.length) % images.length]
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
    else if (e.key === 'ArrowLeft') step(-1)
    else if (e.key === 'ArrowRight') step(1)
  }
</script>

<svelte:window onkeydown={key} />

<div class="viewer" role="dialog" aria-modal="true" aria-label={entry.name}>
  <header>
    <span class="title">{entry.name}</span>
    {#if images.length > 1}<span class="hint">{at + 1} / {images.length}</span>{/if}
    <a class="icon-btn" href={url(entry, true)} download aria-label={t.download}><Download size={20} /></a>
    <button class="icon-btn" onclick={onclose} aria-label={t.close}><X size={20} /></button>
  </header>
  <div class="body">
    {#if k === 'image'}
      <img src={src} alt={entry.name} />
      {#if images.length > 1}
        <button class="icon-btn step prev" onclick={() => step(-1)} aria-label={t.prev}><ChevronLeft size={28} /></button>
        <button class="icon-btn step next" onclick={() => step(1)} aria-label={t.next}><ChevronRight size={28} /></button>
      {/if}
    {:else if k === 'video'}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video src={src} controls autoplay playsinline></video>
    {:else if k === 'audio'}
      <audio src={src} controls autoplay></audio>
    {:else if k === 'pdf'}
      <iframe src={src} title={entry.name}></iframe>
    {:else if k === 'text'}
      <pre>{text ?? '…'}</pre>
      {#if partial}<p class="hint">{t.truncated}</p>{/if}
    {:else}
      <p>{t.noPreview}</p>
    {/if}
  </div>
</div>
