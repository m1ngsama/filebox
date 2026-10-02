<script lang="ts">
  import Images from '@lucide/svelte/icons/images'
  import Play from '@lucide/svelte/icons/play'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import EmptyState from '../components/EmptyState.svelte'
  import { api, fileURL, thumbURL, type Entry, type Photo } from '../lib/api'
  import { dated, kind, place, parent } from '../lib/format'
  import { t } from '../lib/i18n'

  type Item = Entry & Photo
  let items = $state.raw<Item[]>([])
  let more = $state(true)
  let busy = $state(false)
  let loaded = $state(false)
  let scanning = $state(false)
  let error = $state('')
  let preview = $state.raw<Entry | null>(null)
  let end = $state<HTMLElement>()

  async function load() {
    if (busy || !more) return
    busy = true
    const last = items.at(-1)
    try {
      const r = await api.photos(last ? `${last.taken}.${last.id}` : '')
      items = [...items, ...r.photos.map((p) => ({ ...p, dir: false }))]
      more = r.more
      scanning = r.scanning
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
    busy = false
    loaded = true
  }

  $effect(() => {
    if (!end) return
    const io = new IntersectionObserver((es) => es.some((e) => e.isIntersecting) && load(), { rootMargin: '1200px' })
    io.observe(end)
    return () => io.disconnect()
  })

  const days = $derived.by(() => {
    const day = dated()
    const out: { day: string; items: Item[] }[] = []
    for (const p of items) {
      const d = day(p.taken)
      if (out.at(-1)?.day === d) out.at(-1)!.items.push(p)
      else out.push({ day: d, items: [p] })
    }
    return out
  })
  const loc = (e: Entry) => e as Item
</script>

{#if loaded && !items.length}
  <div class="page">
    {#if error}
      <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error}><button class="primary" onclick={() => ((more = true), load())}>{t.retry}</button></EmptyState>
    {:else}
      <EmptyState icon={Images} as="h2" title={scanning ? t.sizeIndexing : t.photosEmpty} hint={scanning ? '' : t.photosEmptyHint} />
    {/if}
  </div>
{:else}
  <section class="timeline" aria-label={t.photos} aria-busy={busy}>
    {#each days as d (d.items[0].id)}
      <h2 class="timeline-day">{d.day}</h2>
      <ul class="timeline-grid">
        {#each d.items as p (p.id)}
          <li>
            <button class="timeline-tile" title={`${p.name}\n${place(p.vol, parent(p.path))}`} onclick={() => (preview = p)}>
              <img src={thumbURL(p.vol, p.path)} alt={p.name} loading="lazy" decoding="async" />
              {#if kind(p.name) === 'video'}<span class="card-badge"><Play size={14} /></span>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/each}
    <div bind:this={end} class="timeline-end">{#if busy}<span class="hint">{t.loading}</span>{/if}</div>
  </section>
{/if}

{#if preview}
  {#await import('../components/Preview.svelte') then { default: Preview }}
    <Preview bind:entry={preview} entries={items} siblings={false} url={(e, as) => fileURL(loc(e).vol, loc(e).path, as)} onclose={() => (preview = null)} />
  {/await}
{/if}
