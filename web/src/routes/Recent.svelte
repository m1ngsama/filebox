<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import Clock from '@lucide/svelte/icons/clock'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import Preview from '../components/Preview.svelte'
  import { api, fileURL, filesURL, rawURL, thumbURL, type Entry, type RecentFile, type Run } from '../lib/api'
  import { thumbable, rawThumb, arrange, days, base, type Sort } from '../lib/format'
  import { navigate } from '../lib/router.svelte'
  import { t } from '../lib/i18n'
  import { folderAction, downloadAction, actOn } from '../lib/located'
  import { fail } from '../lib/toast.svelte'

  type Item = RecentFile & { run?: number; key?: string; head?: boolean }
  let files = $state.raw<Item[]>([])
  let runs = $state.raw<Run[]>([])
  let open = $state.raw<Run[]>([])
  const same = (a: Run, b: Run) => a.vol === b.vol && a.dir === b.dir && a.oldest <= b.mtime && b.oldest <= a.mtime
  const isOpen = (r: Run) => open.some((o) => same(o, r))
  const keyOf = (r: Run, i: number) => `\0${r.vol}\0${r.dir}\0${runs.slice(0, i).filter((x) => x.vol === r.vol && x.dir === r.dir).length}`
  let scanning = $state(false)
  let loaded = $state(false)
  let error = $state('')
  let sort = $state<Sort>('mtime')
  let desc = $state(true)
  let preview = $state.raw<Entry | null>(null)

  const heads = $derived(runs.map((r, i): Item => ({ name: base(r.dir) || r.vol, dir: true, size: r.size, mtime: r.mtime, vol: r.vol, path: r.dir, run: i, key: keyOf(r, i), head: true })))
  const shown = $derived.by(() => {
    const d = desc ? -1 : 1
    const order = (xs: Item[]) => (sort === 'mtime' ? [...xs].sort((a, b) => d * (a.mtime - b.mtime)) : arrange(xs, '', sort, desc))
    const top = order([...files.filter((f) => f.run === undefined), ...heads])
    return top.flatMap((x) => (x.head && isOpen(runs[x.run!]) ? [x, ...order(files.filter((f) => f.run === x.run))] : [x]))
  })
  const day = $derived(files && days())
  const loc = (e: Entry) => e as Item

  let tries = $state(0)
  $effect(() => {
    void tries
    let timer = 0
    let parked = false
    const stop = new AbortController()
    const load = () => {
      if ((parked = document.hidden)) return
      api.recent(stop.signal).then(
        (r) => {
          if (stop.signal.aborted) return
          files = r.entries.map((f) => ({ ...f, dir: false }))
          runs = r.runs
          scanning = r.scanning
          loaded = true
          if (r.scanning) timer = setTimeout(load, 3000)
        },
        (e: Error) => !stop.signal.aborted && (loaded ? fail(e) : (error = e.message)),
      )
    }
    const wake = () => parked && load()
    document.addEventListener('visibilitychange', wake)
    load()
    return () => {
      stop.abort()
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', wake)
    }
  })

  const actions: Action[] = [{ id: 'open', label: t.open, icon: Eye }, downloadAction, folderAction]

  function onaction(id: string, e: Entry | null) {
    if (!e) return
    const f = loc(e)
    if (id === 'into') navigate(filesURL(f.vol, f.path))
    else if (id === 'open') onopen(e)
    else actOn(id, f)
  }

  function onopen(e: Entry) {
    const r = runs[loc(e).run!]
    if (!loc(e).head) preview = e
    else if (isOpen(r)) open = open.filter((o) => !same(o, r))
    else open = [...open, r]
  }
</script>

{#snippet recentEmpty()}
  {#if error}
    <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error}>
      <button
        class="primary"
        onclick={() => {
          error = ''
          tries++
        }}>{t.retry}</button
      >
    </EmptyState>
  {:else}
    <EmptyState icon={Clock} title={scanning ? t.recentScanning : t.recentEmpty} />
  {/if}
{/snippet}

<section class="files" aria-label={t.recent}>
  <EntryList
    entries={shown}
    grid={false}
    bind:sort
    bind:desc
    thumb={(e) => (thumbable(e.name) ? thumbURL(loc(e).vol, loc(e).path) : null)}
    raw={(e) => (rawThumb(e) ? rawURL(loc(e).vol, loc(e).path) : null)}
    actions={(e) => (!e ? [] : loc(e).head ? [{ id: 'into', label: t.openNamed(e.name), icon: FolderOpen }] : actions)}
    {onaction}
    {onopen}
    tag={(e) => (loc(e).head ? t.runCount(runs[loc(e).run!].count, runs[loc(e).run!].more) : undefined)}
    expanded={(e) => (loc(e).head ? isOpen(runs[loc(e).run!]) : undefined)}
    nested={(e) => !loc(e).head && loc(e).run !== undefined}
    loading={!loaded && !error}
    empty={recentEmpty}
    id={(e) => (loc(e).head ? loc(e).key! : `${loc(e).vol}:${loc(e).path}`)}
    loc={loc}
    group={sort === 'mtime' ? (e) => day(e.mtime) : undefined}
  />
</section>

{#if preview}
  <Preview bind:entry={preview} entries={shown.filter((e) => !e.head)} siblings={false} url={(e, as) => fileURL(loc(e).vol, loc(e).path, as)} onclose={() => (preview = null)} />
{/if}
