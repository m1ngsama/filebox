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
  import Search from '@lucide/svelte/icons/search'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import EllipsisVertical from '@lucide/svelte/icons/ellipsis-vertical'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import { api, filesURL, rawURL, thumbURL, type Entry, type Move } from '../lib/api'
  import { toast, fail, runLatest } from '../lib/toast.svelte'
  import { navigate, link, route } from '../lib/router.svelte'
  import { enqueue } from '../lib/uploads.svelte'
  import { thumbable, rawThumb, arrange, parent, base, flip, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import NavToggle from '../components/NavToggle.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'

  let { vol, path, vols }: { vol: string; path: string; vols: string[] } = $props()

  type Dialog = { kind: 'mkdir' } | { kind: 'rename'; e: Entry } | { kind: 'delete' | 'move'; names: string[] }

  let entries = $state.raw<Entry[]>([])
  let error = $state('')
  let at = $state('')
  let filter = $state('')
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
  const selected = new SvelteSet<string>()
  let files = $state<HTMLInputElement>()
  let folder = $state<HTMLInputElement>()

  const here = $derived(`${vol}/${path}`)
  const join = (n: string) => (path ? `${path}/${n}` : n)
  const crumbs = $derived(path ? path.split('/') : [])
  const shown = $derived(arrange(at === here ? entries : [], query, sort, desc))
  const one = $derived(selected.size === 1 ? entries.find((e) => selected.has(e.name)) : undefined)
  const selectedFiles = $derived(entries.filter((e) => !e.dir && selected.has(e.name)).map((e) => e.name))
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

  function open(e: Entry) {
    if (e.dir) navigate(filesURL(vol, join(e.name)))
    else preview = e
  }

  function download(names: string[]) {
    for (const n of names) {
      const a = document.createElement('a')
      a.href = rawURL(vol, join(n), true)
      a.download = ''
      a.click()
    }
  }

  function upload(list: FileList | null | undefined, asFolder = false) {
    if (!list?.length) return
    const items = [...list].map((file) => ({ file, rel: asFolder ? file.webkitRelativePath : '' }))
    enqueue(items, '/upload/', { vol, dir: path || '/' }, refresh)
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

  function endSearch() {
    searching = false
    filter = ''
  }

  const act = {
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
    : e.dir ? [act.open, act.rename, act.move, act.share, act.details, act.remove]
    : [act.open, act.download, act.rename, act.move, act.share, act.details, act.remove]

  function onaction(id: string, e: Entry | null) {
    if (id === 'mkdir') dialog = { kind: 'mkdir' }
    else if (id === 'upload') files?.click()
    else if (!e) return
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

  function moved(done: Move[], copy: boolean) {
    selected.clear()
    closeDetails()
    refresh()
    if (!done.length) return
    const w = t.what(done.map((m) => base(m.from.path)))
    if (copy) toast(t.copiedTo(w))
    else toast(t.movedTo(w, `${done[0].to.vol}:/${parent(done[0].to.path)}`), { action: undo(() => reverse(done)) })
  }

  function pass(id: string) {
    const e = one
    selected.clear()
    if (e) onaction(id, e)
  }

  function focused() {
    const i = (document.activeElement as HTMLElement | null)?.closest<HTMLElement>('[data-i]')?.dataset.i
    return i === undefined ? undefined : shown[+i]
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
    else if (mod && !e.shiftKey && k === 'a') {
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
  <button class="ghost" disabled={!selectedFiles.length} onclick={() => download(selectedFiles)}>
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
        <a href={filesURL(vol, '')} onclick={link}>{vol}</a>
        {#each crumbs as c, i}
          <ChevronRight size={16} />
          <a href={filesURL(vol, crumbs.slice(0, i + 1).join('/'))} onclick={link} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</a>
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
      <label for="filter" class="sr-only">{t.filter}</label>
      <input id="filter" class="filter" type="search" bind:value={filter} bind:this={filterEl} placeholder={t.filter} />
      <button class="icon-btn search-open" aria-label={t.openFilter} onclick={search}><Search size={20} /></button>
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
              {#each [['name', t.name], ['size', t.size], ['mtime', t.mtime]] as [k, label] (k)}
                <DropdownMenu.Item class="menu-item" aria-current={sort === k || undefined} onSelect={() => sortBy(k as Sort)}>
                  {#if sort !== k}<span class="menu-gap"></span>{:else if desc}<ArrowDown size={16} />{:else}<ArrowUp size={16} />{/if}{label}
                </DropdownMenu.Item>
              {/each}
            </DropdownMenu.Group>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </header>

    {#if error && at === here}<p class="error banner">{error}</p>{/if}

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

{#if help}
  {#await import('../components/ShortcutsDialog.svelte') then { default: ShortcutsDialog }}
    <ShortcutsDialog onclose={() => (help = false)} />
  {/await}
{/if}

{#if selected.size}
  <div class="sel-tools" role="toolbar" aria-label={t.selected(selected.size)}>
    <button disabled={!selectedFiles.length} onclick={() => download(selectedFiles)}><Download size={20} /><span>{t.download}</span></button>
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
