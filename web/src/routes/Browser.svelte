<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { api, filesURL, rawURL, thumbURL, type Entry } from '../lib/api'
  import { navigate, link } from '../lib/router.svelte'
  import { enqueue } from '../lib/uploads.svelte'
  import { thumbable } from '../lib/format'
  import { t } from '../lib/i18n'
  import Entries from '../components/Entries.svelte'
  import Preview from '../components/Preview.svelte'
  import ShareDialog from '../components/ShareDialog.svelte'

  let { vol, path }: { vol: string; path: string } = $props()

  let entries = $state<Entry[]>([])
  let error = $state('')
  let filter = $state('')
  let sort = $state<'name' | 'size' | 'mtime'>('name')
  let grid = $state((() => { try { return localStorage.getItem('grid') === '1' } catch { return false } })())
  let busy = $state(false)
  let dragging = $state(false)
  let preview = $state<Entry | null>(null)
  let sharing = $state<string | null>(null)
  const selected = new SvelteSet<string>()
  let files = $state<HTMLInputElement>()
  let folder = $state<HTMLInputElement>()

  const join = (n: string) => (path ? `${path}/${n}` : n)
  const crumbs = $derived(path ? path.split('/') : [])
  const shown = $derived(
    entries
      .filter((e) => e.name.toLowerCase().includes(filter.toLowerCase()))
      .sort((a, b) =>
        a.dir !== b.dir ? (a.dir ? -1 : 1)
        : sort === 'size' ? b.size - a.size
        : sort === 'mtime' ? b.mtime - a.mtime
        : a.name.localeCompare(b.name, 'zh-CN', { numeric: true }),
      ),
  )

  async function refresh() {
    try {
      entries = (await api.ls(vol, path)).entries
      error = ''
    } catch (e) {
      entries = []
      error = (e as Error).message
    }
  }

  $effect(() => {
    vol
    path
    selected.clear()
    filter = ''
    refresh()
  })

  $effect(() => {
    try {
      localStorage.setItem('grid', grid ? '1' : '0')
    } catch {}
  })

  async function run(fn: () => Promise<unknown>) {
    busy = true
    try {
      await fn()
    } catch (e) {
      alert((e as Error).message)
    }
    busy = false
    selected.clear()
    await refresh()
  }

  function open(e: Entry) {
    if (e.dir) navigate(filesURL(vol, join(e.name)))
    else preview = e
  }

  function mkdir() {
    const n = prompt(t.newFolderName)?.trim()
    if (n) run(() => api.mkdir(vol, join(n)))
  }

  function rename() {
    const [n] = selected
    const nn = prompt(t.rename, n)?.trim()
    if (nn && nn !== n) run(() => api.mv({ vol, path: join(n) }, { vol, path: join(nn) }))
  }

  function transfer(copy: boolean) {
    const m = (prompt(copy ? t.copyTo : t.moveTo, `${vol}:/${path}`) ?? '').match(/^([^:]+):(.*)$/)
    if (!m) return
    const [dv, dd] = [m[1], m[2].replace(/^\/+|\/+$/g, '')]
    run(async () => {
      for (const n of selected) {
        const dst = { vol: dv, path: dd ? `${dd}/${n}` : n }
        const r = copy ? await api.cp({ vol, path: join(n) }, dst) : await api.mv({ vol, path: join(n) }, dst)
        if (r?.job) await api.waitJob(r.job)
      }
    })
  }

  function remove() {
    if (confirm(t.confirmDelete(selected.size))) run(() => api.rm(vol, [...selected].map(join)))
  }

  function upload(list: FileList | null, asFolder = false) {
    if (!list?.length) return
    const items = [...list].map((file) => ({ file, rel: asFolder ? file.webkitRelativePath : '' }))
    enqueue(items, '/upload/', { vol, dir: path || '/' }, refresh)
  }

  function drop(e: DragEvent) {
    e.preventDefault()
    dragging = false
    upload(e.dataTransfer?.files ?? null)
  }
</script>

<svelte:window
  ondragover={(e) => { e.preventDefault(); dragging = true }}
  ondragleave={(e) => { if (!e.relatedTarget) dragging = false }}
  ondrop={drop}
/>

<nav class="crumbs">
  <a href={filesURL(vol, '')} onclick={link}>{vol}</a>
  {#each crumbs as c, i}
    <span>/</span><a href={filesURL(vol, crumbs.slice(0, i + 1).join('/'))} onclick={link}>{c}</a>
  {/each}
</nav>

<div class="toolbar">
  <button onclick={() => files?.click()}>{t.upload}</button>
  <button onclick={() => folder?.click()}>{t.uploadFolder}</button>
  <button onclick={mkdir}>{t.newFolder}</button>
  <span class="sep"></span>
  <button disabled={selected.size !== 1} onclick={rename}>{t.rename}</button>
  <button disabled={!selected.size} onclick={() => transfer(false)}>{t.move}</button>
  <button disabled={!selected.size} onclick={() => transfer(true)}>{t.copy}</button>
  <button disabled={selected.size !== 1} onclick={() => (sharing = join([...selected][0]))}>{t.share}</button>
  <button disabled={!selected.size} class="danger" onclick={remove}>{t.remove}</button>
  <span class="sep"></span>
  <input type="search" bind:value={filter} placeholder={t.filter} />
  <select bind:value={sort}>
    <option value="name">{t.sortName}</option>
    <option value="size">{t.sortSize}</option>
    <option value="mtime">{t.sortTime}</option>
  </select>
  <button onclick={() => (grid = !grid)}>{grid ? t.list : t.grid}</button>
  {#if busy}<span class="hint">{t.working}</span>{/if}
</div>

<input bind:this={files} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
<input bind:this={folder} type="file" webkitdirectory hidden onchange={(e) => upload(e.currentTarget.files, true)} />

{#if error}<p class="error">{error}</p>{/if}
{#if !error && !shown.length}<p class="hint">{t.empty}</p>{/if}
<Entries entries={shown} {grid} {selected} onopen={open} thumb={(e) => (thumbable(e.name) ? thumbURL(vol, join(e.name)) : null)} />

{#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
{#if preview}<Preview name={preview.name} url={rawURL(vol, join(preview.name))} onclose={() => (preview = null)} />{/if}
{#if sharing}<ShareDialog {vol} path={sharing} onclose={() => (sharing = null)} />{/if}
