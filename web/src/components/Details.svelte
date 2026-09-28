<script lang="ts">
  import { Tabs } from 'bits-ui'
  import X from '@lucide/svelte/icons/x'
  import FileIcon from './FileIcon.svelte'
  import SharePanel from './SharePanel.svelte'
  import type { Entry } from '../lib/api'
  import { size, date } from '../lib/format'
  import { t } from '../lib/i18n'

  let { vol, path, entry, thumbs, onclose }: { vol: string; path: string; entry: Entry; thumbs: (string | null)[]; onclose: () => void } = $props()
  let tries = $state(0)
  const src = $derived(thumbs.filter(Boolean)[tries])
</script>

<aside class="details" aria-label={t.details}>
  <header>
    <div class="details-thumb">
      {#if src}<img {src} alt="" onerror={() => tries++} />{:else}<FileIcon name={entry.name} dir={entry.dir} size={64} />{/if}
    </div>
    <button class="icon-btn details-close" aria-label={t.close} onclick={onclose}><X size={20} /></button>
    <h2 title={entry.name}>{entry.name}</h2>
    <dl>
      {#if !entry.dir}<dt>{t.size}</dt><dd>{size(entry.size)}</dd>{/if}
      <dt>{t.mtime}</dt><dd>{date(entry.mtime)}</dd>
      <dt>{t.path}</dt><dd class="path">{vol}:/{path}</dd>
    </dl>
  </header>
  <Tabs.Root value="share" class="tabs">
    <Tabs.List class="tab-list">
      <Tabs.Trigger value="share" class="tab">{t.share}</Tabs.Trigger>
    </Tabs.List>
    <Tabs.Content value="share" class="tab-body">
      <SharePanel {vol} {path} dir={entry.dir} />
    </Tabs.Content>
  </Tabs.Root>
</aside>
