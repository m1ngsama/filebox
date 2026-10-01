<script lang="ts">
  import { onMount } from 'svelte'
  import PhotoSwipe from 'photoswipe'
  import 'photoswipe/style.css'
  import X from '@lucide/svelte/icons/x'
  import type { Entry, Src } from '../lib/api'
  import { size, date, when, thumbable, converted, metaRows, type Meta } from '../lib/format'
  import { t } from '../lib/i18n'

  type Book = { title: string; rtl: boolean; onpage: (i: number) => void; onrtl: () => void }
  let {
    entry = $bindable(),
    images,
    url,
    onclose,
    onedge,
    book,
  }: { entry: Entry; images: Entry[]; url: Src; onclose: () => void; onedge?: (d: 1 | -1) => void; book?: Book } = $props()

  let info = $state(false)
  let meta = $state<Meta | null>(null)
  let host = $state<HTMLElement>()
  let box: PhotoSwipe | undefined
  const side = () => info && innerWidth > 640

  $effect(() => {
    info
    box?.updateSize(true)
  })
  const portal = (el: HTMLElement) => host?.append(el)
  const stop = (e: Event) => e.stopPropagation()

  const icon = (d: string) =>
    `<svg class="pswp__icn fb-icn" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${d}</svg>`

  const rows = $derived.by(() => {
    const m = meta
    const r: [string, string][] = [[t.size, size(entry.size)]]
    if (entry.mtime) r.push([t.mtime, date(entry.mtime)])
    if (!m) return r
    return [...r, ...metaRows(m)]
  })

  const metas = new Map<string, Promise<Meta>>()
  const metaOf = (e: Entry) => {
    let m = metas.get(e.name)
    if (!m) {
      m = fetch(url(e, 'meta')).then((r) => (r.ok ? r.json() : {}), () => ({}))
      metas.set(e.name, m)
    }
    return m
  }
  const taken = (m: Meta) => {
    const at = m.taken ? new Date(m.taken.replace(' ', 'T')).getTime() : NaN
    return Number.isNaN(at) ? '' : when(at)
  }

  $effect(() => {
    if (!info) return
    let stale = false
    meta = null
    metaOf(entry).then((m) => !stale && (meta = m))
    return () => {
      stale = true
    }
  })

  onMount(() => {
    const n = images.length
    const pos = (i: number) => (book?.rtl ? n - 1 - i : i)
    const item = (i: number) => images[pos(i)]
    const at = images.indexOf(entry)
    const tile = (e: Entry) => (book ? null : document.querySelector<HTMLImageElement>(`[data-name="${CSS.escape(e.name)}"] img.ok`))
    const data = images.map((_, i) => item(i)).map((e) => {
      const th = tile(e)
      const k = th?.naturalWidth ? 4096 / Math.max(th.naturalWidth, th.naturalHeight) : 0
      return { src: url(e, !book && converted(e.name) ? 'large' : undefined), alt: e.name, msrc: !book && thumbable(e.name) ? url(e, 'thumb') : undefined, width: Math.round((th?.naturalWidth ?? 0) * k), height: Math.round((th?.naturalHeight ?? 0) * k) }
    })
    let done = false
    const pswp = new PhotoSwipe({
      dataSource: data,
      index: pos(Math.max(0, at)),
      bgOpacity: 1,
      wheelToZoom: true,
      loop: n > 1 && !onedge && !book,
      counter: !book,
      showHideAnimationType: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'none' : 'zoom',
      preloaderDelay: 150,
      indexIndicatorSep: ' / ',
      closeTitle: t.close,
      zoomTitle: t.zoom,
      arrowPrevTitle: t.prev,
      arrowNextTitle: t.next,
      errorMsg: t.previewFailed,
      paddingFn: () => ({ top: 0, bottom: 0, left: 0, right: side() ? 340 : 0 }),
    })
    box = pswp
    pswp.addFilter('thumbEl', (el, _, i) => (tile(item(i)) ?? el) as HTMLElement)

    pswp.on('contentLoad', (e) => {
      const c = e.content
      if (c.type !== 'image' || c.width) return
      e.preventDefault()
      c.state = 'loading'
      const img = new Image()
      img.onload = img.onerror = () => {
        if (done) return
        const d = data[c.index]
        d.width = img.naturalWidth || 1
        d.height = img.naturalHeight || 1
        pswp.refreshSlideContent(c.index)
      }
      img.src = String(c.data.src)
    })

    pswp.addFilter('contentErrorElement', (el, c) => {
      el.setAttribute('role', 'alert')
      const a = document.createElement('a')
      a.className = 'button primary'
      a.href = url(item(c.index), 'dl')
      a.download = ''
      a.textContent = t.download
      el.append(document.createElement('br'), a)
      return el
    })

    let bottom: HTMLElement | undefined
    pswp.on('uiRegister', () => {
      pswp.ui?.registerElement({ name: 'bottom', appendTo: 'wrapper', className: 'fb-bottom pswp__hide-on-close', onInit: (el) => (bottom = el) })
      pswp.ui?.registerElement({
        name: 'name',
        order: 6,
        appendTo: 'bar',
        className: 'fb-name',
        onInit: (el) =>
          pswp.on('change', () => {
            if (book) return void (el.textContent = book.title)
            const e = item(pswp.currIndex)
            el.textContent = e.name
            el.title = e.name
            if (thumbable(e.name)) metaOf(e).then((m) => item(pswp.currIndex) === e && taken(m) && (el.textContent = taken(m)))
          }),
      })
      if (book)
        pswp.ui?.registerElement({
          name: 'page',
          order: 7,
          appendTo: 'bar',
          className: 'fb-page',
          onInit: (el) => pswp.on('change', () => (el.textContent = t.page(pos(pswp.currIndex) + 1, n))),
        })
      pswp.ui?.registerElement({
        name: 'download',
        order: 8,
        isButton: true,
        tagName: 'a',
        title: t.download,
        html: icon('<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/><path d="M12 15V3"/>'),
        onInit: (el) => {
          el.setAttribute('download', '')
          pswp.on('change', () => el.setAttribute('href', url(item(pswp.currIndex), 'dl')))
        },
      })
      if (book)
        pswp.ui?.registerElement({
          name: 'rtl',
          order: 9,
          isButton: true,
          title: t.rtl,
          html: icon('<path d="M8 3 4 7l4 4"/><path d="M4 7h16"/><path d="m16 21 4-4-4-4"/><path d="M20 17H4"/>'),
          onInit: (el) => el.setAttribute('aria-pressed', String(book.rtl)),
          onClick: () => book.onrtl(),
        })
      else pswp.ui?.registerElement({
        name: 'info',
        order: 9,
        isButton: true,
        title: t.info,
        html: icon('<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>'),
        onClick: () => (info = !info),
      })
    })

    pswp.on('change', () => {
      entry = item(pswp.currIndex)
      book?.onpage(pos(pswp.currIndex))
      pswp.element?.setAttribute('aria-label', book ? book.title : entry.name)
      if (onedge) queueMicrotask(() => pswp.element?.querySelectorAll<HTMLButtonElement>('.pswp__button--arrow').forEach((b) => (b.disabled = false)))
    })
    if (onedge) {
      const edge = (d: 1 | -1) => (d > 0 ? pswp.currIndex === n - 1 : pswp.currIndex === 0)
      const towards = (d: 1 | -1) => ((book?.rtl ? -d : d) as 1 | -1)
      for (const d of [1, -1] as const) {
        const go = d > 0 ? pswp.next.bind(pswp) : pswp.prev.bind(pswp)
        pswp[d > 0 ? 'next' : 'prev'] = () => (edge(d) ? onedge(towards(d)) : go())
      }
      let from: { x: number; i: number } | null = null
      pswp.on('pointerDown', (e) => (from = { x: e.originalEvent.clientX, i: pswp.currIndex }))
      pswp.on('pointerUp', (e) => {
        const dx = from && from.i === pswp.currIndex && (pswp.currSlide?.currZoomLevel ?? 1) <= (pswp.currSlide?.zoomLevels.fit ?? 1) ? e.originalEvent.clientX - from.x : 0
        from = null
        if (dx < -80 && edge(1)) onedge(towards(1))
        else if (dx > 80 && edge(-1)) onedge(towards(-1))
      })
    }
    pswp.on('keydown', (e) => {
      const k = e.originalEvent
      if (onedge && n === 1 && (k.key === 'ArrowRight' || k.key === 'ArrowLeft')) {
        e.preventDefault()
        k.preventDefault()
        onedge(book?.rtl === (k.key === 'ArrowRight') ? -1 : 1)
      } else if (k.key === 'i' && !book && !k.metaKey && !k.ctrlKey && !k.altKey) info = !info
      else if (k.key === 'Escape' && info) {
        e.preventDefault()
        info = false
      }
    })
    pswp.on('bindEvents', () => {
      host = pswp.element
      pswp.element?.setAttribute('aria-modal', 'true')
      if (onedge) pswp.element?.classList.add('fb-edges')
      pswp.element?.setAttribute('aria-label', book ? book.title : entry.name)
      pswp.element?.querySelector<HTMLElement>('.pswp__button--close')?.focus()
    })
    pswp.on('destroy', () => {
      if (done) return
      done = true
      onclose()
    })
    pswp.init()
    const narrow = matchMedia('(max-width: 640px)')
    const place = () => {
      const bar = pswp.topBar
      const close = bar?.querySelector('.pswp__button--close')
      for (const b of pswp.element?.querySelectorAll('.pswp__button--zoom, .pswp__button--download, .pswp__button--info, .pswp__button--rtl') ?? [])
        if (narrow.matches) bottom?.append(b)
        else bar?.insertBefore(b, close ?? null)
    }
    place()
    narrow.addEventListener('change', place)
    return () => {
      narrow.removeEventListener('change', place)
      if (done) return
      done = true
      pswp.destroy()
    }
  })
</script>

{#if info}
  <aside class="exif" aria-label={t.info} {@attach portal} onwheel={stop} onpointerdown={stop} ontouchstart={stop}>
    <header>
      <h2>{entry.name}</h2>
      <button class="icon-btn" onclick={() => (info = false)} aria-label={t.close}><X size={20} /></button>
    </header>
    <dl>
      {#each rows as [k, v] (k)}<dt>{k}</dt><dd>{v}</dd>{/each}
    </dl>
    {#if meta === null}<div class="loading" role="status" aria-label={t.loading}></div>{/if}
  </aside>
{/if}

<style>
  :global(.pswp) {
    --pswp-bg: var(--viewer-bg);
    --pswp-placeholder-bg: transparent;
    --pswp-icon-color: var(--viewer-icon);
  }
  :global(.pswp .pswp__button) {
    opacity: 1;
  }
  :global(.fb-bottom) {
    display: none;
  }
  :global(.pswp--one-slide.fb-edges .pswp__button--arrow) {
    display: block;
  }
  @media (pointer: coarse) {
    :global(.pswp .pswp__button--arrow),
    :global(.pswp .pswp__button--zoom) {
      display: none;
    }
  }
  :global(.pswp__top-bar) {
    padding-top: env(safe-area-inset-top);
    align-items: center;
  }
  :global(.fb-name) {
    order: -1;
    flex: 1;
    min-width: 0;
    padding: 0 12px 0 20px;
    color: var(--viewer-fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    pointer-events: auto;
  }
  :global(.pswp__counter),
  :global(.fb-page) {
    order: 0;
    margin: 0 8px;
    color: var(--viewer-fg);
  }
  :global(.pswp .pswp__button),
  :global(.pswp .pswp__button:hover:not(:disabled)) {
    border: 0;
    border-radius: 0;
    background: none;
  }
  :global(.pswp__button--download),
  :global(.pswp__button--info),
  :global(.pswp__button--rtl) {
    display: grid;
    place-items: center;
    color: var(--pswp-icon-color);
  }
  :global(.pswp__button--rtl[aria-pressed='true']) {
    color: var(--viewer-fg);
    background: var(--viewer-hover);
    border-radius: 50%;
  }
  :global(.pswp .fb-icn) {
    position: static;
    color: inherit;
    width: 22px;
    height: 22px;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
  }
  :global(.pswp__error-msg) {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: 0 var(--space-4);
    color: var(--viewer-fg);
  }
  :global(.pswp__error-msg br) {
    display: none;
  }
  .exif {
    position: fixed;
    z-index: 100001;
    top: calc(60px + env(safe-area-inset-top));
    right: 0;
    bottom: 0;
    width: min(340px, 100%);
    padding: var(--space-3) var(--space-5) var(--space-5);
    overflow: auto;
    background: var(--panel);
    color: var(--fg);
    box-shadow: var(--shadow);
  }
  .exif header {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }
  .exif h2 {
    flex: 1;
    overflow-wrap: anywhere;
  }
  .exif dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0;
  }
  .exif dt {
    color: var(--muted);
  }
  .exif dd {
    margin: 0;
    overflow-wrap: anywhere;
    user-select: text;
  }
  @media (max-width: 640px) {
    :global(.fb-bottom) {
      position: absolute;
      left: 0;
      right: 0;
      bottom: 0;
      display: flex;
      justify-content: space-around;
      padding-bottom: env(safe-area-inset-bottom);
      background: color-mix(in srgb, var(--viewer-bg) 70%, transparent);
    }
    :global(.pswp__counter) {
      margin: 0;
    }
    .exif {
      top: auto;
      width: 100%;
      max-height: 60%;
      border-radius: var(--radius-xl) var(--radius-xl) 0 0;
      padding-bottom: calc(var(--space-5) + env(safe-area-inset-bottom));
    }
  }
</style>
