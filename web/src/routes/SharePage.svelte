<script lang="ts">
  import Download from '@lucide/svelte/icons/download'
  import Upload from '@lucide/svelte/icons/upload'
  import Eye from '@lucide/svelte/icons/eye'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import Lock from '@lucide/svelte/icons/lock'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import FileIcon from '../components/FileIcon.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import Preview from '../components/Preview.svelte'
  import { api, HttpError, shareRawURL, shareThumbURL, shareURL, validShareToken, type Entry, type ShareInfo } from '../lib/api'
  import { route, link, navigate } from '../lib/router.svelte'
  import { enqueue } from '../lib/uploads.svelte'
  import { arrange, size, thumbable, rawThumb, fallback, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'

  let { token }: { token: string } = $props()

  let info = $state<ShareInfo | null>(null)
  let fatal = $state('')
  let password = $state('')
  let unlockError = $state('')
  let entries = $state.raw<Entry[]>([])
  let at = $state<string | null>(null)
  let error = $state('')
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let preview = $state.raw<Entry | null>(null)
  let dragging = $state(false)
  let depth = 0
  let picker = $state<HTMLInputElement>()
  let tries = $state(0)

  const base = $derived(shareURL(token))
  const p = $derived(new URLSearchParams(route.search).get('p') ?? '')
  const crumbs = $derived(p ? p.split('/') : [])
  const join = (n: string) => (p ? `${p}/${n}` : n)
  const here = (sub: string) => (sub ? `${base}?${new URLSearchParams({ p: sub })}` : base)
  const shown = $derived(arrange(at === p ? entries : [], '', sort, desc))
  const shared = $derived(info?.locked === false ? info : null)
  const canUpload = $derived(shared?.mode === 'upload' || shared?.mode === 'drop')
  const listed = $derived(!!shared?.dir && shared.mode !== 'drop')
  const file = $derived<Entry | null>(shared && !shared.dir ? { name: shared.name, dir: false, size: shared.size ?? 0, mtime: 0 } : null)
  const fileSrc = $derived(file && fallback([thumbable(file.name) && shareThumbURL(token, ''), rawThumb(file) && shareRawURL(token, '')], tries))

  async function load() {
    fatal = ''
    if (!validShareToken(token)) {
      fatal = t.shareGone
      return
    }
    try {
      info = await api.shareInfo(token)
    } catch (e) {
      info = null
      fatal = e instanceof HttpError && e.status === 404 ? t.shareGone : t.loadFailed
    }
  }
  load()

  async function refresh() {
    const want = p
    try {
      const list = (await api.shareLs(token, want)).entries
      if (want !== p) return
      entries = list
      error = ''
    } catch (e) {
      if (want !== p) return
      entries = []
      error = (e as Error).message
    }
    at = want
  }

  $effect(() => {
    p
    if (listed) refresh()
  })

  async function unlock(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.unlock(token, password)
      password = ''
      unlockError = ''
      await load()
    } catch (err) {
      unlockError = err instanceof HttpError && err.status === 401 ? t.wrongPassword : (err as Error).message
    }
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
    !e ? (canUpload ? [act.upload] : []) : e.dir ? [act.open] : [act.preview, act.download]

  function open(e: Entry) {
    if (e.dir) navigate(here(join(e.name)))
    else preview = e
  }

  function onaction(id: string, e: Entry | null) {
    if (id === 'upload') picker?.click()
    else if (!e) return
    else if (id === 'open') open(e)
    else if (id === 'download') location.assign(shareRawURL(token, join(e.name), true))
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer?.types.includes('Files')
</script>

<svelte:head><title>{shared?.name ?? t.brand}</title></svelte:head>
<svelte:window ondragover={(e) => e.preventDefault()} ondrop={(e) => e.preventDefault()} />

<div
  class="public"
  role="presentation"
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
        <nav class="crumbs" aria-label={t.breadcrumb}>
          <a href={here('')} onclick={link} aria-current={crumbs.length ? undefined : 'page'}>{shared.name}</a>
          {#each crumbs as c, i}
            <ChevronRight size={16} />
            <a href={here(crumbs.slice(0, i + 1).join('/'))} onclick={link} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</a>
          {/each}
        </nav>
      {:else}
        <h1 class="public-name" title={shared.name}>{shared.name}</h1>
      {/if}
      <span class="grow"></span>
      {#if file}
        <a class="button primary" href={shareRawURL(token, '', true)} download><Download size={18} />{t.download}</a>
      {:else if listed && shared.mode === 'upload'}
        <button class="primary" onclick={() => picker?.click()}><Upload size={18} />{t.upload}</button>
      {/if}
    {/if}
  </header>

  {#if fatal}
    <div class="load-error">
      <p class={fatal === t.shareGone ? 'hint big' : 'error'}>{fatal}</p>
      {#if fatal !== t.shareGone}<button onclick={load}>{t.retry}</button>{/if}
    </div>
  {:else if info?.locked}
    <form class="login" onsubmit={unlock}>
      <Lock size={32} class="ficon" />
      <p>{t.shareLocked}</p>
      <label for="share-password" class="sr-only">{t.password}</label>
      <input id="share-password" type="password" bind:value={password} placeholder={t.password} autocomplete="off" required />
      <button class="primary" type="submit">{t.unlock}</button>
      {#if unlockError}<p class="error">{unlockError}</p>{/if}
    </form>
  {:else if file}
    <div class="public-file">
      <div class="details-thumb">
        {#if fileSrc}
          <img src={fileSrc} alt="" onerror={() => tries++} />
        {:else}
          <FileIcon name={file.name} dir={false} size={72} />
        {/if}
      </div>
      <h2>{file.name}</h2>
      <p class="hint">{size(file.size)}</p>
      <button onclick={() => (preview = file)}><Eye size={18} />{t.preview}</button>
    </div>
  {:else if info && info.mode === 'drop'}
    <div class="dropbox">
      <Upload size={40} class="ficon" />
      <p>{t.dropTitle}</p>
      <button class="primary" onclick={() => picker?.click()}><Upload size={18} />{t.chooseFiles}</button>
      <p class="hint">{t.dropHint}</p>
    </div>
  {:else if listed}
    <section class="files">
      {#if error}<p class="error banner">{error}</p>{/if}
      {#key p}
        <EntryList
          entries={shown}
          grid={false}
          bind:sort
          bind:desc
          thumb={(e) => (!e.dir && thumbable(e.name) ? shareThumbURL(token, join(e.name)) : null)}
          raw={(e) => (rawThumb(e) ? shareRawURL(token, join(e.name)) : null)}
          {actions}
          {onaction}
          onopen={open}
          loading={at !== p}
        >
          {#snippet empty()}
            {#if !error}<EmptyState icon={FolderOpen} title={t.folderEmpty} hint={canUpload ? t.dropHere : ''} />{/if}
          {/snippet}
        </EntryList>
      {/key}
    </section>
  {/if}

  {#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
</div>

{#if canUpload}
  <input bind:this={picker} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
{/if}

{#if preview}
  <Preview
    bind:entry={preview}
    entries={file ? [file] : shown}
    url={(e, dl) => shareRawURL(token, file ? '' : join(e.name), dl)}
    onclose={() => (preview = null)}
  />
{/if}
