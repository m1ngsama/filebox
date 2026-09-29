<script lang="ts" module>
  import type { Component } from 'svelte'
  export type Action = { id: string; label: string; icon: Component<{ size?: number }>; danger?: boolean }
</script>

<script lang="ts">
  import { tick, untrack, type Snippet } from 'svelte'
  import { SvelteSet, SvelteMap } from 'svelte/reactivity'
  import { ContextMenu, DropdownMenu } from 'bits-ui'
  import { createVirtualizer } from '@tanstack/svelte-virtual'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import FileIcon from './FileIcon.svelte'
  import type { Entry } from '../lib/api'
  import { size, date, ago, look, fallback, flip, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { narrow } from '../lib/shell.svelte'

  let {
    entries,
    grid,
    selected,
    sort = $bindable('name'),
    desc = $bindable(false),
    thumb,
    raw,
    actions,
    onaction,
    onopen,
    batch,
    empty,
    loading = false,
    id = (e) => e.name,
    sub,
  }: {
    entries: Entry[]
    grid: boolean
    selected?: SvelteSet<string>
    sort?: Sort
    desc?: boolean
    thumb: (e: Entry) => string | null
    raw?: (e: Entry) => string | null
    actions: (e: Entry | null) => Action[]
    onaction: (id: string, e: Entry | null) => void
    onopen: (e: Entry) => void
    batch?: Snippet
    empty?: Snippet
    loading?: boolean
    id?: (e: Entry) => string
    sub?: (e: Entry) => string
  } = $props()

  const broken = new SvelteMap<string, number>()
  let scroller = $state<HTMLDivElement>()
  let width = $state(0)
  let ctx = $state.raw<Entry | null>(null)
  let anchor = -1
  let cur = $state(0)
  let want = -1
  let touch = false
  let swallow = false
  let timer = 0
  let origin = [0, 0]
  let pressing = $state(-1)
  let held = $state(false)

  const cols = $derived(grid ? Math.max(1, Math.floor((width - 16) / 172)) : 1)
  const rows = $derived(Math.ceil(entries.length / cols))
  const tab = $derived(Math.min(cur, entries.length - 1))
  const all = $derived(!!selected && entries.length > 0 && entries.every((e) => selected.has(id(e))))

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
      estimateSize: () => (grid ? 212 : narrow.current ? 56 : 48),
      getItemKey: grid ? (i: number) => i : (i: number) => (list[i] ? id(list[i]) : i),
    }
    untrack(() => {
      $v.setOptions(opts)
      $v.measure()
    })
  })

  function by(k: Sort) {
    desc = flip(sort, desc, k)
    sort = k
  }

  function toggle(n: string) {
    if (!selected) return
    if (selected.has(n)) selected.delete(n)
    else selected.add(n)
  }

  function press(ev: PointerEvent, i: number) {
    touch = ev.pointerType !== 'mouse'
    swallow = false
    if (!touch || !selected || (ev.target as Element).closest('input, .more')) return
    held = true
    origin = [ev.clientX, ev.clientY]
    pressing = i
    const key = id(entries[i])
    clearTimeout(timer)
    timer = setTimeout(() => {
      pressing = -1
      swallow = true
      anchor = entries.findIndex((e) => id(e) === key)
      selected.add(key)
      navigator.vibrate?.(10)
    }, 450)
  }

  $effect(() => () => clearTimeout(timer))

  function release() {
    clearTimeout(timer)
    pressing = -1
    held = false
  }

  function drift(ev: PointerEvent) {
    if (pressing >= 0 && Math.hypot(ev.clientX - origin[0], ev.clientY - origin[1]) > 10) release()
  }

  function menu(ev: MouseEvent, e: Entry) {
    if (touch && selected) ev.preventDefault()
    else ctx = e
  }

  function tap(i: number) {
    if (swallow) return void (swallow = false)
    if (touch && selected?.size) toggle(id(entries[i]))
    else onopen(entries[i])
  }

  function pick(ev: MouseEvent, i: number) {
    if (!selected || (ev.target as Element).closest('button, input, a')) return
    if (touch) return tap(i)
    if (ev.shiftKey && anchor >= 0) {
      getSelection()?.removeAllRanges()
      for (const e of entries.slice(Math.min(anchor, i), Math.max(anchor, i) + 1)) selected.add(id(e))
      return
    }
    anchor = i
    toggle(id(entries[i]))
  }

  function focusWanted() {
    const el = scroller?.querySelector<HTMLElement>(`[data-i="${want}"]`)
    if (!el) return
    el.focus()
    want = -1
  }

  $effect(() => {
    $v.getVirtualItems()
    if (want >= 0) tick().then(focusWanted)
  })

  function key(ev: KeyboardEvent, i: number) {
    if (ev.target !== ev.currentTarget) return
    const step = { ArrowDown: cols, ArrowUp: -cols, ArrowRight: grid ? 1 : 0, ArrowLeft: grid ? -1 : 0 }[ev.key]
    if (ev.key === ' ') {
      ev.preventDefault()
      anchor = i
      toggle(id(entries[i]))
    } else if (ev.key === 'Enter') {
      ev.preventDefault()
      onopen(entries[i])
    } else if (step !== undefined || ev.key === 'Home' || ev.key === 'End') {
      ev.preventDefault()
      const to = ev.key === 'Home' ? 0 : ev.key === 'End' ? entries.length - 1 : Math.min(entries.length - 1, Math.max(0, i + step!))
      cur = want = to
      $v.scrollToIndex(Math.floor(to / cols))
      focusWanted()
    }
  }

  function selectAll() {
    if (!selected) return
    if (all) selected.clear()
    else for (const e of entries) selected.add(id(e))
  }

  const src = (e: Entry) => (e.dir ? null : fallback([thumb(e), raw?.(e)], broken.get(id(e)) ?? 0))
  const miss = (e: Entry) => broken.set(id(e), (broken.get(id(e)) ?? 0) + 1)
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
    <input type="checkbox" class={cls} checked={selected.has(id(e))} onchange={() => toggle(id(e))} aria-label={t.select(e.name)} />
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
    <button class="ghost select-all" onclick={selectAll}>{all ? t.selectNone : t.selectAll}</button>
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
  <ContextMenu.Trigger disabled={held || (!ctx && !actions(null).length)}>
    {#snippet child({ props })}
      <div {...props} class="scroller" class:selecting={!!selected?.size} bind:this={scroller} bind:clientWidth={width} oncontextmenucapture={() => (ctx = null)} onscroll={release}>
        {#if !entries.length && loading}
          <div class="skeleton" class:grid role="status" aria-label={t.loading}>
            {#each { length: grid ? 12 : 10 }, i (i)}
              <div class={grid ? 'sk-card' : 'sk-row'}><span class="sk sk-icon"></span><span class="sk sk-line" style:width={`${30 + ((i * 37) % 45)}%`}></span></div>
            {/each}
          </div>
        {:else if !entries.length && empty}<div class="empty">{@render empty()}</div>{/if}
        <div class="spacer" role="grid" aria-label={t.fileList} aria-multiselectable={selected ? true : undefined} aria-rowcount={rows} style:height={`${$v.getTotalSize()}px`}>
          {#each $v.getVirtualItems().filter((r) => r.index < rows) as r (r.key)}
            {#if grid}
              <div class="cards" role="row" aria-rowindex={r.index + 1} style:transform={`translateY(${r.start}px)`} style:grid-template-columns={`repeat(${cols}, minmax(0, 1fr))`}>
                {#each entries.slice(r.index * cols, r.index * cols + cols) as e, j (id(e))}
                  {@const s = src(e)}
                  {@const i = r.index * cols + j}
                  <div
                    class="card"
                    class:sel={selected?.has(id(e))}
                    role="gridcell"
                    aria-selected={selected ? selected.has(id(e)) : undefined}
                    tabindex={i === tab ? 0 : -1}
                    data-i={i}
                    class:pressing={pressing === i}
                    onclick={(ev) => pick(ev, i)}
                    onkeydown={(ev) => key(ev, i)}
                    onfocus={() => (cur = i)}
                    oncontextmenu={(ev) => menu(ev, e)}
                    onpointerdown={(ev) => press(ev, i)}
                    onpointermove={drift}
                    onpointerup={release}
                    onpointercancel={release}
                  >
                    {@render check(e, 'card-check')}
                    <button class="card-open" data-look={e.dir ? 'dir' : look(e.name)} onclick={() => tap(i)} title={e.name}>
                      {#if s}<img src={s} alt="" loading="lazy" decoding="async" onerror={() => miss(e)} />{:else}<FileIcon name={e.name} dir={e.dir} size={56} />{/if}
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
              <div
                class="row"
                class:sel={selected?.has(id(e))}
                style:transform={`translateY(${r.start}px)`}
                role="row"
                aria-rowindex={r.index + 1}
                aria-selected={selected ? selected.has(id(e)) : undefined}
                tabindex={r.index === tab ? 0 : -1}
                data-i={r.index}
                class:pressing={pressing === r.index}
                onclick={(ev) => pick(ev, r.index)}
                onkeydown={(ev) => key(ev, r.index)}
                onfocus={() => (cur = r.index)}
                oncontextmenu={(ev) => menu(ev, e)}
                onpointerdown={(ev) => press(ev, r.index)}
                onpointermove={drift}
                onpointerup={release}
                onpointercancel={release}
              >
                <span class="cell check-cell" role="gridcell">{@render check(e, '')}</span>
                <span class="thumb" role="gridcell">
                  {#if s}<img src={s} alt="" loading="lazy" decoding="async" onerror={() => miss(e)} />{:else}<FileIcon name={e.name} dir={e.dir} />{/if}
                </span>
                <span class="cell name-cell" role="gridcell">
                  <button class="name" onclick={() => tap(r.index)} title={e.name}>{e.name}</button>
                  {#if narrow.current}
                    <span class="hint sub">{e.dir ? '' : `${size(e.size)} · `}{ago(e.mtime)}{sub ? ` · ${sub(e)}` : ''}</span>
                  {:else if sub}
                    <span class="hint sub" title={sub(e)}>{sub(e)}</span>
                  {/if}
                </span>
                <span class="cell" role="gridcell">{@render more(e)}</span>
                <span class="num size" role="gridcell">{e.dir ? '' : size(e.size)}</span>
                <span class="num mtime" role="gridcell" title={date(e.mtime)}>{ago(e.mtime)}</span>
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
