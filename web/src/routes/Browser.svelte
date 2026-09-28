<script lang="ts">
  import { untrack } from 'svelte'
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
  import { api, filesURL, rawURL, thumbURL, type Entry } from '../lib/api'
  import { navigate, link, route } from '../lib/router.svelte'
  import { enqueue } from '../lib/uploads.svelte'
  import { thumbable, arrange, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import NavToggle from '../components/NavToggle.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import Preview from '../components/Preview.svelte'
  import Details from '../components/Details.svelte'
  import NameDialog from '../components/NameDialog.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import MoveDialog from '../components/MoveDialog.svelte'

  let { vol, path, vols }: { vol: string; path: string; vols: string[] } = $props()

  type Dialog = { kind: 'mkdir' } | { kind: 'rename'; e: Entry } | { kind: 'delete' | 'move'; names: string[] }

  let entries = $state<Entry[]>([])
  let error = $state('')
  let at = $state('')
  let filter = $state('')
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let grid = $state(load('grid') === '1')
  let dragging = $state(false)
  let depth = 0
  let preview = $state<Entry | null>(null)
  let details = $state<Entry | null>(null)
  let dialog = $state<Dialog | null>(null)
  const selected = new SvelteSet<string>()
  let files = $state<HTMLInputElement>()
  let folder = $state<HTMLInputElement>()

  const here = $derived(`${vol}/${path}`)
  const join = (n: string) => (path ? `${path}/${n}` : n)
  const crumbs = $derived(path ? path.split('/') : [])
  const shown = $derived(arrange(at === here ? entries : [], filter, sort, desc))
  const selectedFiles = $derived(entries.filter((e) => !e.dir && selected.has(e.name)).map((e) => e.name))
  const thumb = (e: Entry) => (!e.dir && thumbable(e.name) ? thumbURL(vol, join(e.name)) : null)

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
    filter
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

  const what = (names: string[]) => (names.length === 1 ? `“${names[0]}”` : t.items(names.length))

  function keydown(e: KeyboardEvent) {
    if (document.querySelector('[role=dialog], [role=menu]')) return
    const typing = e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement
    if (e.key === 'Escape') {
      if (details) closeDetails()
      else selected.clear()
    } else if (typing) return
    else if ((e.key === 'Delete' || e.key === 'Backspace') && selected.size) dialog = { kind: 'delete', names: [...selected] }
    else if (e.key === 'Enter' && selected.size === 1 && !(e.target instanceof HTMLButtonElement || e.target instanceof HTMLAnchorElement)) {
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
    <header class="bar">
      <NavToggle />
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
            <DropdownMenu.Item class="menu-item" onSelect={() => files?.click()}><Upload size={16} />{t.upload}</DropdownMenu.Item>
            <DropdownMenu.Item class="menu-item" onSelect={() => folder?.click()}><FolderUp size={16} />{t.uploadFolder}</DropdownMenu.Item>
            <DropdownMenu.Separator class="menu-sep" />
            <DropdownMenu.Item class="menu-item" onSelect={() => (dialog = { kind: 'mkdir' })}><FolderPlus size={16} />{t.newFolder}</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      <span class="grow"></span>
      <label for="filter" class="sr-only">{t.filter}</label>
      <input id="filter" class="filter" type="search" bind:value={filter} placeholder={t.filter} />
      <button class="icon-btn" aria-label={grid ? t.listView : t.gridView} title={grid ? t.listView : t.gridView} onclick={() => (grid = !grid)}>
        {#if grid}<List size={20} />{:else}<LayoutGrid size={20} />{/if}
      </button>
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
        {actions}
        {onaction}
        onopen={open}
        {batch}
        empty={at !== here || error ? '' : filter ? t.noMatch : t.empty}
      />
    {/key}

    {#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
  </section>

  {#if details}
    {#key details.name}
      <Details {vol} path={join(details.name)} entry={details} thumb={thumb(details)} onclose={closeDetails} />
    {/key}
  {/if}
</div>

<input bind:this={files} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
<input bind:this={folder} type="file" webkitdirectory hidden onchange={(e) => upload(e.currentTarget.files, true)} />

{#if preview}
  <Preview bind:entry={preview} entries={shown} url={(e, dl) => rawURL(vol, join(e.name), dl)} onclose={() => (preview = null)} />
{/if}

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
      await api.mv({ vol, path: join(e.name) }, { vol, path: join(n) })
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
    message={t.confirmDelete(what(names))}
    action={t.remove}
    onconfirm={async () => {
      const failed = new Set((await api.rm(vol, names.map(join)))?.failed.map((f) => f.path))
      const left = names.filter((n) => failed.has(join(n)))
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
    ondone={() => {
      selected.clear()
      closeDetails()
      refresh()
    }}
    onclose={() => (dialog = null)}
  />
{/if}
