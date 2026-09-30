<script lang="ts">
  import { icon } from '../lib/icon'
  import { SvelteSet } from 'svelte/reactivity'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import X from '@lucide/svelte/icons/x'
  import FileIcon from '../components/FileIcon.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import RowList from '../components/RowList.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import { api, filesURL, type TrashItem } from '../lib/api'
  import { link, navigate } from '../lib/router.svelte'
  import { size, date, ago, parent, place } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast, fail } from '../lib/toast.svelte'

  let { vol, vols }: { vol: string; vols: string[] } = $props()

  let items = $state<TrashItem[] | null>(null)
  let error = $state('')
  let busy = $state(false)
  let confirm = $state<'empty' | TrashItem[] | null>(null)
  const selected = new SvelteSet<string>()
  const picked = $derived(items?.filter((x) => selected.has(x.id)) ?? [])
  const all = $derived(!!items?.length && picked.length === items.length)

  async function load() {
    const want = vol
    try {
      const list = (await api.trash(want)).items
      if (want !== vol) return
      items = list
      error = ''
      for (const id of selected) if (!list.some((x) => x.id === id)) selected.delete(id)
    } catch (e) {
      if (want === vol) error = (e as Error).message
    }
  }

  $effect(() => {
    vol
    items = null
    error = ''
    selected.clear()
    load()
  })

  const what = (list: TrashItem[]) => t.what(list.map((x) => x.name))

  async function restore(list: TrashItem[]) {
    busy = true
    const v = vol
    const res = await Promise.allSettled(list.map((x) => api.restore(v, x.id)))
    const ok = list.filter((_, i) => res[i].status === 'fulfilled')
    const bad = list.find((_, i) => res[i].status === 'rejected')
    busy = false
    for (const x of ok) selected.delete(x.id)
    if (ok.length) {
      const first = ok[0]
      toast(t.restored(what(ok)), {
        actions: [{ label: t.show, run: () => navigate(`${filesURL(v, parent(first.path))}?details=${encodeURIComponent(first.name)}`) }],
      })
    }
    if (bad) fail(new Error(t.failedItem(bad.name, (res[list.indexOf(bad)] as PromiseRejectedResult).reason.message)))
    await load()
  }

  function toggleAll() {
    if (all) selected.clear()
    else for (const x of items ?? []) selected.add(x.id)
  }
</script>

<div class="toolbar">
  <div class="chips" role="group" aria-label={t.trash}>
    {#each vols as v}
      <a class="chip" href={`/trash/${encodeURIComponent(v)}`} onclick={link} aria-current={v === vol ? 'page' : undefined}><HardDrive size={icon.sm} />{v}</a>
    {/each}
  </div>
  <span class="grow"></span>
  <button class="quiet" disabled={!items?.length} onclick={() => (confirm = 'empty')}><TrashIcon size={icon.sm} />{t.emptyTrash}</button>
</div>
<p class="hint trash-note">{t.trashNote}</p>

{#if items?.length}
  <div class="trash-head" class:batch={selected.size > 0}>
    <label class="hit"><input type="checkbox" checked={all} indeterminate={!all && selected.size > 0} onchange={toggleAll} aria-label={t.selectAll} /></label>
    {#if selected.size}
      <span class="count">{t.selected(selected.size)}</span>
      <button class="ghost" disabled={busy} onclick={() => restore(picked)}><RotateCcw size={icon.sm} />{t.restore}</button>
      <button class="ghost danger" disabled={busy} onclick={() => (confirm = picked)}><TrashIcon size={icon.sm} />{t.deleteForever}</button>
      <button class="icon-btn" aria-label={t.clearSelection} onclick={() => selected.clear()}><X size={icon.sm} /></button>
    {:else}
      <span class="hint">{t.trashCount(items.length)}</span>
    {/if}
  </div>
{/if}

<RowList {items} {error} onretry={load} key={(x) => x.id} label={t.trash}>
  {#snippet row(it)}
    <label class="hit">
      <input
        type="checkbox"
        checked={selected.has(it.id)}
        onchange={() => (selected.has(it.id) ? selected.delete(it.id) : selected.add(it.id))}
        aria-label={t.select(it.name)}
      />
    </label>
    <FileIcon name={it.name} dir={it.dir} />
    <div class="row-main">
      <span class="row-title" title={it.name}>{it.name}</span>
      <span class="hint" title={`${date(it.deleted)} · ${t.origin} ${place(vol, parent(it.path))}`}>
        {t.deletedAt(ago(it.deleted))} · {t.origin} {place(vol, parent(it.path))}
      </span>
    </div>
    <span class="num">{it.dir ? '' : size(it.size)}</span>
    <button class="ghost" disabled={busy} onclick={() => restore([it])}><RotateCcw size={icon.sm} />{t.restore}</button>
  {/snippet}
  {#snippet empty()}<EmptyState icon={TrashIcon} title={t.trashEmpty} hint={t.trashEmptyHint} />{/snippet}
</RowList>

{#if confirm}
  {@const c = confirm}
  <ConfirmDialog
    title={c === 'empty' ? t.emptyTrash : t.deleteForever}
    message={c === 'empty' ? t.emptyTrashMessage(items?.length ?? 0, vol) : t.deleteForeverMessage(what(c))}
    action={c === 'empty' ? t.emptyTrash : t.deleteForever}
    onconfirm={async () => {
      if (c === 'empty') await api.emptyTrash(vol)
      else await api.purge(vol, c.map((x) => x.id))
      toast(c === 'empty' ? t.trashEmptied : t.purged(what(c)))
      selected.clear()
      await load()
    }}
    onclose={() => (confirm = null)}
  />
{/if}
