<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye'
  import Clock from '@lucide/svelte/icons/clock'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import Preview from '../components/Preview.svelte'
  import { api, fileURL, rawURL, thumbURL, type Entry, type RecentFile } from '../lib/api'
  import { thumbable, rawThumb, arrange, days, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { folderAction, downloadAction, actOn } from '../lib/located'

  let files = $state.raw<RecentFile[]>([])
  let scanning = $state(false)
  let loaded = $state(false)
  let error = $state('')
  let sort = $state<Sort>('mtime')
  let desc = $state(true)
  let preview = $state.raw<Entry | null>(null)

  const shown = $derived(arrange(files, '', sort, desc))
  const day = $derived(files && days())
  const loc = (e: Entry) => e as RecentFile

  $effect(() => {
    let timer = 0
    const load = () =>
      api.recent().then(
        (r) => {
          files = r.entries.map((f) => ({ ...f, dir: false }))
          scanning = r.scanning
          loaded = true
          if (r.scanning) timer = setTimeout(load, 3000)
        },
        (e: Error) => (error = e.message),
      )
    load()
    return () => clearTimeout(timer)
  })

  const actions: Action[] = [{ id: 'open', label: t.open, icon: Eye }, downloadAction, folderAction]

  function onaction(id: string, e: Entry | null) {
    if (!e) return
    const f = loc(e)
    if (id === 'open') preview = e
    else actOn(id, f)
  }
</script>

{#snippet recentEmpty()}
  <EmptyState icon={Clock} title={scanning ? t.recentScanning : t.recentEmpty} />
{/snippet}

<section class="files" aria-label={t.recent}>
  {#if error}<p class="error banner">{error}</p>{/if}
  <EntryList
    entries={shown}
    grid={false}
    bind:sort
    bind:desc
    thumb={(e) => (thumbable(e.name) ? thumbURL(loc(e).vol, loc(e).path) : null)}
    raw={(e) => (rawThumb(e) ? rawURL(loc(e).vol, loc(e).path) : null)}
    actions={(e) => (e ? actions : [])}
    {onaction}
    onopen={(e) => (preview = e)}
    loading={!loaded && !error}
    empty={recentEmpty}
    id={(e) => `${loc(e).vol}:${loc(e).path}`}
    loc={loc}
    group={sort === 'mtime' ? (e) => day(e.mtime) : undefined}
  />
</section>

{#if preview}
  <Preview bind:entry={preview} entries={shown} siblings={false} url={(e, as) => fileURL(loc(e).vol, loc(e).path, as)} onclose={() => (preview = null)} />
{/if}
