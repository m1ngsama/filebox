<script lang="ts">
  import TagIcon from '@lucide/svelte/icons/tag'
  import Pencil from '@lucide/svelte/icons/pencil'
  import ArrowLeft from '@lucide/svelte/icons/arrow-left'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import EmptyState from '../components/EmptyState.svelte'
  import Modal from '../components/Modal.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import { api, filesURL, rawURL, thumbURL, type Entry, type Favorite, type Tag } from '../lib/api'
  import { navigate, link } from '../lib/router.svelte'
  import { tags, tagColors, loadTags, tagOf } from '../lib/tags.svelte'
  import { toast, fail } from '../lib/toast.svelte'
  import { thumbable, rawThumb, arrange, type Sort } from '../lib/format'
  import { folderAction, downloadAction, actOn } from '../lib/located'
  import { icon } from '../lib/icon'
  import { t } from '../lib/i18n'

  let { id }: { id?: number } = $props()

  let loaded = $state(false)
  let error = $state('')
  let items = $state.raw<Favorite[]>([])
  let itemsLoaded = $state(false)
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let editing = $state<{ tag: Tag; name: string; color: string } | null>(null)
  let deleting = $state<Tag | null>(null)

  const current = $derived(id ? tagOf(id) : undefined)
  const shown = $derived(arrange(items, '', sort, desc))
  const loc = (e: Entry) => e as Favorite

  $effect(() => {
    loadTags(true).then(() => (loaded = true))
  })
  $effect(() => {
    if (!id) return
    itemsLoaded = false
    api.tagged(id).then(
      (r) => ((items = r.entries), (itemsLoaded = true)),
      (e: Error) => (error = e.message),
    )
  })

  const untag: Action = { id: 'untag', label: t.removeTag(''), icon: TagIcon, danger: true }

  function onaction(a: string, e: Entry | null) {
    if (!e || !id) return
    const f = loc(e)
    if (a === 'untag') api.applyTag(id, f.vol, [f.path], false).then(() => ((items = items.filter((x) => x !== f)), loadTags(true)), fail)
    else actOn(a, f)
  }

  function open(e: Entry) {
    const f = loc(e)
    if (f.missing) return
    if (f.dir) navigate(filesURL(f.vol, f.path))
    else actOn('folder', f)
  }

  async function save() {
    if (!editing) return
    try {
      await api.editTag(editing.tag.id, editing.name.trim(), editing.color)
      await loadTags(true)
      editing = null
    } catch (e) {
      fail(e)
    }
  }
</script>

{#if id}
  <div class="tag-head">
    <a class="icon-btn" href="/tags" onclick={link} aria-label={t.tags}><ArrowLeft size={icon.md} /></a>
    {#if current}
      <span class="tag-chip big" data-color={current.color}>{current.name}</span>
      <button class="icon-btn" aria-label={t.editTag} onclick={() => (editing = { tag: current, name: current.name, color: current.color })}><Pencil size={icon.sm} /></button>
    {/if}
  </div>
  <section class="files" aria-label={current?.name ?? t.tags}>
    <EntryList
      entries={shown}
      grid={false}
      bind:sort
      bind:desc
      thumb={(e) => (!e.dir && !loc(e).missing && thumbable(e.name) ? thumbURL(loc(e).vol, loc(e).path) : null)}
      raw={(e) => (!loc(e).missing && rawThumb(e) ? rawURL(loc(e).vol, loc(e).path) : null)}
      actions={(e) => (!e ? [] : loc(e).missing ? [untag] : [folderAction, downloadAction, { ...untag, label: t.removeTag(current?.name ?? '') }])}
      {onaction}
      onopen={open}
      loading={!itemsLoaded && !error}
      id={(e) => `${loc(e).vol}:${loc(e).path}`}
      loc={loc}
      dim={(e) => (loc(e).missing ? t.missing : undefined)}
    >
      {#snippet empty()}
        {#if error}<EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error} />{:else}<EmptyState icon={TagIcon} as="h2" title={t.taggedEmpty} />{/if}
      {/snippet}
    </EntryList>
  </section>
{:else if loaded && !tags.list.length}
  <div class="page"><EmptyState icon={TagIcon} as="h2" title={t.tagsEmpty} hint={t.tagsEmptyHint} /></div>
{:else}
  <div class="page">
    <ul class="rows tag-list" aria-label={t.tags}>
      {#each tags.list as x (x.id)}
        <li>
          <span class="tag-dot" data-color={x.color}></span>
          <a class="row-main" href={`/tags/${x.id}`} onclick={link}><span class="row-title">{x.name}</span><span class="hint">{t.tagCount(x.count)}</span></a>
          <button class="icon-btn" aria-label={`${t.editTag} ${x.name}`} onclick={() => (editing = { tag: x, name: x.name, color: x.color })}><Pencil size={icon.sm} /></button>
        </li>
      {/each}
    </ul>
  </div>
{/if}

{#if editing}
  {@const ed = editing}
  <Modal title={t.editTag} onclose={() => (editing = null)} onsubmit={save}>
    <label class="field"><span>{t.tagName}</span><input bind:value={ed.name} maxlength="64" autocomplete="off" /></label>
    <div class="field">
      <span>{t.tagColor}</span>
      <div class="swatches" role="radiogroup" aria-label={t.tagColor}>
        {#each tagColors as c (c)}
          <button type="button" role="radio" class="swatch" data-color={c} aria-checked={ed.color === c} aria-label={t.tagColors[c]} title={t.tagColors[c]} onclick={() => (ed.color = c)}></button>
        {/each}
      </div>
    </div>
    {#snippet footer()}
      <button type="button" class="ghost danger" onclick={() => ((deleting = ed.tag), (editing = null))}>{t.deleteTag}</button>
      <span class="spacer"></span>
      <button type="button" onclick={() => (editing = null)}>{t.cancel}</button>
      <button class="primary" disabled={!ed.name.trim()}>{t.save}</button>
    {/snippet}
  </Modal>
{/if}

{#if deleting}
  {@const d = deleting}
  <ConfirmDialog
    title={t.deleteTag}
    message={t.deleteTagMessage(d.name, d.count)}
    action={t.deleteTag}
    onconfirm={async () => {
      await api.deleteTag(d.id)
      await loadTags(true)
      toast(t.tagDeleted)
      if (id === d.id) navigate('/tags')
    }}
    onclose={() => (deleting = null)}
  />
{/if}

<style>
  .tag-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4) 0;
  }
  .tag-chip.big {
    max-width: none;
    font-size: var(--text-md);
    line-height: 28px;
    padding: 0 var(--space-3);
  }
  .tag-list a.row-main {
    color: inherit;
    text-decoration: none;
  }
  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .swatch {
    width: 28px;
    height: 28px;
    padding: 0;
    border: 2px solid transparent;
    border-radius: 50%;
    background: var(--tag) content-box;
    outline: 1px solid var(--line);
  }
  .swatch[data-color=''] {
    background: linear-gradient(135deg, transparent 45%, var(--muted) 45% 55%, transparent 55%) content-box;
  }
  .swatch[aria-checked='true'] {
    border-color: var(--bg);
    outline: 2px solid var(--accent);
  }
  .spacer {
    flex: 1;
  }
</style>
