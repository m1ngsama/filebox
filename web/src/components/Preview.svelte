<script lang="ts">
  import { kind } from '../lib/format'
  import { t } from '../lib/i18n'

  let { name, url, onclose }: { name: string; url: string; onclose: () => void } = $props()
  const k = $derived(kind(name))
  const LIMIT = 1 << 20
  let text = $state<string | null>(null)
  let partial = $state(false)

  $effect(() => {
    if (k !== 'text') return
    text = null
    fetch(url, { headers: { Range: `bytes=0-${LIMIT - 1}` } })
      .then((r) => {
        partial = Number(r.headers.get('Content-Range')?.split('/')[1] ?? 0) > LIMIT
        return r.text()
      })
      .then((s) => (text = s))
  })
  const dl = $derived(url + (url.includes('?') ? '&dl' : '?dl'))
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div class="overlay" role="dialog" aria-modal="true" aria-label={name}>
  <header>
    <span class="title">{name}</span>
    <a href={dl}>{t.download}</a>
    <button onclick={onclose}>{t.close}</button>
  </header>
  <div class="body">
    {#if k === 'image'}
      <img src={url} alt={name} />
    {:else if k === 'video'}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video src={url} controls autoplay playsinline></video>
    {:else if k === 'audio'}
      <audio src={url} controls autoplay></audio>
    {:else if k === 'pdf'}
      <iframe src={url} title={name}></iframe>
    {:else if k === 'text'}
      <pre>{text ?? '…'}</pre>
      {#if partial}<p class="hint">{t.truncated}</p>{/if}
    {:else}
      <p>{t.noPreview}</p>
    {/if}
  </div>
</div>
