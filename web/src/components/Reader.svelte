<script lang="ts">
  import { onMount } from 'svelte'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import { EPUB } from '../vendor/foliate/epub.js'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'

  let { list, entry, spot, onready, onfail }: { list: string; entry: string; spot: string; onready: () => void; onfail: () => void } = $props()

  type View = HTMLElement & {
    open: (b: unknown) => void
    goTo: (to: { index: number; anchor?: number }) => Promise<void>
    next: () => Promise<void>
    prev: () => Promise<void>
    setStyles?: (css: string) => void
    destroy?: () => void
  }
  let host = $state<HTMLDivElement>()
  let view: View | undefined
  let rtl = $state(false)

  const turn = (d: number) => (d > 0 ? view?.next() : view?.prev())

  function key(e: KeyboardEvent) {
    if (e.metaKey || e.ctrlKey || e.altKey) return
    const d = { ArrowRight: rtl ? -1 : 1, ArrowLeft: rtl ? 1 : -1, PageDown: 1, PageUp: -1, ' ': e.shiftKey ? -1 : 1 }[e.key]
    if (!d) return
    e.preventDefault()
    turn(d)
  }

  function styles() {
    const s = getComputedStyle(document.documentElement)
    const v = (n: string) => s.getPropertyValue(n).trim()
    return `html { color-scheme: ${s.colorScheme}; } body { color: ${v('--fg')}; background: ${v('--panel')}; line-height: 1.7; }
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
      v.addEventListener('load', (e) => {
        const doc = (e as CustomEvent<{ doc: Document }>).detail.doc
        doc.addEventListener('keydown', (k) => window.dispatchEvent(new KeyboardEvent('keydown', k)))
        v.setStyles?.(styles())
      })
      v.addEventListener('relocate', (e) => {
        const { index, fraction } = (e as CustomEvent<{ index: number; fraction: number }>).detail
        save(spot, JSON.stringify([index, fraction ?? 0]))
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
</div>

<style>
  .reader {
    position: relative;
    width: 100%;
    height: 100%;
    background: var(--panel);
  }
  .reader :global(foliate-paginator),
  .reader :global(foliate-fxl) {
    position: absolute;
    inset: 0;
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
</style>
