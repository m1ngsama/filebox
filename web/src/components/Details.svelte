<script lang="ts">
  import { Tabs } from 'bits-ui'
  import X from '@lucide/svelte/icons/x'
  import Star from '@lucide/svelte/icons/star'
  import FileIcon from './FileIcon.svelte'
  import SharePanel from './SharePanel.svelte'
  import VersionsPanel from './VersionsPanel.svelte'
  import { api, type Entry, type Version } from '../lib/api'
  import { size, date, fallback, parent, place } from '../lib/format'
  import { t } from '../lib/i18n'
  import { starred, star } from '../lib/favorites.svelte'
  import { fail } from '../lib/toast.svelte'

  let {
    vol,
    path,
    entry,
    thumbs,
    onclose,
    onchange = () => {},
  }: { vol: string; path: string; entry: Entry; thumbs: (string | null)[]; onclose: () => void; onchange?: () => void } = $props()
  let tries = $state(0)
  const src = $derived(fallback(thumbs, tries))
  const on = $derived(starred(vol, path))
  let total = $state<string | null>('')
  $effect(() => {
    if (!entry.dir) return
    api.size(vol, path).then(
      (s) => (total = s.scanning ? t.sizeIndexing : t.folderSize(size(s.size), s.files)),
      () => (total = null),
    )
  })
  let tab = $state('share')
  let versions = $state<Version[]>([])
  const loadVersions = () => api.versions(vol, path).then((r) => (versions = r.versions), () => {})
  $effect(() => {
    if (!entry.dir) loadVersions()
  })
</script>

<aside class="details" aria-label={t.details}>
  <header>
    <div class="details-thumb">
      {#if src}<img {src} alt="" onerror={() => tries++} />{:else}<FileIcon name={entry.name} dir={entry.dir} size={64} />{/if}
    </div>
    <button class="icon-btn details-close" aria-label={t.close} onclick={onclose}><X size={20} /></button>
    <div class="details-title">
      <h2 title={entry.name}>{entry.name}</h2>
      <button class="icon-btn" class:on aria-pressed={on} aria-label={t.star} title={on ? t.unstar : t.star} onclick={() => star(vol, [path], !on).catch(fail)}>
        <Star size={20} />
      </button>
    </div>
    <dl>
      {#if total !== null}<dt>{t.size}</dt><dd class="size">{entry.dir ? total || '…' : size(entry.size)}</dd>{/if}
      <dt>{t.mtime}</dt><dd>{date(entry.mtime)}</dd>
      <dt>{t.path}</dt><dd class="path">{place(vol, parent(path))}</dd>
      {#if versions.length}<dt>{t.versions}</dt><dd><button class="link" onclick={() => (tab = 'versions')}>{t.versionCount(versions.length)}</button></dd>{/if}
    </dl>
  </header>
  <Tabs.Root bind:value={tab} class="tabs">
    <Tabs.List class="tab-list">
      <Tabs.Trigger value="share" class="tab">{t.share}</Tabs.Trigger>
      {#if !entry.dir}<Tabs.Trigger value="versions" class="tab">{t.versions}</Tabs.Trigger>{/if}
    </Tabs.List>
    <Tabs.Content value="share" class="tab-body">
      <SharePanel {vol} {path} dir={entry.dir} />
    </Tabs.Content>
    {#if !entry.dir}
      <Tabs.Content value="versions" class="tab-body">
        <VersionsPanel
          {vol}
          {versions}
          onchange={() => {
            loadVersions()
            onchange()
          }}
        />
      </Tabs.Content>
    {/if}
  </Tabs.Root>
</aside>
