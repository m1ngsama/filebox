<script lang="ts" module>
  import type { Component } from 'svelte'
  export type Action = { id: string; label: string; icon: Component<{ size?: number }>; danger?: boolean }
</script>

<script lang="ts">
  import { untrack, type Snippet } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { ContextMenu, DropdownMenu } from 'bits-ui'
  import { createVirtualizer } from '@tanstack/svelte-virtual'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import FileIcon from './FileIcon.svelte'
  import type { Entry } from '../lib/api'
  import { size, date, ago } from '../lib/format'
  import { t } from '../lib/i18n'

  type Sort = 'name' | 'size' | 'mtime'
  let {
    entries,
    grid,
    selected,
    sort = $bindable('name'),
    desc = $bindable(false),
    thumb,
    actions,
    onaction,
    onopen,
    batch,
    empty,
  }: {
    entries: Entry[]
    grid: boolean
    selected?: SvelteSet<string>
    sort?: Sort
    desc?: boolean
    thumb: (e: Entry) => string | null
    actions: (e: Entry | null) => Action[]
    onaction: (id: string, e: Entry | null) => void
    onopen: (e: Entry) => void
    batch?: Snippet
    empty: string
  } = $props()

  const broken = new SvelteSet<string>()
  let scroller = $state<HTMLDivElement>()
  let width = $state(0)
  let ctx = $state<Entry | null>(null)
  let anchor = -1

  const cols = $derived(grid ? Math.max(1, Math.floor((width - 16) / 172)) : 1)
  const rows = $derived(Math.ceil(entries.length / cols))
  const all = $derived(!!selected && entries.length > 0 && entries.every((e) => selected.has(e.name)))

  const v = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scroller ?? null,
    estimateSize: () => 48,
    overscan: 6,
  })

  $effect(() => {
    const list = entries
    const opts = {
      count: rows,
      estimateSize: () => (grid ? 212 : 48),
      getItemKey: grid ? (i: number) => i : (i: number) => list[i]?.name ?? i,
    }
    untrack(() => {
      $v.setOptions(opts)
      $v.measure()
    })
  })

  function by(k: Sort) {
    desc = sort === k ? !desc : k !== 'name'
    sort = k
  }

  function toggle(n: string) {
    if (!selected) return
    if (selected.has(n)) selected.delete(n)
    else selected.add(n)
  }

  function pick(ev: MouseEvent, i: number) {
    if (!selected || (ev.target as Element).closest('button, input, a')) return
    if (ev.shiftKey && anchor >= 0) {
      for (const e of entries.slice(Math.min(anchor, i), Math.max(anchor, i) + 1)) selected.add(e.name)
      return
    }
    anchor = i
    toggle(entries[i].name)
  }

  function selectAll() {
    if (!selected) return
    if (all) selected.clear()
    else for (const e of entries) selected.add(e.name)
  }

  const src = (e: Entry) => (e.dir || broken.has(e.name) ? null : thumb(e))
</script>

{#snippet items(e: Entry | null)}
  {#each actions(e) as a (a.id)}
    {#if a.danger}<DropdownMenu.Separator class="menu-sep" />{/if}
    <DropdownMenu.Item class={a.danger ? 'menu-item danger' : 'menu-item'} onSelect={() => onaction(a.id, e)}>
      <a.icon size={16} />{a.label}
    </DropdownMenu.Item>
  {/each}
{/snippet}

{#snippet more(e: Entry)}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger class="icon-btn more" aria-label={`${e.name} ${t.actions}`}><Ellipsis size={18} /></DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content class="menu" preventScroll={false} align="end" sideOffset={4}>{@render items(e)}</DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
{/snippet}

{#snippet check(e: Entry, cls: string)}
  {#if selected}
    <input type="checkbox" class={cls} checked={selected.has(e.name)} onchange={() => toggle(e.name)} aria-label={t.select(e.name)} />
  {:else}
    <span></span>
  {/if}
{/snippet}

<div class="list-head" class:grid class:batch={!!selected?.size}>
  {#if selected}
    <input
      type="checkbox"
      checked={all}
      indeterminate={!all && selected.size > 0}
      onchange={selectAll}
      aria-label={t.selectAll}
      disabled={!entries.length}
    />
  {:else}
    <span></span>
  {/if}
  {#if selected?.size && batch}
    <span class="count">{t.selected(selected.size)}</span>
    {@render batch()}
  {:else}
    <span></span>
    {#each [['name', t.name], ['size', t.size], ['mtime', t.mtime]] as [k, label] (k)}
      <button class={`sort ${k}`} aria-pressed={sort === k} onclick={() => by(k as Sort)}>
        {label}
        {#if sort === k}{#if desc}<ArrowDown size={14} />{:else}<ArrowUp size={14} />{/if}{/if}
      </button>
    {/each}
  {/if}
</div>

<ContextMenu.Root onOpenChange={(o) => !o && (ctx = null)}>
  <ContextMenu.Trigger>
    {#snippet child({ props })}
      <div {...props} class="scroller" bind:this={scroller} bind:clientWidth={width} oncontextmenucapture={() => (ctx = null)}>
        {#if !entries.length}<p class="empty">{empty}</p>{/if}
        <div class="spacer" style:height={`${$v.getTotalSize()}px`}>
          {#each $v.getVirtualItems().filter((r) => r.index < rows) as r (r.key)}
            {#if grid}
              <div class="cards" style:transform={`translateY(${r.start}px)`} style:grid-template-columns={`repeat(${cols}, minmax(0, 1fr))`}>
                {#each entries.slice(r.index * cols, r.index * cols + cols) as e, j (e.name)}
                  {@const s = src(e)}
                  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
                  <div class="card" class:sel={selected?.has(e.name)} onclick={(ev) => pick(ev, r.index * cols + j)} oncontextmenu={() => (ctx = e)}>
                    {@render check(e, 'card-check')}
                    <button class="card-open" onclick={() => onopen(e)} title={e.name}>
                      {#if s}<img src={s} alt="" onerror={() => broken.add(e.name)} />{:else}<FileIcon name={e.name} dir={e.dir} size={56} />{/if}
                    </button>
                    <div class="card-foot">
                      <span class="card-name" title={e.name}>{e.name}</span>
                      {@render more(e)}
                    </div>
                  </div>
                {/each}
              </div>
            {:else}
              {@const e = entries[r.index]}
              {@const s = src(e)}
              <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
              <div
                class="row"
                class:sel={selected?.has(e.name)}
                style:transform={`translateY(${r.start}px)`}
                onclick={(ev) => pick(ev, r.index)}
                oncontextmenu={() => (ctx = e)}
              >
                {@render check(e, '')}
                <span class="thumb">
                  {#if s}<img src={s} alt="" onerror={() => broken.add(e.name)} />{:else}<FileIcon name={e.name} dir={e.dir} />{/if}
                </span>
                <button class="name" onclick={() => onopen(e)} title={e.name}>{e.name}</button>
                {@render more(e)}
                <span class="num size">{e.dir ? '' : size(e.size)}</span>
                <span class="num mtime" title={date(e.mtime)}>{ago(e.mtime)}</span>
              </div>
            {/if}
          {/each}
        </div>
      </div>
    {/snippet}
  </ContextMenu.Trigger>
  <ContextMenu.Portal>
    <ContextMenu.Content class="menu" preventScroll={false}>{@render items(ctx)}</ContextMenu.Content>
  </ContextMenu.Portal>
</ContextMenu.Root>
