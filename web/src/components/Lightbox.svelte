<script lang="ts">
  import { onMount } from 'svelte'
  import PhotoSwipe from 'photoswipe'
  import 'photoswipe/style.css'
  import X from '@lucide/svelte/icons/x'
  import type { Entry, Src } from '../lib/api'
  import { size, date, thumbable } from '../lib/format'
  import { t } from '../lib/i18n'

  let { entry = $bindable(), images, url, onclose }: { entry: Entry; images: Entry[]; url: Src; onclose: () => void } = $props()

  type Meta = Partial<Record<keyof typeof t.meta, string>> & { width?: number; height?: number; duration?: number }
  let info = $state(false)
  let meta = $state<Meta | null>(null)
  let host = $state<HTMLElement>()
  const portal = (el: HTMLElement) => host?.append(el)
  const stop = (e: Event) => e.stopPropagation()

  const icon = (d: string) =>
    `<svg class="pswp__icn fb-icn" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${d}</svg>`

  const rows = $derived.by(() => {
    const m = meta
    const r: [string, string][] = [[t.size, size(entry.size)]]
    if (entry.mtime) r.push([t.mtime, date(entry.mtime)])
    if (!m) return r
    if (m.width && m.height) r.push([t.meta.dimensions, `${m.width} × ${m.height}`])
    for (const k of ['taken', 'camera', 'lens', 'focal', 'aperture', 'shutter', 'iso', 'gps'] as const) if (m[k]) r.push([t.meta[k], m[k]])
    return r
  })

  $effect(() => {
    if (!info) return
    const e = entry
    let stale = false
    meta = null
    fetch(url(e, 'meta'))
      .then((r) => (r.ok ? r.json() : {}))
      .then((m: Meta) => !stale && (meta = m), () => !stale && (meta = {}))
    return () => {
      stale = true
    }
  })

  onMount(() => {
    const at = images.findIndex((e) => e.name === entry.name)
    const data = images.map((e) => ({ src: url(e), alt: e.name, msrc: thumbable(e.name) ? url(e, 'thumb') : undefined, width: 0, height: 0 }))
    let done = false
    const pswp = new PhotoSwipe({
      dataSource: data,
      index: Math.max(0, at),
      bgOpacity: 1,
      wheelToZoom: true,
      loop: images.length > 1,
      showHideAnimationType: 'fade',
      preloaderDelay: 150,
      indexIndicatorSep: ' / ',
      closeTitle: t.close,
      zoomTitle: t.zoom,
      arrowPrevTitle: t.prev,
      arrowNextTitle: t.next,
      errorMsg: t.previewFailed,
    })

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
      a.href = url(images[c.index], 'dl')
      a.download = ''
      a.textContent = t.download
      el.append(document.createElement('br'), a)
      return el
    })

    pswp.on('uiRegister', () => {
      pswp.ui?.registerElement({
        name: 'name',
        order: 6,
        appendTo: 'bar',
        className: 'fb-name',
        onInit: (el) => pswp.on('change', () => (el.textContent = images[pswp.currIndex].name)),
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
          pswp.on('change', () => el.setAttribute('href', url(images[pswp.currIndex], 'dl')))
        },
      })
      pswp.ui?.registerElement({
        name: 'info',
        order: 9,
        isButton: true,
        title: t.info,
        html: icon('<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>'),
        onClick: () => (info = !info),
      })
    })

    pswp.on('change', () => {
      entry = images[pswp.currIndex]
      pswp.element?.setAttribute('aria-label', entry.name)
    })
    pswp.on('keydown', (e) => {
      const k = e.originalEvent
      if (k.key === 'i' && !k.metaKey && !k.ctrlKey && !k.altKey) info = !info
      else if (k.key === 'Escape' && info) {
        e.preventDefault()
        info = false
      }
    })
    pswp.on('bindEvents', () => {
      host = pswp.element
      pswp.element?.setAttribute('aria-modal', 'true')
      pswp.element?.setAttribute('aria-label', entry.name)
      pswp.element?.querySelector<HTMLElement>('.pswp__button--close')?.focus()
    })
    pswp.on('destroy', () => {
      done = true
      onclose()
    })
    pswp.init()
    return () => {
      if (!done) pswp.destroy()
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
  :global(.pswp__counter) {
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
  :global(.pswp__button--info) {
    display: grid;
    place-items: center;
    color: var(--pswp-icon-color);
  }
  :global(.pswp .fb-icn) {
    position: static;
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
    top: 0;
    right: 0;
    bottom: 0;
    width: min(340px, 100%);
    padding: calc(12px + env(safe-area-inset-top)) var(--space-5) var(--space-5);
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
    .exif {
      top: auto;
      width: 100%;
      max-height: 60%;
      border-radius: var(--radius-xl) var(--radius-xl) 0 0;
      padding-bottom: calc(var(--space-5) + env(safe-area-inset-bottom));
    }
  }
</style>
