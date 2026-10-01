<script lang="ts">
  import { onMount } from 'svelte'
  import Minus from '@lucide/svelte/icons/minus'
  import Plus from '@lucide/svelte/icons/plus'
  import Search from '@lucide/svelte/icons/search'
  import ListTree from '@lucide/svelte/icons/list-tree'
  import ChevronUp from '@lucide/svelte/icons/chevron-up'
  import ChevronDown from '@lucide/svelte/icons/chevron-down'
  import X from '@lucide/svelte/icons/x'
  import { load, save } from '../lib/storage'
  import { t } from '../lib/i18n'
  import type { EventBus, PDFViewer, PDFLinkService } from 'pdfjs-dist/web/pdf_viewer.mjs'

  let { src, onready, onfail }: { src: string; onready: () => void; onfail: () => void } = $props()

  type Mark = { title: string; dest: unknown; depth: number }
  let host = $state<HTMLDivElement>()
  let page = $state(1)
  let total = $state(0)
  let outline = $state<Mark[]>([])
  let panel = $state<'' | 'outline' | 'find'>('')
  let query = $state('')
  let found = $state<{ current: number; total: number } | null>(null)
  let jump = $state('')
  let bus: EventBus | undefined
  let view: PDFViewer | undefined
  let links: PDFLinkService | undefined
  const spot = $derived('pdf:' + src)

  onMount(() => {
    let gone = false
    let stop = () => {}
    let doc: { destroy: () => Promise<void> } | undefined
    ;(async () => {
      const pdfjs = await import('pdfjs-dist')
      // pdf_viewer.mjs reads the core library from this global when it is evaluated.
      Object.assign(globalThis, { pdfjsLib: pdfjs })
      const [viewer, worker] = await Promise.all([import('pdfjs-dist/web/pdf_viewer.mjs'), import('pdfjs-dist/build/pdf.worker.min.mjs?url'), import('pdfjs-dist/web/pdf_viewer.css')])
      pdfjs.GlobalWorkerOptions.workerSrc = worker.default
      if (gone || !host) return
      bus = new viewer.EventBus()
      links = new viewer.PDFLinkService({ eventBus: bus, externalLinkTarget: viewer.LinkTarget.BLANK })
      const finder = new viewer.PDFFindController({ eventBus: bus, linkService: links })
      view = new viewer.PDFViewer({ container: host, eventBus: bus, linkService: links, findController: finder, textLayerMode: 1, removePageBorders: true })
      links.setViewer(view)
      let width = host.clientWidth
      const fit = new ResizeObserver(() => {
        if (!view?.pagesCount || host!.clientWidth === width) return
        width = host!.clientWidth
        if (/^(auto|page-width)$/.test(view.currentScaleValue)) view.currentScaleValue = innerWidth < 768 ? 'page-width' : 'auto'
      })
      fit.observe(host)
      stop = () => fit.disconnect()
      bus.on('pagesinit', () => {
        view!.currentScaleValue = innerWidth < 768 ? 'page-width' : 'auto'
        const at = Number(load(spot))
        if (at > 1 && at <= view!.pagesCount) view!.currentPageNumber = at
        onready()
      })
      bus.on('pagechanging', (e: { pageNumber: number }) => {
        page = e.pageNumber
        save(spot, String(page))
      })
      bus.on('updatefindmatchescount', (e: { matchesCount: { current: number; total: number } }) => e.matchesCount.total && (found = e.matchesCount))
      bus.on('updatefindcontrolstate', (e: { state: number; matchesCount: { current: number; total: number } }) => {
        found = e.state === viewer.FindState.PENDING ? null : e.matchesCount
      })
      const d = await pdfjs.getDocument({ url: src, cMapUrl: PDFJS_DATA + 'cmaps/', cMapPacked: true, standardFontDataUrl: PDFJS_DATA + 'standard_fonts/', isEvalSupported: false, rangeChunkSize: 1 << 19 }).promise
      if (gone) return void d.destroy()
      doc = d
      total = d.numPages
      view.setDocument(d)
      links.setDocument(d)
      const flat = (items: { title: string; dest: unknown; items?: unknown[] }[] | null, depth = 0): Mark[] =>
        (items ?? []).flatMap((x) => [{ title: x.title, dest: x.dest, depth }, ...flat(x.items as never, depth + 1)])
      outline = flat(await d.getOutline())
    })().catch(() => !gone && onfail())
    return () => {
      gone = true
      stop()
      view?.setDocument(null as never)
      doc?.destroy()
    }
  })

  function wheel(e: WheelEvent) {
    if (!view || (!e.ctrlKey && !e.metaKey)) return
    e.preventDefault()
    view.updateScale({ drawingDelay: 300, scaleFactor: Math.exp(-e.deltaY / 300), origin: [e.clientX, e.clientY] })
  }

  let pinch = 0
  let pending = 1
  const spread = (e: TouchEvent) => Math.hypot(e.touches[0].clientX - e.touches[1].clientX, e.touches[0].clientY - e.touches[1].clientY)

  function touchstart(e: TouchEvent) {
    pinch = e.touches.length === 2 ? spread(e) : 0
    pending = 1
  }

  function touchmove(e: TouchEvent) {
    if (!view || !pinch || e.touches.length !== 2) return
    const d = spread(e)
    pending *= d / pinch
    pinch = d
    if (Math.abs(pending - 1) < 0.02) return
    const [a, b] = e.touches
    view.updateScale({ drawingDelay: 300, scaleFactor: pending, origin: [(a.clientX + b.clientX) / 2, (a.clientY + b.clientY) / 2] })
    pending = 1
  }

  const zoom = (d: number) => view && (d > 0 ? view.increaseScale() : view.decreaseScale())

  function find(again = false, back = false) {
    bus?.dispatch('find', { source: null, type: again ? 'again' : '', query, caseSensitive: false, entireWord: false, highlightAll: true, findPrevious: back, matchDiacritics: false })
  }

  function go(e: SubmitEvent) {
    e.preventDefault()
    const n = Number(jump)
    if (view && n >= 1 && n <= total) view.currentPageNumber = n
    jump = ''
    ;(document.activeElement as HTMLElement | null)?.blur()
  }

  function key(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
      e.preventDefault()
      panel = 'find'
    }
  }
</script>

<svelte:window onkeydown={key} />

<div class="pdf" role="document" bind:this={host} onwheel={wheel} ontouchstart={touchstart} ontouchmove={touchmove} ontouchend={touchstart}>
  <div class="pdfViewer"></div>
</div>
{#if panel === 'outline'}
  <nav class="pdf-outline" aria-label={t.contents}>
    <ol>
      {#each outline as m, i (i)}
        <li style:padding-inline-start={`calc(${m.depth} * var(--space-4))`}>
          <button
            onclick={() => {
              panel = ''
              links?.goToDestination(m.dest as never)
            }}>{m.title}</button
          >
        </li>
      {/each}
    </ol>
  </nav>
{/if}
{#if total}
  <div class="pdf-bar">
    {#if panel === 'find'}
      <form
        class="pill find"
        onsubmit={(e) => {
          e.preventDefault()
          find(true)
        }}
      >
        <!-- svelte-ignore a11y_autofocus -->
        <input type="search" bind:value={query} oninput={() => find()} placeholder={t.findInDocument} aria-label={t.findInDocument} autofocus />
        <span class="count">{found && query ? t.matchOf(found.current, found.total) : ''}</span>
        <button type="button" class="icon-btn" onclick={() => find(true, true)} aria-label={t.prev}><ChevronUp size={18} /></button>
        <button type="submit" class="icon-btn" aria-label={t.next}><ChevronDown size={18} /></button>
        <button
          type="button"
          class="icon-btn"
          onclick={() => {
            panel = ''
            query = ''
            find()
          }}
          aria-label={t.close}><X size={18} /></button
        >
      </form>
    {:else}
      <span class="pill">
        {#if outline.length}<button class="icon-btn" onclick={() => (panel = panel === 'outline' ? '' : 'outline')} aria-label={t.contents} aria-expanded={panel === 'outline'}><ListTree size={18} /></button>{/if}
        <button class="icon-btn" onclick={() => (panel = 'find')} aria-label={t.findInDocument}><Search size={18} /></button>
      </span>
      <form class="pill jump" onsubmit={go}>
        <input inputmode="numeric" bind:value={jump} placeholder={String(page)} aria-label={t.goToPage} size="3" />
        <span>/ {total}</span>
      </form>
      <span class="pill">
        <button class="icon-btn" onclick={() => zoom(-1)} aria-label={t.zoomOut}><Minus size={18} /></button>
        <button class="icon-btn" onclick={() => zoom(1)} aria-label={t.zoomIn}><Plus size={18} /></button>
      </span>
    {/if}
  </div>
{/if}

<style>
  .pdf {
    position: absolute;
    inset: 0;
    overflow: auto;
    background: var(--viewer-bg);
    overscroll-behavior: contain;
    touch-action: pan-x pan-y;
  }
  .pdf :global(.pdfViewer) {
    padding: var(--space-3) 0 calc(var(--hit) * 2);
  }
  .pdf :global(.pdfViewer .page) {
    margin: 0 auto var(--space-3);
    box-shadow: var(--shadow-sm);
  }
  .pdf-bar {
    position: absolute;
    left: 0;
    right: 0;
    bottom: calc(env(safe-area-inset-bottom) + var(--space-3));
    display: flex;
    justify-content: center;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-3);
    pointer-events: none;
  }
  .pill {
    pointer-events: auto;
    display: flex;
    align-items: center;
    min-height: var(--hit);
    padding: 0 var(--space-1);
    border-radius: var(--radius-full);
    background: color-mix(in srgb, var(--viewer-bg) 82%, transparent);
    color: var(--viewer-fg);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    font-size: var(--text-sm);
    font-variant-numeric: tabular-nums;
  }
  .pill :global(.icon-btn) {
    color: var(--viewer-fg);
  }
  .pill input {
    background: none;
    border: 0;
    color: inherit;
    padding: var(--space-1);
    text-align: end;
    font-variant-numeric: tabular-nums;
  }
  .jump {
    padding: 0 var(--space-3) 0 var(--space-1);
  }
  .find {
    flex: 1;
    max-width: 520px;
    padding-inline-start: var(--space-3);
  }
  .find input {
    flex: 1;
    min-width: 0;
    text-align: start;
  }
  .count {
    color: var(--viewer-icon);
    white-space: nowrap;
    padding: 0 var(--space-1);
  }
  .pdf-outline {
    position: absolute;
    inset: 0 0 calc(var(--hit) + var(--space-6)) 0;
    overflow: auto;
    background: var(--panel);
    color: var(--fg);
    animation: appear var(--dur-2) var(--ease-out);
  }
  .pdf-outline ol {
    list-style: none;
    margin: 0 auto;
    padding: var(--space-4);
    max-width: 680px;
  }
  .pdf-outline button {
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
</style>
