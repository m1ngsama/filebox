<script lang="ts">
  import { tick, untrack } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { DropdownMenu } from 'bits-ui'
  import Plus from '@lucide/svelte/icons/plus'
  import Upload from '@lucide/svelte/icons/upload'
  import FolderUp from '@lucide/svelte/icons/folder-up'
  import FolderPlus from '@lucide/svelte/icons/folder-plus'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import Download from '@lucide/svelte/icons/download'
  import Pencil from '@lucide/svelte/icons/pencil'
  import FolderInput from '@lucide/svelte/icons/folder-input'
  import Share2 from '@lucide/svelte/icons/share-2'
  import Info from '@lucide/svelte/icons/info'
  import Trash from '@lucide/svelte/icons/trash'
  import X from '@lucide/svelte/icons/x'
  import LayoutGrid from '@lucide/svelte/icons/layout-grid'
  import List from '@lucide/svelte/icons/list'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import SearchX from '@lucide/svelte/icons/search-x'
  import SquareCheck from '@lucide/svelte/icons/square-check'
  import { narrow } from '../lib/shell.svelte'
  import Search from '@lucide/svelte/icons/search'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import EllipsisVertical from '@lucide/svelte/icons/ellipsis-vertical'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import { api, filesURL, rawURL, thumbURL, zipURL, saveURL, type Entry, type Move, type RecentFile } from '../lib/api'
  import { toast, fail, runLatest } from '../lib/toast.svelte'
  import { navigate, link, route } from '../lib/router.svelte'
  import { enqueue, type Replaced } from '../lib/uploads.svelte'
  import { thumbable, rawThumb, arrange, parent, base, child, flip, sorts, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import NavToggle from '../components/NavToggle.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import type { Choice } from '../components/ConflictDialog.svelte'
  import { target, inside, type Carried, type Target } from '../lib/dnd'

  let { vol, path, vols }: { vol: string; path: string; vols: string[] } = $props()

  type Dialog = { kind: 'mkdir' } | { kind: 'rename'; e: Entry } | { kind: 'delete' | 'move'; names: string[] }

  let entries = $state.raw<Entry[]>([])
  let error = $state('')
  let at = $state('')
  let filter = $state('')
  let scope = $state<'here' | 'all'>('here')
  let hits = $state.raw<RecentFile[] | null>(null)
  let finding = $state(false)
  let partial = $state(false)
  let reveal = $state('')
  let query = $state('')
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let grid = $state(load('grid') === '1')
  let dragging = $state(false)
  let depth = 0
  let preview = $state.raw<Entry | null>(null)
  let details = $state.raw<Entry | null>(null)
  let dialog = $state<Dialog | null>(null)
  let searching = $state(false)
  let help = $state(false)
  let sheet = $state<'new' | 'more' | null>(null)
  let filterEl = $state<HTMLInputElement>()
  let opener = $state<HTMLButtonElement>()
  let barH = $state(0)
  let conflict = $state<{ name: string; rest: number; resolve: (r: [Choice, boolean] | null) => void } | null>(null)
  const selected = new SvelteSet<string>()
  let files = $state<HTMLInputElement>()
  let folder = $state<HTMLInputElement>()

  const here = $derived(`${vol}/${path}`)
  const join = (n: string) => child(path, n)
  const crumbs = $derived(path ? path.split('/') : [])
  const shown = $derived(arrange(at === here ? entries : [], query, sort, desc))
  const one = $derived(selected.size === 1 ? entries.find((e) => selected.has(e.name)) : undefined)
  const thumb = (e: Entry) => (!e.dir && thumbable(e.name) ? thumbURL(vol, join(e.name)) : null)
  const raw = (e: Entry) => (rawThumb(e) ? rawURL(vol, join(e.name)) : null)

  async function refresh() {
    const want = here
    const [list, err] = await api.ls(vol, path).then(
      (r) => [r.entries, ''] as const,
      (e: Error) => [[], e.message] as const,
    )
    if (want !== here) return false
    entries = [...list]
    error = err
    at = want
    return true
  }

  $effect(() => {
    vol
    path
    filter = ''
    searching = false
    details = null
    reveal = ''
    const focus = new URLSearchParams(untrack(() => route.search)).get('details')
    refresh().then((ok) => {
      if (ok && focus) details = entries.find((e) => e.name === focus) ?? null
    })
  })

  function closeDetails() {
    details = null
    if (new URLSearchParams(route.search).has('details')) navigate(route.path, true)
  }

  $effect(() => {
    const q = filter.trim()
    if (scope !== 'all' || [...q].length < 2) {
      hits = null
      finding = false
      return
    }
    finding = true
    let live = true
    const id = setTimeout(
      () =>
        api
          .search(q)
          .then((r) => live && ((hits = r.entries), (partial = r.scanning)), (e: Error) => live && fail(e))
          .finally(() => live && (finding = false)),
      200,
    )
    return () => {
      live = false
      clearTimeout(id)
    }
  })

  $effect(() => {
    const name = new URLSearchParams(route.search).get('select')
    if (!name || at !== here) return
    untrack(async () => {
      filter = query = ''
      searching = false
      await tick()
      if (entries.some((e) => e.name === name)) {
        selected.clear()
        selected.add(name)
        reveal = name
      }
      navigate(route.path, true)
    })
  })

  const hitLoc = (e: Entry) => e as RecentFile

  function locate(e: Entry) {
    const h = hitLoc(e)
    navigate(`${filesURL(h.vol, parent(h.path))}?select=${encodeURIComponent(h.name)}`)
  }

  const hitActions: Action[] = [
    { id: 'folder', label: t.openFolder, icon: FolderOpen },
    { id: 'download', label: t.download, icon: Download },
  ]

  function onhit(id: string, e: Entry | null) {
    if (!e) return
    const h = hitLoc(e)
    if (id === 'folder') locate(e)
    else saveURL(h.dir ? zipURL(h.vol, [h.path], h.name) : rawURL(h.vol, h.path, true))
  }

  $effect(() => {
    const f = filter
    if (!f) return void (query = '')
    const id = setTimeout(() => (query = f), 120)
    return () => clearTimeout(id)
  })

  $effect(() => {
    query
    vol
    path
    selected.clear()
  })

  $effect(() => save('grid', grid ? '1' : '0'))

  $effect(() => {
    const s = document.documentElement.style
    if (selected.size && barH) s.setProperty('--bar-h', `${barH}px`)
    else s.removeProperty('--bar-h')
    return () => s.removeProperty('--bar-h')
  })

  function open(e: Entry) {
    if (e.dir) navigate(filesURL(vol, join(e.name)))
    else preview = e
  }

  function download(names: string[]) {
    const hit = names.length === 1 ? entries.find((e) => e.name === names[0]) : undefined
    if (hit && !hit.dir) saveURL(rawURL(vol, join(hit.name), true))
    else saveURL(zipURL(vol, names.map(join), t.zipName(hit ? hit.name : (crumbs.at(-1) ?? vol), names.length)))
  }

  const ask = (name: string, rest: number) => new Promise<[Choice, boolean] | null>((resolve) => (conflict = { name, rest, resolve }))

  async function upload(list: FileList | null | undefined, asFolder = false) {
    if (!list?.length) return
    const v = vol
    const dir = path
    let items = [...list].map((file) => ({ file, rel: asFolder ? file.webkitRelativePath : '' }))
    const top = (x: { file: File; rel: string }) => (x.rel ? x.rel.slice(0, x.rel.indexOf('/')) : x.file.name)
    const taken = new Set((await api.ls(v, dir).catch(() => ({ entries }))).entries.map((e) => e.name))
    const clash = [...new Set(items.map(top))].filter((n) => taken.has(n))
    const choices = new Map<string, Choice>()
    let every: Choice | null = null
    for (const [i, n] of clash.entries()) {
      if (!every) {
        const r = await ask(n, clash.length - i - 1)
        conflict = null
        if (!r) return
        if (r[1]) every = r[0]
        choices.set(n, r[0])
      } else choices.set(n, every)
    }
    const renamed = new Map<string, string>()
    for (const [n, c] of choices) {
      if (c !== 'keep' || !asFolder) continue
      let k = 1
      while (taken.has(`${n} (${k})`)) k++
      renamed.set(n, `${n} (${k})`)
      taken.add(`${n} (${k})`)
    }
    items = items
      .filter((x) => choices.get(top(x)) !== 'skip')
      .map((x) => (renamed.has(top(x)) ? { ...x, rel: renamed.get(top(x)) + x.rel.slice(x.rel.indexOf('/')) } : x))
    const meta = { vol: v, dir: dir || '/' }
    const over = items.filter((x) => choices.get(top(x)) === 'replace')
    if (over.length) enqueue(over, '/upload/', { ...meta, overwrite: '1' }, refresh, (rs) => undo(() => unreplace(rs)))
    const rest = items.filter((x) => choices.get(top(x)) !== 'replace')
    if (rest.length) enqueue(rest, '/upload/', meta, refresh)
  }

  const creators = [
    { label: t.upload, icon: Upload, run: () => files?.click() },
    { label: t.uploadFolder, icon: FolderUp, run: () => folder?.click() },
    { label: t.newFolder, icon: FolderPlus, run: () => (dialog = { kind: 'mkdir' }) },
  ]

  function sortBy(k: Sort) {
    desc = flip(sort, desc, k)
    sort = k
  }

  async function search() {
    searching = true
    await tick()
    filterEl?.focus()
  }

  async function endSearch() {
    searching = false
    filter = ''
    await tick()
    if (narrow.current) opener?.focus()
  }

  const act = {
    select: { id: 'select', label: t.selectItem, icon: SquareCheck },
    open: { id: 'open', label: t.open, icon: FolderOpen },
    download: { id: 'download', label: t.download, icon: Download },
    rename: { id: 'rename', label: t.rename, icon: Pencil },
    move: { id: 'move', label: t.moveOrCopy, icon: FolderInput },
    share: { id: 'share', label: t.share, icon: Share2 },
    details: { id: 'details', label: t.details, icon: Info },
    remove: { id: 'remove', label: t.remove, icon: Trash, danger: true },
    mkdir: { id: 'mkdir', label: t.newFolder, icon: FolderPlus },
    upload: { id: 'upload', label: t.upload, icon: Upload },
  } satisfies Record<string, Action>

  const actions = (e: Entry | null): Action[] =>
    !e ? [act.mkdir, act.upload]
    : [...(narrow.current ? [act.select] : []), act.open, act.download, act.rename, act.move, act.share, act.details, act.remove]

  function onaction(id: string, e: Entry | null) {
    if (id === 'mkdir') dialog = { kind: 'mkdir' }
    else if (id === 'upload') files?.click()
    else if (!e) return
    else if (id === 'select') selected.add(e.name)
    else if (id === 'open') open(e)
    else if (id === 'download') download([e.name])
    else if (id === 'rename') dialog = { kind: 'rename', e }
    else if (id === 'move') dialog = { kind: 'move', names: [e.name] }
    else if (id === 'remove') dialog = { kind: 'delete', names: [e.name] }
    else details = e
  }

  type Failed = { name: string; error: Error }

  const undo = (run: () => Promise<Failed[]>) => ({
    label: t.undo,
    keys: 'Control+Z Meta+Z',
    run: () =>
      run()
        .then((bad) => {
          if (!bad.length) toast(t.undone)
          else if (bad.length === 1) fail(new Error(t.failedItem(t.undoFailed(t.what([bad[0].name])), bad[0].error.message)))
          else fail(new Error(t.undoFailed(bad.map((b) => t.what([b.name])).join('、'))))
        }, fail)
        .finally(refresh),
  })

  async function reverse(moves: Move[]) {
    if (moves.some((m) => m.from.vol !== m.to.vol)) toast(t.undoing, { kind: 'info' })
    const bad: Failed[] = []
    for (const m of [...moves].reverse()) await api.move(m.to, m.from).catch((error) => bad.push({ name: base(m.from.path), error }))
    return bad
  }

  async function restore(v: string, items: { path: string; id: string }[]) {
    const res = await Promise.allSettled(items.map((x) => api.restore(v, x.id)))
    return items.flatMap((x, i) => (res[i].status === 'rejected' ? [{ name: base(x.path), error: res[i].reason as Error }] : []))
  }

  async function unreplace(rs: Replaced[]) {
    const bad: Failed[] = []
    for (const r of [...rs].reverse()) {
      const gone = await api.rm(r.vol, [r.path]).then(
        (x) => x.failed.map((f) => ({ name: base(r.path), error: new Error(f.error) })),
        (error: Error) => [{ name: base(r.path), error }],
      )
      bad.push(...(gone.length ? gone : await restore(r.vol, [r])))
    }
    return bad
  }

  function moved(done: Move[], copy: boolean) {
    selected.clear()
    closeDetails()
    refresh()
    if (!done.length) return
    const w = t.what(done.map((m) => base(m.from.path)))
    if (copy) toast(t.copiedTo(w))
    else toast(t.movedTo(w, `${done[0].to.vol}:/${parent(done[0].to.path)}`), { action: undo(() => reverse(done)) })
  }

  async function dropInto(to: { vol: string; path: string }, c: Carried, copy: boolean) {
    const r = await api.transfer(c.vol, c.dir, c.names, to, copy)
    moved(r.done, copy)
    if (r.error) fail(r.error)
  }

  const into = (to: { vol: string; path: string }, spring?: () => void): Target => ({
    accepts: (c) => !inside(c, to),
    drop: (c, copy) => dropInto(to, c, copy),
    spring,
  })

  const dnd = {
    carry: (e: Entry) => ({ vol, dir: path, names: selected.has(e.name) ? [...selected] : [e.name] }),
    target: (e: Entry) => into({ vol, path: join(e.name) }, () => open(e)),
  }

  async function pass(id: string) {
    const e = one
    selected.clear()
    await tick()
    document.querySelector<HTMLElement>('[role=grid] [tabindex="0"]')?.focus()
    if (e) onaction(id, e)
  }

  function focused() {
    const i = (document.activeElement as HTMLElement | null)?.closest<HTMLElement>('[data-i]')?.dataset.i
    return i === undefined ? undefined : shown[+i]
  }

  function inList(el: Element) {
    const at = getSelection()?.anchorNode
    return !!el.closest('.files') || (el.matches('body, main') && (!at || !!at.parentElement?.closest('.files')))
  }

  function keydown(e: KeyboardEvent) {
    if (document.querySelector('[role=dialog], [role=menu]')) return
    const typing = (e.target as Element).matches?.('input:not([type=checkbox], [type=radio]), select, textarea, [contenteditable]')
    const mod = e.metaKey || e.ctrlKey
    const k = e.key.toLowerCase()
    const target = one ?? focused()
    if (e.key === 'Escape') {
      if (details) closeDetails()
      else if (searching || filter) endSearch()
      else selected.clear()
    } else if (typing || e.altKey) return
    else if (mod && !e.shiftKey && !e.repeat && k === 'z' && runLatest(t.undo)) e.preventDefault()
    else if (mod && !e.shiftKey && k === 'a' && inList(e.target as Element)) {
      e.preventDefault()
      for (const x of shown) selected.add(x.name)
    } else if (mod) return
    else if (e.key === '/' || e.key === '?' || e.key === 'n' || e.key === 'u' || (e.key === 'F2' && target)) {
      e.preventDefault()
      if (e.key === '/') search()
      else if (e.key === '?') help = true
      else if (e.key === 'n') dialog = { kind: 'mkdir' }
      else if (e.key === 'u') files?.click()
      else if (target) dialog = { kind: 'rename', e: target }
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && selected.size) dialog = { kind: 'delete', names: [...selected] }
    else if (e.key === 'Enter' && selected.size === 1 && !(e.target as Element).closest('button, a, [role=grid]')) {
      const hit = entries.find((x) => selected.has(x.name))
      if (hit) open(hit)
    }
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer?.types.includes('Files')
</script>

<svelte:window onkeydowncapture={keydown} ondragover={(e) => e.preventDefault()} ondrop={(e) => e.preventDefault()} />

{#snippet batch()}
  <button class="ghost" onclick={() => download([...selected])}>
    <Download size={16} />{t.download}
  </button>
  <button class="ghost" onclick={() => (dialog = { kind: 'move', names: [...selected] })}><FolderInput size={16} />{t.moveOrCopy}</button>
  <button class="ghost danger" onclick={() => (dialog = { kind: 'delete', names: [...selected] })}><Trash size={16} />{t.remove}</button>
  <button class="icon-btn" aria-label={t.clearSelection} onclick={() => selected.clear()}><X size={16} /></button>
{/snippet}

<div class="files-wrap">
  <section
    class="files"
    class:selecting={selected.size > 0}
    aria-label={vol}
    ondragenter={(e) => {
      if (!hasFiles(e)) return
      depth++
      dragging = true
    }}
    ondragleave={() => {
      if (--depth <= 0) {
        depth = 0
        dragging = false
      }
    }}
    ondrop={(e) => {
      depth = 0
      dragging = false
      upload(e.dataTransfer?.files)
    }}
  >
    <header class="bar" class:searching>
      <NavToggle />
      {#if crumbs.length}
        {@const up = crumbs.length > 1 ? crumbs[crumbs.length - 2] : vol}
        <a class="up" href={filesURL(vol, parent(path))} onclick={link} aria-label={t.upTo(up)}><ChevronLeft size={20} /><span>{up}</span></a>
      {/if}
      <h1 class="title">{crumbs.length ? crumbs[crumbs.length - 1] : vol}</h1>
      <nav class="crumbs" aria-label={t.breadcrumb}>
        <a href={filesURL(vol, '')} onclick={link} use:target={into({ vol, path: '' })}>{vol}</a>
        {#each crumbs as c, i}
          {@const to = crumbs.slice(0, i + 1).join('/')}
          <ChevronRight size={16} />
          <a href={filesURL(vol, to)} onclick={link} use:target={into({ vol, path: to })} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</a>
        {/each}
      </nav>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class="primary new"><Plus size={18} />{t.new}</DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content class="menu" preventScroll={false} align="start" sideOffset={4}>
            {#each creators as c, i (c.label)}
              {#if i === 2}<DropdownMenu.Separator class="menu-sep" />{/if}
              <DropdownMenu.Item class="menu-item" onSelect={c.run}><c.icon size={16} />{c.label}</DropdownMenu.Item>
            {/each}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      <span class="grow"></span>
      <div class="find">
        <label for="filter" class="sr-only">{t.filter}</label>
        <input
          id="filter"
          class="filter"
          type="search"
          bind:value={filter}
          bind:this={filterEl}
          placeholder={scope === 'all' ? t.searchAll : t.filter}
          onkeydown={(e) => {
            if (e.key === 'Enter' && hits?.length) locate(hits[0])
          }}
        />
        <div class="scope" role="radiogroup" aria-label={t.searchScope}>
          {#each [['here', t.scopeHere], ['all', t.scopeAll]] as const as [k, label] (k)}
            <button
              type="button"
              role="radio"
              aria-checked={scope === k}
              onclick={() => {
                scope = k
                filterEl?.focus()
              }}>{label}</button
            >
          {/each}
        </div>
      </div>
      <button class="icon-btn search-open" aria-label={t.openFilter} onclick={search} bind:this={opener}><Search size={20} /></button>
      <button class="icon-btn search-close" aria-label={t.closeFilter} onclick={endSearch}><X size={20} /></button>
      <button class="icon-btn view" aria-label={grid ? t.listView : t.gridView} title={grid ? t.listView : t.gridView} onclick={() => (grid = !grid)}>
        {#if grid}<List size={20} />{:else}<LayoutGrid size={20} />{/if}
      </button>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class="icon-btn overflow" aria-label={t.more}><EllipsisVertical size={20} /></DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content class="menu" preventScroll={false} align="end" sideOffset={4}>
            <DropdownMenu.Item class="menu-item" onSelect={() => (grid = !grid)}>
              {#if grid}<List size={16} />{t.listView}{:else}<LayoutGrid size={16} />{t.gridView}{/if}
            </DropdownMenu.Item>
            <DropdownMenu.Separator class="menu-sep" />
            <DropdownMenu.Group>
              <DropdownMenu.GroupHeading class="menu-label">{t.sortBy}</DropdownMenu.GroupHeading>
              {#each sorts as [k, label] (k)}
                <DropdownMenu.Item class="menu-item" aria-current={sort === k || undefined} onSelect={() => sortBy(k)}>
                  {#if sort !== k}<span class="menu-gap"></span>{:else if desc}<ArrowDown size={16} />{:else}<ArrowUp size={16} />{/if}{label}
                </DropdownMenu.Item>
              {/each}
            </DropdownMenu.Group>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </header>

    {#if error && at === here}<p class="error banner">{error}</p>{/if}

    {#if hits}
      {#if partial}<p class="hint banner">{t.indexing}</p>{/if}
      <EntryList
        entries={hits}
        grid={false}
        thumb={(e) => (!e.dir && thumbable(e.name) ? thumbURL(hitLoc(e).vol, hitLoc(e).path) : null)}
        raw={(e) => (rawThumb(e) ? rawURL(hitLoc(e).vol, hitLoc(e).path) : null)}
        actions={(e) => (e ? hitActions : [])}
        onaction={onhit}
        onopen={locate}
        loading={finding}
        id={(e) => `${hitLoc(e).vol}:${hitLoc(e).path}`}
        loc={hitLoc}
      >
        {#snippet empty()}
          {#if !finding}<EmptyState icon={SearchX} title={t.noResults} hint={t.noResultsHint} />{/if}
        {/snippet}
      </EntryList>
    {:else}
    {#key here}
      <EntryList
        entries={shown}
        {grid}
        {selected}
        bind:sort
        bind:desc
        {thumb}
        {raw}
        {actions}
        {onaction}
        onopen={open}
        {batch}
        {dnd}
        {reveal}
        loading={at !== here}
      >
        {#snippet empty()}
          {#if query}
            <EmptyState icon={SearchX} title={t.noMatch} hint={t.noMatchHint}>
              <button onclick={() => (filter = '')}>{t.clearFilter}</button>
            </EmptyState>
          {:else if !error}
            <EmptyState icon={FolderOpen} title={t.folderEmpty} hint={t.emptyHint}>
              <button class="primary" onclick={() => files?.click()}><Upload size={16} />{t.upload}</button>
            </EmptyState>
          {/if}
        {/snippet}
      </EntryList>
    {/key}
    {/if}

    {#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
    {#if !selected.size && !details}<button class="primary fab" aria-label={t.new} onclick={() => (sheet = 'new')}><Plus size={24} /></button>{/if}
  </section>

  {#if details}
    {#await import('../components/Details.svelte') then { default: Details }}
      {#key details.name}
        <Details {vol} path={join(details.name)} entry={details} thumbs={[thumb(details), raw(details)]} onclose={closeDetails} />
      {/key}
    {/await}
  {/if}
</div>

<input bind:this={files} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
<input bind:this={folder} type="file" webkitdirectory hidden onchange={(e) => upload(e.currentTarget.files, true)} />

{#if conflict}
  {@const c = conflict}
  {#await import('../components/ConflictDialog.svelte') then { default: ConflictDialog }}
    {#key c}
      <ConflictDialog name={c.name} rest={c.rest} onchoose={(choice, all) => c.resolve([choice, all])} onclose={() => c.resolve(null)} />
    {/key}
  {/await}
{/if}

{#if help}
  {#await import('../components/ShortcutsDialog.svelte') then { default: ShortcutsDialog }}
    <ShortcutsDialog onclose={() => (help = false)} />
  {/await}
{/if}

{#if selected.size}
  <div class="sel-tools" role="group" aria-label={t.selected(selected.size)} bind:offsetHeight={barH}>
    <button onclick={() => download([...selected])}><Download size={20} /><span>{t.download}</span></button>
    <button onclick={() => (dialog = { kind: 'move', names: [...selected] })}><FolderInput size={20} /><span>{t.moveOrCopy}</span></button>
    <button disabled={!one} onclick={() => pass('share')}><Share2 size={20} /><span>{t.share}</span></button>
    <button class="danger" onclick={() => (dialog = { kind: 'delete', names: [...selected] })}><Trash size={20} /><span>{t.remove}</span></button>
    <button disabled={!one} onclick={() => (sheet = 'more')}><Ellipsis size={20} /><span>{t.more}</span></button>
  </div>
{/if}

{#if sheet}
  {#await import('../components/Sheet.svelte') then { default: Sheet }}
    {@const items = sheet === 'new' ? creators : [act.rename, act.details].map((a) => ({ ...a, run: () => pass(a.id) }))}
    <Sheet title={sheet === 'new' ? t.new : (one?.name ?? '')} onclose={() => (sheet = null)}>
      {#each items as c (c.label)}
        <button
          class="sheet-item"
          onclick={() => {
            sheet = null
            c.run()
          }}><c.icon size={20} />{c.label}</button
        >
      {/each}
    </Sheet>
  {/await}
{/if}

{#if preview}
  {#await import('../components/Preview.svelte') then { default: Preview }}
    <Preview bind:entry={preview} entries={shown} url={(e, dl) => rawURL(vol, join(e.name), dl)} onclose={() => (preview = null)} />
  {/await}
{/if}

{#if dialog}
  {#await Promise.all([import('../components/NameDialog.svelte'), import('../components/ConfirmDialog.svelte'), import('../components/MoveDialog.svelte')]) then [{ default: NameDialog }, { default: ConfirmDialog }, { default: MoveDialog }]}
    {#if dialog?.kind === 'mkdir'}
      <NameDialog
        title={t.newFolder}
        label={t.folderName}
        action={t.create}
        onsave={(n) => api.mkdir(vol, join(n)).then(refresh)}
        onclose={() => (dialog = null)}
      />
    {:else if dialog?.kind === 'rename'}
      {@const e = dialog.e}
      <NameDialog
        title={t.rename}
        label={t.newName}
        action={t.rename}
        value={e.name}
        stem={!e.dir}
        onsave={async (n) => {
          const m = { from: { vol, path: join(e.name) }, to: { vol, path: join(n) } }
          await api.mv(m.from, m.to)
          toast(t.renamed(n), { action: undo(() => reverse([m])) })
          selected.clear()
          if (details?.name === e.name) closeDetails()
          await refresh()
        }}
        onclose={() => (dialog = null)}
      />
    {:else if dialog?.kind === 'delete'}
      {@const names = dialog.names}
      <ConfirmDialog
        title={t.confirmDeleteTitle}
        message={t.confirmDelete(t.what(names))}
        action={t.remove}
        onconfirm={async () => {
          const v = vol
          const r = await api.rm(v, names.map(join))
          const failed = new Set(r.failed.map((f) => f.path))
          const left = names.filter((n) => failed.has(join(n)))
          if (r.trashed.length)
            toast(t.trashed(t.what(names.filter((n) => !failed.has(join(n))))), {
              action: undo(() => restore(v, r.trashed)),
            })
          selected.clear()
          if (details && names.includes(details.name) && !left.includes(details.name)) closeDetails()
          await refresh()
          if (left.length) {
            dialog = { kind: 'delete', names: left }
            throw new Error(t.removeFailed(left))
          }
        }}
        onclose={() => (dialog = null)}
      />
    {:else if dialog?.kind === 'move'}
      <MoveDialog
        {vols}
        {vol}
        dir={path}
        names={dialog.names}
        ondone={moved}
        onclose={() => (dialog = null)}
      />
    {/if}
  {/await}
{/if}
