<script lang="ts">
  import { untrack } from 'svelte'
  import { SvelteMap } from 'svelte/reactivity'
  import Modal from './Modal.svelte'
  import Check from '@lucide/svelte/icons/check'
  import Minus from '@lucide/svelte/icons/minus'
  import Plus from '@lucide/svelte/icons/plus'
  import { api } from '../lib/api'
  import { tags, tagColors, loadTags } from '../lib/tags.svelte'
  import { fail } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  let { vol, items, onchange, onclose }: { vol: string; items: { path: string; tags?: number[] }[]; onchange: () => void; onclose: () => void } = $props()

  const have = new SvelteMap<number, number>()
  untrack(() => {
    for (const it of items) for (const id of it.tags ?? []) have.set(id, (have.get(id) ?? 0) + 1)
  })
  let query = $state('')
  let busy = $state(false)
  loadTags()

  const q = $derived(query.trim().replace(/\s+/g, ' '))
  const shown = $derived(tags.list.filter((x) => x.name.toLowerCase().includes(q.toLowerCase())))
  const exact = $derived(tags.list.find((x) => x.name.toLowerCase() === q.toLowerCase()))
  const paths = $derived(items.map((x) => x.path))

  async function toggle(id: number) {
    const on = (have.get(id) ?? 0) < items.length
    busy = true
    try {
      await api.applyTag(id, vol, paths, on)
      have.set(id, on ? items.length : 0)
      loadTags(true)
      onchange()
    } catch (e) {
      fail(e)
    }
    busy = false
  }

  async function create() {
    if (!q) return
    busy = true
    try {
      const used = new Set(tags.list.map((x) => x.color))
      const tag = await api.newTag(q, tagColors.slice(1).find((c) => !used.has(c)) ?? tagColors[(tags.list.length % (tagColors.length - 1)) + 1])
      await loadTags(true)
      busy = false
      query = ''
      await toggle(tag.id)
    } catch (e) {
      fail(e)
      busy = false
    }
  }

  function submit() {
    if (exact) toggle(exact.id)
    else if (q) create()
    else onclose()
  }
</script>

<Modal title={items.length === 1 ? t.tagsOf(items[0].path.split('/').at(-1)!) : t.tagsOfMany(items.length)} {onclose} onsubmit={submit}>
  <input class="tag-query" bind:value={query} placeholder={t.tagSearch} aria-label={t.tagSearch} autocomplete="off" maxlength="64" />
  <ul class="tag-options" aria-label={t.tags}>
    {#each shown as x (x.id)}
      {@const n = have.get(x.id) ?? 0}
      <li>
        <button type="button" role="checkbox" aria-checked={n === items.length ? 'true' : n ? 'mixed' : 'false'} disabled={busy} onclick={() => toggle(x.id)}>
          <span class="tag-box">{#if n === items.length}<Check size={14} />{:else if n}<Minus size={14} />{/if}</span>
          <span class="tag-dot" data-color={x.color}></span>
          <span class="tag-name">{x.name}</span>
          <span class="hint">{x.count}</span>
        </button>
      </li>
    {/each}
    {#if q && !exact}
      <li>
        <button type="button" disabled={busy} onclick={create}><span class="tag-box"><Plus size={14} /></span><span class="tag-name">{t.newTag(q)}</span></button>
      </li>
    {/if}
    {#if !q && !tags.list.length}<li class="hint tag-empty">{t.noTagsYet}</li>{/if}
  </ul>
  {#snippet footer()}
    <button type="button" class="primary" onclick={onclose}>{t.done}</button>
  {/snippet}
</Modal>

<style>
  .tag-query {
    width: 100%;
  }
  .tag-options {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: 0;
    max-height: 300px;
    overflow: auto;
  }
  .tag-options button {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: 40px;
    padding: 0 var(--space-2);
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    text-align: left;
  }
  .tag-box {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    flex: none;
    border: 1.5px solid var(--muted);
    border-radius: 5px;
  }
  [aria-checked='true'] .tag-box,
  [aria-checked='mixed'] .tag-box {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--on-accent);
  }
  .tag-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (hover: hover) and (pointer: fine) {
    .tag-options button:not(:disabled):hover {
      background: var(--hover);
    }
  }
  .tag-empty {
    padding: var(--space-3) var(--space-2);
  }
</style>
