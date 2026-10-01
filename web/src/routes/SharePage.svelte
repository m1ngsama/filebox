<script lang="ts">
  import { icon } from '../lib/icon'
  import { SvelteSet } from 'svelte/reactivity'
  import Download from '@lucide/svelte/icons/download'
  import Upload from '@lucide/svelte/icons/upload'
  import Eye from '@lucide/svelte/icons/eye'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import Lock from '@lucide/svelte/icons/lock'
  import LayoutGrid from '@lucide/svelte/icons/layout-grid'
  import List from '@lucide/svelte/icons/list'
  import Clock from '@lucide/svelte/icons/clock'
  import Link2Off from '@lucide/svelte/icons/link-2-off'
  import X from '@lucide/svelte/icons/x'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import FolderX from '@lucide/svelte/icons/folder-x'
  import type { Action } from '../components/EntryList.svelte'
  import FileIcon from '../components/FileIcon.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import Toasts from '../components/Toasts.svelte'
  import { api, HttpError, shareFileURL, shareRawURL, shareThumbURL, shareURL, shareZipURL, saveURL, validShareToken, type Entry, type ShareInfo } from '../lib/api'
  import { route, link, navigate } from '../lib/router.svelte'
  import { enqueue } from '../lib/uploads.svelte'
  import { arrange, size, kind, thumbable, rawThumb, fallback, child, prefersGrid, sidecars, type Sort } from '../lib/format'
  import { viewOf, keepView, type View } from '../lib/storage'
  import { saveZip, packing } from '../lib/located'
  import { t } from '../lib/i18n'
  import { playing } from '../lib/playing.svelte'

  let { token }: { token: string } = $props()

  let info = $state<ShareInfo | null>(null)
  let fatal = $state<'gone' | 'expired' | 'error' | ''>('')
  let password = $state('')
  let unlockError = $state('')
  let unlocking = $state(false)
  let entries = $state.raw<Entry[]>([])
  let at = $state<string | null>(null)
  let error = $state<{ status: number; message: string } | null>(null)
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let chosen = $state<View>()
  let preview = $state.raw<Entry | null>(null)
  let dragging = $state(false)
  let depth = 0
  let picker = $state<HTMLInputElement>()
  let tries = $state(0)
  const selected = new SvelteSet<string>()

  const base = $derived(shareURL(token))
  const p = $derived(new URLSearchParams(route.search).get('p') ?? '')
  const crumbs = $derived(p ? p.split('/') : [])
  const join = (n: string) => child(p, n)
  const here = (sub: string) => (sub ? `${base}?${new URLSearchParams({ p: sub })}` : base)
  const side = $derived(sidecars(at === p ? entries : []))
  const hidden = $derived(new Set([...side.values()].flat()))
  const shown = $derived(arrange(at === p ? entries.filter((e) => !hidden.has(e.name)) : [], '', sort, desc))
  const shared = $derived(info?.locked === false ? info : null)
  const canUpload = $derived(shared?.mode === 'upload' || shared?.mode === 'drop')
  const listed = $derived(!!shared?.dir && shared.mode !== 'drop')
  const lister = $derived(listed ? import('../components/EntryList.svelte') : null)
  const file = $derived<Entry | null>(shared && !shared.dir ? { name: shared.name, dir: false, size: shared.size ?? 0, mtime: 0 } : null)
  const fileKind = $derived(file ? kind(file.name) : '')
  const imageSrc = $derived(file && fallback([shareRawURL(token, ''), thumbable(file.name) && shareThumbURL(token, '')], tries))
  const folder = $derived(crumbs.at(-1) ?? shared?.name ?? '')
  const coarse = matchMedia('(pointer: coarse)').matches
  const left = $derived(shared?.expires ? shared.expires - Date.now() / 1000 : 0)

  let frozen = $state<boolean>()
  const auto = $derived(frozen ?? (at === p ? prefersGrid(shown) : undefined))
  const grid = $derived(chosen ? chosen === 'grid' : !!auto)
  $effect(() => {
    chosen = viewOf(`s:${token}/${p}`)
    frozen = undefined
  })
  $effect(() => {
    if (frozen === undefined && auto !== undefined) frozen = auto
  })

  function flipView() {
    chosen = grid ? 'list' : 'grid'
    keepView(`s:${token}/${p}`, chosen)
  }

  let barH = $state(0)
  $effect(() => {
    const s = document.documentElement.style
    if (selected.size && barH) s.setProperty('--bar-h', `${barH}px`)
    else s.removeProperty('--bar-h')
    return () => s.removeProperty('--bar-h')
  })

  async function load() {
    fatal = ''
    if (!validShareToken(token)) {
      fatal = 'gone'
      return
    }
    try {
      info = await api.shareInfo(token)
    } catch (e) {
      info = null
      const s = e instanceof HttpError ? e.status : 0
      fatal = s === 404 ? 'gone' : s === 410 ? 'expired' : 'error'
    }
  }
  load()

  let loading: AbortController | undefined
  async function refresh() {
    const want = p
    loading?.abort()
    const stop = (loading = new AbortController())
    try {
      const list = await api.shareLs(token, want, stop.signal)
      if (want !== p || stop.signal.aborted) return
      entries = list
      error = null
    } catch (e) {
      if (want !== p || stop.signal.aborted) return
      entries = []
      const status = e instanceof HttpError ? e.status : 0
      if (status === 401) {
        info = { locked: true, mode: info?.mode ?? 'read' }
        error = null
        return
      }
      if (status === 410) {
        info = null
        fatal = 'expired'
      }
      error = { status, message: status ? (e as Error).message : t.loadFailed }
    }
    at = want
  }

  $effect(() => {
    p
    selected.clear()
    if (listed) refresh()
  })

  async function unlock(e: SubmitEvent) {
    e.preventDefault()
    unlocking = true
    try {
      await api.unlock(token, password)
      password = ''
      unlockError = ''
      await load()
    } catch (err) {
      unlockError = !(err instanceof HttpError) ? t.loadFailed : err.status === 401 ? t.wrongPassword : err.message
    }
    unlocking = false
  }

  function upload(list: FileList | null | undefined) {
    if (!canUpload || !list?.length) return
    const sub = info?.mode === 'upload' ? p : ''
    enqueue(
      [...list].map((f) => ({ file: f, rel: sub ? `${sub}/${f.name}` : '' })),
      `${base}/upload/`,
      {},
      () => listed && refresh(),
    )
  }

  const act = {
    open: { id: 'open', label: t.open, icon: FolderOpen },
    preview: { id: 'open', label: t.preview, icon: Eye },
    download: { id: 'download', label: t.download, icon: Download },
    upload: { id: 'upload', label: t.upload, icon: Upload },
  } satisfies Record<string, Action>

  const actions = (e: Entry | null): Action[] =>
    !e ? (canUpload ? [act.upload] : []) : e.dir ? [act.open, act.download] : [act.preview, act.download]

  function open(e: Entry) {
    if (e.dir) navigate(here(join(e.name)))
    else preview = e
  }

  function download(names: string[]) {
    const one = names.length === 1 ? shown.find((e) => e.name === names[0]) : undefined
    if (one && !one.dir) saveURL(shareRawURL(token, join(one.name), true))
    else {
      const name = one ? one.name : t.zipName(folder, names.length)
      saveZip(shareZipURL(token, names.map(join), name), name)
    }
  }

  function onaction(id: string, e: Entry | null) {
    if (id === 'upload') picker?.click()
    else if (!e) return
    else if (id === 'open') open(e)
    else if (id === 'download') download([e.name])
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer?.types.includes('Files')
</script>

<svelte:head><title>{shared?.name ?? t.brand}</title></svelte:head>
<svelte:window ondragover={(e) => e.preventDefault()} ondrop={(e) => e.preventDefault()} />

{#snippet batch()}
  <button class="ghost" onclick={() => download([...selected])}><Download size={icon.sm} />{t.download}</button>
  <button class="icon-btn" aria-label={t.clearSelection} onclick={() => selected.clear()}><X size={icon.sm} /></button>
{/snippet}

<main
  class="public"
  ondragenter={(e) => {
    if (!canUpload || !hasFiles(e)) return
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
  <header class="bar public-bar">
    <span class="brand">{t.brand}</span>
    {#if shared}
      {#if listed}
        <h1 class="sr-only">{folder}</h1>
        <nav class="crumbs" aria-label={t.breadcrumb}>
          <a href={here('')} onclick={link} aria-current={crumbs.length ? undefined : 'page'}>{shared.name}</a>
          {#each crumbs as c, i}
            <ChevronRight size={icon.sm} />
            <a href={here(crumbs.slice(0, i + 1).join('/'))} onclick={link} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</a>
          {/each}
        </nav>
      {:else}
        <h1 class="public-name" title={shared.name}>{shared.name}</h1>
      {/if}
      <span class="grow"></span>
      {#if file}
        <a class="button primary" href={shareRawURL(token, '', true)} download><Download size={icon.md} /><span>{t.download}</span></a>
      {:else if listed}
        <button class="icon-btn view" aria-label={grid ? t.listView : t.gridView} title={grid ? t.listView : t.gridView} onclick={flipView}>
          {#if grid}<List size={icon.md} />{:else}<LayoutGrid size={icon.md} />{/if}
        </button>
        <a class={shared.mode === 'upload' ? 'button labeled' : 'button primary labeled'} href={shareZipURL(token, p ? [p] : [], folder)} download onclick={() => packing(folder)}
          ><Download size={icon.md} /><span>{t.downloadAll}</span></a
        >
        {#if shared.mode === 'upload'}<button class="primary" onclick={() => picker?.click()}><Upload size={icon.md} /><span>{t.upload}</span></button>{/if}
      {/if}
    {/if}
  </header>

  {#if shared && (shared.note || shared.expires || (canUpload && shared.max_upload) || listed)}
    <div class="public-info" class:hero={listed && !p}>
      {#if listed && !p}<div class="public-title" aria-hidden="true">{shared.name}</div>{/if}
      {#if shared.note}<p class="public-note">{shared.note}</p>{/if}
      <p class="hint">
        {#if listed}<span class="public-count">{at === p && !error ? t.folderSummary(shown.filter((e) => e.dir).length, shown.filter((e) => !e.dir).length, size(shown.reduce((n, e) => n + e.size, 0))) : '\u00a0'}</span>{/if}
        {#if shared.expires}<span><Clock size={icon.sm} />{left > 0 ? t.expiresIn(left) : t.expired}</span>{/if}
        {#if canUpload && shared.max_upload}<span>{t.maxUpload(size(shared.max_upload))}</span>{/if}
      </p>
    </div>
  {/if}

  {#if fatal === 'error'}
    <div class="public-gone">
      <EmptyState icon={CloudOff} as="h1" title={t.loadFailedTitle} hint={t.loadFailed}>
        <button class="primary" onclick={load}>{t.retry}</button>
      </EmptyState>
    </div>
  {:else if fatal}
    <div class="public-gone">
      <EmptyState icon={fatal === 'expired' ? Clock : Link2Off} as="h1" title={fatal === 'expired' ? t.linkExpired : t.linkGone} hint={fatal === 'expired' ? t.linkExpiredHint : t.shareGone} />
    </div>
  {:else if info?.locked}
    <div class="public-gone">
      <EmptyState icon={Lock} as="h1" title={t.shareLocked} hint={t.shareLockedHint}>
        <form class="unlock" onsubmit={unlock}>
          <label class="field">
            <span>{t.password}</span>
            <input
              type="password"
              bind:value={password}
              autocomplete="off"
              required
              aria-invalid={!!unlockError}
              aria-describedby={unlockError ? 'unlock-error' : undefined}
              oninput={() => (unlockError = '')}
            />
          </label>
          {#if unlockError}<p class="error" id="unlock-error" role="alert">{unlockError}</p>{/if}
          <button class="primary" class:busy={unlocking} disabled={unlocking} type="submit">{t.unlock}</button>
        </form>
      </EmptyState>
    </div>
  {:else if file}
    {#if fileKind === 'image' && imageSrc}
      <button class="public-image" aria-label={t.preview} onclick={() => (preview = file)}>
        <img src={imageSrc} alt={file.name} onerror={() => tries++} />
      </button>
    {:else if fileKind && fileKind !== 'image'}
      <div class="public-inline">
        {#await import('../components/Preview.svelte') then { default: Preview }}
          <Preview entry={file} entries={[file]} url={(_, as) => shareFileURL(token, '', as)} onclose={() => {}} inline />
        {/await}
      </div>
    {:else}
      <div class="public-file">
        <div class="details-thumb"><FileIcon name={file.name} dir={false} size={72} /></div>
        <h2>{file.name}</h2>
        <p class="hint">{size(file.size)}</p>
        <a class="button primary" href={shareRawURL(token, '', true)} download><Download size={icon.md} />{t.download}</a>
      </div>
    {/if}
    {#if fileKind && (fileKind !== 'image' || imageSrc)}<p class="public-meta hint">{size(file.size)}</p>{/if}
  {:else if info && info.mode === 'drop'}
    <div class="dropbox">
      <Upload size={40} class="ficon" />
      <p>{coarse ? t.dropTitleTouch : t.dropTitle}</p>
      <button class="primary" onclick={() => picker?.click()}><Upload size={icon.md} />{t.chooseFiles}</button>
      <p class="hint">{t.dropHint}</p>
    </div>
  {:else if listed}
    <section class="files" class:selecting={selected.size > 0}>
      {#key p}
        {#await lister!}
          <div class="loading" role="status" aria-label={t.loading}></div>
        {:then { default: EntryList }}
        <EntryList
          entries={shown}
          {grid}
          {selected}
          bind:sort
          bind:desc
          thumb={(e) => (e.dir || thumbable(e.name) ? shareThumbURL(token, join(e.name)) : null)}
          meta={(e) => shareFileURL(token, join(e.name), 'meta')}
          raw={(e) => (rawThumb(e) ? shareRawURL(token, join(e.name)) : null)}
          {actions}
          {onaction}
          onopen={open}
          tag={(e) => (side.has(e.name) ? t.subtitles : undefined)}
          {batch}
          loading={at !== p}
        >
          {#snippet empty()}
            {#if error?.status === 404}
              <EmptyState icon={FolderX} as="h2" title={t.folderMissing} hint={t.folderMissingHint}>
                <a class="button primary" href={here('')} onclick={link}><FolderOpen size={icon.sm} />{t.openNamed(shared?.name ?? t.brand)}</a>
              </EmptyState>
            {:else if error}
              <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error.message}>
                <button class="primary" onclick={refresh}>{t.retry}</button>
              </EmptyState>
            {:else}
              <EmptyState icon={FolderOpen} title={t.folderEmpty} hint={canUpload ? t.dropHere : ''} />
            {/if}
          {/snippet}
        </EntryList>
        {/await}
      {/key}
    </section>
    {#if selected.size}
      <div class="sel-tools" role="group" aria-label={t.selected(selected.size)} bind:offsetHeight={barH}>
        <button onclick={() => download([...selected])}><Download size={icon.md} /><span>{t.download}</span></button>
        <button onclick={() => selected.clear()}><X size={icon.md} /><span>{t.clearSelection}</span></button>
      </div>
    {/if}
  {/if}

  {#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
</main>

{#if canUpload}
  <input bind:this={picker} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
  {#await import('../components/UploadPanel.svelte') then { default: UploadPanel }}<UploadPanel />{/await}
{/if}

{#if preview}
  {#await import('../components/Preview.svelte') then { default: Preview }}
    <Preview
      bind:entry={preview}
      entries={file ? [file] : [...shown, ...entries.filter((e) => hidden.has(e.name))]}
      url={(e, as) => shareFileURL(token, file ? '' : join(e.name), as)}
      onclose={() => (preview = null)}
    />
  {/await}
{/if}
{#if playing.on}{#await import('../components/MiniPlayer.svelte') then { default: MiniPlayer }}<MiniPlayer />{/await}{/if}
<Toasts />
