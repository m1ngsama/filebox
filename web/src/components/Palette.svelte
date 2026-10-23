<script lang="ts">
  import { icon } from '../lib/icon'
  import { linger } from '../lib/motion'
  import { Dialog } from 'bits-ui'
  import Search from '@lucide/svelte/icons/search'
  import Clock from '@lucide/svelte/icons/clock'
  import Star from '@lucide/svelte/icons/star'
  import ActivityIcon from '@lucide/svelte/icons/activity'
  import TagIcon from '@lucide/svelte/icons/tag'
  import Images from '@lucide/svelte/icons/images'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import Share2 from '@lucide/svelte/icons/share-2'
  import Trash from '@lucide/svelte/icons/trash'
  import ChartPie from '@lucide/svelte/icons/chart-pie'
  import Copy from '@lucide/svelte/icons/copy'
  import Settings from '@lucide/svelte/icons/settings'
  import Sun from '@lucide/svelte/icons/sun'
  import File from '@lucide/svelte/icons/file'
  import Folder from '@lucide/svelte/icons/folder'
  import { api, filesURL, selectURL, type RecentFile } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { setTheme, type Theme } from '../lib/theme'
  import { t } from '../lib/i18n'

  let { vols, onclose }: { vols: string[]; onclose: () => void } = $props()

  type Item = { id: string; label: string; hint?: string; group: string; icon: typeof File; run: () => void }
  let query = $state('')
  let active = $state(0)
  let found = $state.raw<RecentFile[]>([])
  let list = $state<HTMLElement>()

  const go = (url: string) => () => navigate(url)
  const commands: Item[] = [
    { id: 'recent', label: t.recent, icon: Clock, run: go('/recent') },
    { id: 'favorites', label: t.favorites, icon: Star, run: go('/favorites') },
    { id: 'photos', label: t.photos, icon: Images, run: go('/photos') },
    { id: 'tags', label: t.tags, icon: TagIcon, run: go('/tags') },
    { id: 'activity', label: t.activity, icon: ActivityIcon, run: go('/activity') },
    { id: 'storage', label: t.storage, icon: ChartPie, run: go('/storage') },
    { id: 'duplicates', label: t.duplicates, icon: Copy, run: go('/duplicates') },
    { id: 'shares', label: t.myShares, icon: Share2, run: go('/shares') },
    { id: 'trash', label: t.trash, icon: Trash, run: go('/trash') },
    { id: 'settings', label: t.settings, icon: Settings, run: go('/settings') },
  ].map((c) => ({ ...c, group: t.paletteGo }))
  const places = $derived<Item[]>(vols.map((v) => ({ id: `vol:${v}`, label: v, group: t.paletteGo, icon: HardDrive, run: go(filesURL(v, '')) })))
  const themes: Item[] = (['system', 'light', 'dark'] as Theme[]).map((v) => ({ id: `theme:${v}`, label: t.paletteTheme(t.themes[v]), group: t.settings, icon: Sun, run: () => setTheme(v) }))

  const q = $derived(query.trim().toLowerCase())
  const matches = $derived([...commands, ...places, ...themes].filter((c) => !q || c.label.toLowerCase().includes(q)))
  const files = $derived<Item[]>(
    found.map((f) => ({ id: `${f.vol}:${f.path}`, label: f.name, hint: `${f.vol}/${f.path.slice(0, -f.name.length).replace(/\/$/, '')}`, group: t.paletteFiles, icon: f.dir ? Folder : File, run: go(selectURL(f.vol, f.path)) })),
  )
  const items = $derived([...matches, ...files])

  $effect(() => {
    q
    active = 0
  })

  $effect(() => {
    const text = query.trim()
    if ([...text].length < 2) return void (found = [])
    const stop = new AbortController()
    const id = setTimeout(() => api.search(text, stop.signal).then((r) => !stop.signal.aborted && (found = r.entries.slice(0, 20)), () => {}), 180)
    return () => {
      clearTimeout(id)
      stop.abort()
    }
  })

  $effect(() => {
    active
    list?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' })
  })

  function choose(i: Item | undefined) {
    if (!i) return
    onclose()
    i.run()
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (items.length) active = (active + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
    } else if (e.key === 'Enter' && !e.isComposing) {
      e.preventDefault()
      choose(items[active])
    }
  }
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" {@attach linger} />
    <Dialog.Content class="dialog palette" preventScroll={false} {@attach linger}>
      <Dialog.Title class="sr-only">{t.palette}</Dialog.Title>
      <div class="palette-input">
        <Search size={icon.md} aria-hidden="true" />
        <!-- svelte-ignore a11y_autofocus -->
        <input
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-activedescendant={items[active] ? `pal-${active}` : undefined}
          aria-label={t.palette}
          placeholder={t.palette}
          autocomplete="off"
          spellcheck="false"
          autofocus
          bind:value={query}
          onkeydown={key}
        />
      </div>
      <div class="palette-list" id="palette-list" role="listbox" bind:this={list}>
        {#each items as item, i (item.id)}
          {#if !i || items[i - 1].group !== item.group}<div class="menu-label" role="presentation">{item.group}</div>{/if}
          <div id={`pal-${i}`} class="menu-item" role="option" aria-selected={i === active} data-highlighted={i === active ? '' : undefined} tabindex="-1" onpointermove={() => (active = i)} onclick={() => choose(item)} onkeydown={null}>
            <item.icon size={icon.sm} />
            <span class="palette-label">{item.label}</span>
            {#if item.hint}<span class="hint palette-hint">{item.hint}</span>{/if}
          </div>
        {:else}
          <p class="hint palette-empty">{t.paletteEmpty}</p>
        {/each}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
