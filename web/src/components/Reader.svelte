<script lang="ts">
  import { onMount } from 'svelte'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import ListTree from '@lucide/svelte/icons/list-tree'
  import Minus from '@lucide/svelte/icons/minus'
  import Plus from '@lucide/svelte/icons/plus'
  import { EPUB, type TocItem } from '../vendor/foliate/epub.js'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'

  let { list, entry, spot, onready, onfail, onclose }: { list: string; entry: string; spot: string; onready: () => void; onfail: () => void; onclose: () => void } =
    $props()

  type View = HTMLElement & {
    open: (b: unknown) => void
    goTo: (to: { index: number; anchor?: number | ((doc: Document) => unknown) }) => Promise<void>
    next: () => Promise<void>
    prev: () => Promise<void>
    setStyles?: (css: string) => void
    destroy?: () => void
  }
  let host = $state<HTMLDivElement>()
  let view: View | undefined
  let rtl = $state(false)
  let read = $state<number | null>(null)
  let toc = $state<{ label: string; href: string; depth: number }[]>([])
  let contents = $state(false)
  let go: (href: string) => void = () => {}
  const scales = [0.85, 1, 1.15, 1.3, 1.5]
  let scale = $state(Math.max(0, scales.indexOf(Number(load('reader-scale') ?? 1))))
  const flat = (items: TocItem[] | null | undefined, depth = 0): { label: string; href: string; depth: number }[] =>
    (items ?? []).flatMap((x) => [{ label: x.label.trim(), href: x.href, depth }, ...flat(x.subitems, depth + 1)])

  function zoom(d: number) {
    scale = Math.min(scales.length - 1, Math.max(0, scale + d))
    save('reader-scale', String(scales[scale]))
    view?.setStyles?.(styles())
  }

  const turn = (d: number) => (d > 0 ? view?.next() : view?.prev())

  function key(e: KeyboardEvent) {
    if (e.metaKey || e.ctrlKey || e.altKey) return
    if (e.key === 'Escape') return onclose()
    const d = { ArrowRight: rtl ? -1 : 1, ArrowLeft: rtl ? 1 : -1, PageDown: 1, PageUp: -1, ' ': e.shiftKey ? -1 : 1 }[e.key]
    if (!d) return
    e.preventDefault()
    turn(d)
  }

  function styles() {
    const s = getComputedStyle(document.documentElement)
    const v = (n: string) => s.getPropertyValue(n).trim()
    return `html { color-scheme: ${s.colorScheme}; } body { zoom: ${scales[scale]}; color: ${v('--fg')}; background: ${v('--panel')}; line-height: 1.7; }
      a:any-link { color: ${v('--accent')}; } img, svg { max-width: 100%; height: auto; }`
  }

  onMount(() => {
    let gone = false
    ;(async () => {
      const r = await fetch(list + '&all')
      if (!r.ok) throw new Error(String(r.status))
      const { entries }: { entries: { name: string; size: number }[] } = await r.json()
      const sizes = new Map(entries.map((e) => [e.name, e.size]))
      const get = async (name: string) => {
        if (!sizes.has(name)) return null
        const res = await fetch(`${entry}&e=${encodeURIComponent(name)}`)
        if (!res.ok) throw new Error(String(res.status))
        return res
      }
      const book = await new EPUB({
        loadText: async (n) => (await get(n))?.text() ?? null,
        loadBlob: async (n) => (await get(n))?.blob() ?? null,
        getSize: (n) => sizes.get(n) ?? 0,
      }).init()
      book.transformTarget?.addEventListener('load', (e) => {
        const d = (e as CustomEvent<{ isScript: boolean; allow: boolean }>).detail
        if (d.isScript) d.allow = false
      })
      const fixed = book.rendition?.layout === 'pre-paginated'
      await (fixed ? import('../vendor/foliate/fixed-layout.js') : import('../vendor/foliate/paginator.js'))
      if (gone || !host) return
      rtl = book.dir === 'rtl'
      const v = document.createElement(fixed ? 'foliate-fxl' : 'foliate-paginator') as View
      v.setAttribute('margin', '32px')
      v.setAttribute('gap', '6%')
      v.setAttribute('max-inline-size', '680px')
      host.append(v)
      view = v
      v.open(book)
      toc = flat(book.toc)
      go = (href) => {
        const at = book.resolveHref(href)
        if (at) v.goTo(at)
      }
      v.addEventListener('load', (e) => {
        const { doc, index } = (e as CustomEvent<{ doc: Document; index: number }>).detail
        doc.addEventListener('keydown', (k) => window.dispatchEvent(new KeyboardEvent('keydown', k)))
        doc.addEventListener('click', (c) => {
          const a = (c.target as Element).closest?.('a[href]')
          const href = a?.getAttribute('href')
          if (!href || book.isExternal(href)) return
          c.preventDefault()
          go(book.sections[index].resolveHref(href))
        })
        v.setStyles?.(styles())
      })
      const weights = book.sections.map((s) => (s.linear === 'no' ? 0 : (s.size ?? 0)))
      const total = weights.reduce((a: number, b: number) => a + b, 0)
      v.addEventListener('relocate', (e) => {
        const { index, fraction } = (e as CustomEvent<{ index: number; fraction: number }>).detail
        save(spot, JSON.stringify([index, fraction ?? 0]))
        if (total) read = (weights.slice(0, index).reduce((a: number, b: number) => a + b, 0) + (fraction ?? 0) * weights[index]) / total
      })
      let at: unknown = null
      try {
        at = JSON.parse(load(spot) ?? 'null')
      } catch {}
      if (Array.isArray(at) && Number.isInteger(at[0]) && at[0] < book.sections.length) await v.goTo({ index: at[0], anchor: Number(at[1]) || 0 })
      else await v.next()
      if (!gone) onready()
    })().catch(() => !gone && onfail())
    return () => {
      gone = true
      view?.destroy?.()
    }
  })
</script>

<svelte:window onkeydown={key} />

<div class="reader" bind:this={host}>
  <button class="icon-btn turn prev" onclick={() => turn(rtl ? 1 : -1)} aria-label={rtl ? t.nextPage : t.prevPage}><ChevronLeft size={24} /></button>
  <button class="icon-btn turn next" onclick={() => turn(rtl ? -1 : 1)} aria-label={rtl ? t.prevPage : t.nextPage}><ChevronRight size={24} /></button>
  <button class="zone prev" tabindex="-1" aria-hidden="true" onclick={() => turn(rtl ? 1 : -1)}></button>
  <button class="zone next" tabindex="-1" aria-hidden="true" onclick={() => turn(rtl ? -1 : 1)}></button>
  <div class="foot">
    <button class="icon-btn" onclick={() => (contents = !contents)} disabled={!toc.length} aria-label={t.contents} aria-expanded={contents}><ListTree size={20} /></button>
    {#if read !== null}<span class="read" role="status">{Math.round(read * 100)}%</span>{/if}
    <span class="zoom">
      <button class="icon-btn" onclick={() => zoom(-1)} disabled={scale === 0} aria-label={t.smallerText}><Minus size={18} /></button>
      <button class="icon-btn" onclick={() => zoom(1)} disabled={scale === scales.length - 1} aria-label={t.largerText}><Plus size={18} /></button>
    </span>
  </div>
  {#if contents}
    <nav class="toc" aria-label={t.contents}>
      <ol>
        {#each toc as c, i (i)}
          <li style:padding-inline-start={`calc(${c.depth} * var(--space-4))`}>
            <button
              onclick={() => {
                contents = false
                go(c.href)
              }}>{c.label}</button
            >
          </li>
        {/each}
      </ol>
    </nav>
  {/if}
</div>

<style>
  .reader {
    position: fixed;
    inset: 0;
    background: var(--panel);
  }
  .reader :global(foliate-paginator),
  .reader :global(foliate-fxl) {
    position: absolute;
    inset: 0 0 calc(var(--hit) + env(safe-area-inset-bottom)) 0;
  }
  .turn {
    position: absolute;
    z-index: 1;
    top: 50%;
    translate: 0 -50%;
    color: var(--muted);
  }
  .prev {
    left: 0;
  }
  .next {
    right: 0;
  }
  .zone {
    display: none;
    position: absolute;
    z-index: 1;
    top: 0;
    bottom: 0;
    width: 22%;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: none;
  }
  .foot {
    position: absolute;
    z-index: 2;
    left: 0;
    right: 0;
    bottom: 0;
    height: calc(var(--hit) + env(safe-area-inset-bottom));
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--space-2) env(safe-area-inset-bottom);
    color: var(--muted);
  }
  .read {
    font-size: var(--text-xs);
    font-variant-numeric: tabular-nums;
  }
  .zoom {
    display: flex;
  }
  .toc {
    position: absolute;
    z-index: 3;
    inset: 0 0 var(--hit) 0;
    overflow: auto;
    background: var(--panel);
    border-bottom: 1px solid var(--line);
    animation: appear var(--dur-2) var(--ease-out);
  }
  .toc ol {
    list-style: none;
    margin: 0 auto;
    padding: var(--space-4);
    max-width: 680px;
  }
  .toc button {
    width: 100%;
    justify-content: flex-start;
    min-height: var(--hit);
    border: 0;
    border-bottom: 1px solid var(--line);
    border-radius: 0;
    background: none;
    text-align: start;
    white-space: normal;
  }
  @media (pointer: coarse) {
    .turn {
      display: none;
    }
    .zone {
      display: block;
    }
  }
</style>
