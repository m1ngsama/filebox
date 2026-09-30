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
  import { selectURL, type Entry, type Loc } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { size, date, ago, look, fallback, flip, sorts, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { narrow } from '../lib/shell.svelte'
  import { carry, drop, target, type Carried, type Target } from '../lib/dnd'

  const inert: Target = { accepts: () => false, drop: () => {} }

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
    footer,
    head = true,
    loading = false,
    busy = false,
    id = (e) => e.name,
    loc,
    group,
    reveal,
    dim,
    dnd,
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
    footer?: Snippet
    head?: boolean
    loading?: boolean
    busy?: boolean
    id?: (e: Entry) => string
    loc?: (e: Entry) => Loc
    group?: (e: Entry) => string
    reveal?: { name: string; center?: boolean }
    dim?: (e: Entry) => string | undefined
    dnd?: { carry: (e: Entry) => Carried; target: (e: Entry) => Target }
  } = $props()

  const draggable = $derived(!!dnd && !narrow.current)
  const dropOn = (e: Entry) => (dnd && e.dir ? dnd.target(e) : inert)
  const lift = (ev: DragEvent, e: Entry) => dnd && carry(ev, dnd.carry(e))

  const broken = new SvelteMap<string, number>()
  let scroller = $state<HTMLDivElement>()
  let width = $state(0)
  let ctx = $state.raw<Entry | null>(null)
  let anchor = ''
  let cur = $state('')
  let inside = false
  let want = -1
  let touch = false
  let swallow = false
  let timer = 0
  let origin = [0, 0]
  let pressing = $state(-1)
  let held = $state(false)

  const cols = $derived(grid ? Math.max(1, Math.floor((width - 16) / 172)) : 1)
  const layout = $derived.by(() => {
    if (!group || grid) return null
    const items: (string | number)[] = []
    const rowOf = new Int32Array(entries.length)
    let last: string | undefined
    entries.forEach((e, i) => {
      const g = group(e)
      if (g !== last) items.push((last = g))
      rowOf[i] = items.length
      items.push(i)
    })
    return { items, rowOf }
  })
  const rows = $derived(layout ? layout.items.length : Math.ceil(entries.length / cols))
  const rowOf = (i: number) => (layout ? layout.rowOf[i] : Math.floor(i / cols))
  const tab = $derived(Math.max(0, cur ? entries.findIndex((e) => id(e) === cur) : 0))
  const all = $derived(!!selected && entries.length > 0 && entries.every((e) => selected.has(id(e))))

  const v = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scroller ?? null,
    estimateSize: () => 48,
    overscan: 6,
  })

  $effect(() => {
    const list = entries
    const items = layout?.items
    const h = grid ? 212 : narrow.current ? 56 : 48
    const opts = {
      count: rows,
      estimateSize: (i: number) => (typeof items?.[i] === 'string' ? 36 : h),
      getItemKey: grid ? (i: number) => i
        : items ? (i: number) => (typeof items[i] === 'string' ? `\0${items[i]}` : id(list[items[i] as number]))
        : (i: number) => (list[i] ? id(list[i]) : i),
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
      anchor = key
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
    const a = anchor ? entries.findIndex((e) => id(e) === anchor) : -1
    if (ev.shiftKey && a >= 0) {
      getSelection()?.removeAllRanges()
      for (const e of entries.slice(Math.min(a, i), Math.max(a, i) + 1)) selected.add(id(e))
      return
    }
    anchor = id(entries[i])
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
      anchor = id(entries[i])
      toggle(id(entries[i]))
    } else if (ev.key === 'Enter') {
      ev.preventDefault()
      onopen(entries[i])
    } else if (step !== undefined || ev.key === 'Home' || ev.key === 'End') {
      ev.preventDefault()
      const to = ev.key === 'Home' ? 0 : ev.key === 'End' ? entries.length - 1 : Math.min(entries.length - 1, Math.max(0, i + step!))
      cur = id(entries[to])
      want = to
      $v.scrollToIndex(rowOf(to))
      focusWanted()
    }
  }

  $effect(() => {
    entries
    if (!untrack(() => inside && cur)) return
    tick().then(() => {
      if (document.activeElement !== document.body) return
      const i = entries.findIndex((e) => id(e) === cur)
      if (i < 0) return
      want = i
      $v.scrollToIndex(rowOf(i))
      tick().then(focusWanted)
    })
  })

  let revealed: typeof reveal
  $effect(() => {
    const r = reveal
    if (!r || r === revealed) return
    const i = entries.findIndex((e) => id(e) === r.name)
    if (i < 0) return
    revealed = r
    untrack(() => {
      cur = r.name
      want = i
      $v.scrollToIndex(rowOf(i), r.center ? { align: 'center' } : undefined)
      tick().then(focusWanted)
    })
  })

  function selectAll() {
    if (!selected) return
    if (all) selected.clear()
    else for (const e of entries) selected.add(id(e))
  }

  const src = (e: Entry) => (e.dir ? null : fallback([thumb(e), raw?.(e)], broken.get(id(e)) ?? 0))
  const cancel = (img: HTMLImageElement) => () => img.removeAttribute('src')
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

{#snippet trail(e: Entry)}
  {#if loc}
    {@const l = loc(e)}
    {@const segs = l.path.split('/')}
    {#each [l.vol, ...segs.slice(0, -1)] as s, i (i)}{#if i}<span class="slash">›</span>{/if}<a
        href={selectURL(l.vol, segs.slice(0, i + 1).join('/'))}
        tabindex="-1"
        onclick={link}>{s}</a
      >{/each}
  {/if}
{/snippet}

{#snippet check(e: Entry, cls: string)}
  {#if selected}
    <input type="checkbox" class={cls} checked={selected.has(id(e))} onchange={() => toggle(id(e))} aria-label={t.select(e.name)} />
  {:else}
    <span></span>
  {/if}
{/snippet}

{#if head}
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
    {#each sorts as [k, label] (k)}
      <button class={`sort ${k}`} aria-pressed={sort === k} onclick={() => by(k)}>
        {label}
        {#if sort === k}{#if desc}<ArrowDown size={14} />{:else}<ArrowUp size={14} />{/if}{/if}
      </button>
    {/each}
  {/if}
</div>
{/if}

<ContextMenu.Root onOpenChange={(o) => !o && (ctx = null)}>
  <ContextMenu.Trigger disabled={held || (!ctx && !actions(null).length)}>
    {#snippet child({ props })}
      <div {...props} class="scroller" class:selecting={!!selected?.size} bind:this={scroller} bind:clientWidth={width} oncontextmenucapture={() => (ctx = null)} onscroll={release} onfocusin={() => (inside = true)} onfocusout={(ev) => (inside = !ev.relatedTarget || !!scroller?.contains(ev.relatedTarget as Node))}>
        {#if !entries.length && loading}
          <div class="skeleton" class:grid role="status" aria-label={t.loading}>
            {#each { length: grid ? 12 : 10 }, i (i)}
              <div class={grid ? 'sk-card' : 'sk-row'}><span class="sk sk-icon"></span><span class="sk sk-line" style:width={`${30 + ((i * 37) % 45)}%`}></span></div>
            {/each}
          </div>
        {:else if !entries.length && empty}<div class="empty">{@render empty()}</div>{/if}
        <div class="spacer" role="grid" aria-label={t.fileList} aria-multiselectable={selected ? true : undefined} aria-busy={busy || undefined} aria-rowcount={rows} style:height={`${$v.getTotalSize()}px`}>
          {#each $v.getVirtualItems().filter((r) => r.index < rows) as r (r.key)}
            {#if grid}
              <div class="cards" role="row" aria-rowindex={r.index + 1} style:transform={`translateY(${r.start}px)`} style:grid-template-columns={`repeat(${cols}, minmax(0, 1fr))`}>
                {#each entries.slice(r.index * cols, r.index * cols + cols) as e, j (id(e))}
                  {@const s = src(e)}
                  {@const i = r.index * cols + j}
                  <div
                    class="card"
                    {draggable}
                    ondragstart={(ev) => lift(ev, e)}
                    ondragend={drop}
                    use:target={dropOn(e)}
                    class:sel={selected?.has(id(e))}
                    role="gridcell"
                    aria-selected={selected ? selected.has(id(e)) : undefined}
                    tabindex={i === tab ? 0 : -1}
                    data-i={i}
                    class:pressing={pressing === i}
                    onclick={(ev) => pick(ev, i)}
                    onkeydown={(ev) => key(ev, i)}
                    onfocus={() => (cur = id(e))}
                    oncontextmenu={(ev) => menu(ev, e)}
                    onpointerdown={(ev) => press(ev, i)}
                    onpointermove={drift}
                    onpointerup={release}
                    onpointercancel={release}
                  >
                    {@render check(e, 'card-check')}
                    <button class="card-open" data-look={e.dir ? 'dir' : look(e.name)} onclick={() => tap(i)} title={e.name}>
                      {#if s}<img src={s} alt="" draggable="false" loading="lazy" decoding="async" onerror={() => miss(e)} {@attach cancel} />{:else}<FileIcon name={e.name} dir={e.dir} size={56} />{/if}
                    </button>
                    <div class="card-foot">
                      <span class="card-name" title={e.name}>{e.name}</span>
                      {@render more(e)}
                    </div>
                  </div>
                {/each}
              </div>
            {:else if typeof layout?.items[r.index] === 'string'}
              <div class="group-head" role="row" aria-rowindex={r.index + 1} style:transform={`translateY(${r.start}px)`}>
                <span role="columnheader">{layout?.items[r.index]}</span>
              </div>
            {:else}
              {@const n = layout ? (layout.items[r.index] as number) : r.index}
              {@const e = entries[n]}
              {@const s = src(e)}
              {@const gone = dim?.(e)}
              <div
                class="row"
                class:dim={!!gone}
                {draggable}
                ondragstart={(ev) => lift(ev, e)}
                ondragend={drop}
                use:target={dropOn(e)}
                class:sel={selected?.has(id(e))}
                style:transform={`translateY(${r.start}px)`}
                role="row"
                aria-rowindex={r.index + 1}
                aria-selected={selected ? selected.has(id(e)) : undefined}
                tabindex={n === tab ? 0 : -1}
                data-i={n}
                class:pressing={pressing === n}
                onclick={(ev) => pick(ev, n)}
                onkeydown={(ev) => key(ev, n)}
                onfocus={() => (cur = id(e))}
                oncontextmenu={(ev) => menu(ev, e)}
                onpointerdown={(ev) => press(ev, n)}
                onpointermove={drift}
                onpointerup={release}
                onpointercancel={release}
              >
                <span class="cell check-cell" role="gridcell">{@render check(e, '')}</span>
                <span class="thumb" role="gridcell">
                  {#if s}<img src={s} alt="" draggable="false" loading="lazy" decoding="async" onerror={() => miss(e)} {@attach cancel} />{:else}<FileIcon name={e.name} dir={e.dir} />{/if}
                </span>
                <span class="cell name-cell" role="gridcell">
                  <button class="name" onclick={() => tap(n)} title={e.name}>{e.name}</button>
                  {#if narrow.current}
                    <span class="hint sub">{#if gone}{@render trail(e)} · {gone}{:else}{e.dir ? '' : `${size(e.size)} · `}{ago(e.mtime)}{#if loc}{' · '}{@render trail(e)}{/if}{/if}</span>
                  {:else if loc}
                    <span class="hint sub">{@render trail(e)}{gone ? ` · ${gone}` : ''}</span>
                  {/if}
                </span>
                <span class="cell" role="gridcell">{@render more(e)}</span>
                <span class="num size" role="gridcell">{e.dir || gone ? '' : size(e.size)}</span>
                <span class="num mtime" role="gridcell" title={gone ? undefined : date(e.mtime)}>{gone ? '' : ago(e.mtime)}</span>
              </div>
            {/if}
          {/each}
        </div>
        {@render footer?.()}
      </div>
    {/snippet}
  </ContextMenu.Trigger>
  <ContextMenu.Portal>
    <ContextMenu.Content class="menu" preventScroll={false}>{@render items(ctx)}</ContextMenu.Content>
  </ContextMenu.Portal>
</ContextMenu.Root>
