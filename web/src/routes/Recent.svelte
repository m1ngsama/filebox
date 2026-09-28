<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye'
  import Download from '@lucide/svelte/icons/download'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import Preview from '../components/Preview.svelte'
  import { api, filesURL, rawURL, thumbURL, type Entry, type RecentFile } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { thumbable, arrange, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'

  let files = $state<RecentFile[]>([])
  let scanning = $state(false)
  let loaded = $state(false)
  let error = $state('')
  let sort = $state<Sort>('mtime')
  let desc = $state(true)
  let preview = $state<Entry | null>(null)

  const shown = $derived(arrange(files, '', sort, desc))
  const loc = (e: Entry) => e as RecentFile
  const parent = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '')

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

  const actions: Action[] = [
    { id: 'open', label: t.open, icon: Eye },
    { id: 'download', label: t.download, icon: Download },
    { id: 'folder', label: t.openFolder, icon: FolderOpen },
  ]

  function onaction(id: string, e: Entry | null) {
    if (!e) return
    const f = loc(e)
    if (id === 'open') preview = e
    else if (id === 'folder') navigate(`${filesURL(f.vol, parent(f.path))}?details=${encodeURIComponent(f.name)}`)
    else {
      const a = document.createElement('a')
      a.href = rawURL(f.vol, f.path, true)
      a.download = ''
      a.click()
    }
  }
</script>

<section class="files" aria-label={t.recent}>
  {#if error}<p class="error banner">{error}</p>{/if}
  <EntryList
    entries={shown}
    grid={false}
    bind:sort
    bind:desc
    thumb={(e) => (thumbable(e.name) ? thumbURL(loc(e).vol, loc(e).path) : null)}
    actions={(e) => (e ? actions : [])}
    {onaction}
    onopen={(e) => (preview = e)}
    empty={!loaded ? '' : scanning ? t.recentScanning : t.recentEmpty}
    id={(e) => `${loc(e).vol}:${loc(e).path}`}
    sub={(e) => `${loc(e).vol}:/${parent(loc(e).path)}`}
  />
</section>

{#if preview}
  <Preview bind:entry={preview} entries={shown} url={(e, dl) => rawURL(loc(e).vol, loc(e).path, dl)} onclose={() => (preview = null)} />
{/if}
