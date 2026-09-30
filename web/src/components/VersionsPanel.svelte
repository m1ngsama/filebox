<script lang="ts">
  import Download from '@lucide/svelte/icons/download'
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw'
  import Trash from '@lucide/svelte/icons/trash'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { api, versionURL, type Version } from '../lib/api'
  import { ago, date, size } from '../lib/format'
  import { t } from '../lib/i18n'
  import { fail, toast } from '../lib/toast.svelte'

  let { vol, versions, onchange }: { vol: string; versions: Version[]; onchange: () => void } = $props()
  let removing = $state<Version | null>(null)

  async function restore(x: Version) {
    const r = await api.restoreVersion(vol, x.id).catch((e) => void fail(e))
    if (!r) return
    onchange()
    const run = () => api.restoreVersion(vol, r.prev).then(() => toast(t.undone), fail).finally(onchange)
    toast(t.versionRestored, { action: r.prev ? { label: t.undo, keys: 'Control+Z Meta+Z', run } : undefined })
  }
</script>

<ul class="versions">
  {#each versions as x (x.id)}
    <li>
      <div class="share-meta">
        <a href={versionURL(vol, x.id)} target="_blank" rel="noreferrer">{date(x.created)}</a>
        <span class="hint">{ago(x.created)} · {size(x.size)} · {t.versionSources[x.source] ?? x.source}</span>
      </div>
      <a class="icon-btn" href={versionURL(vol, x.id, true)} download aria-label={t.download} title={t.download}><Download size={18} /></a>
      <button class="icon-btn" aria-label={t.restore} title={t.restore} onclick={() => restore(x)}><RotateCcw size={18} /></button>
      <button class="icon-btn danger" aria-label={t.deleteVersion} title={t.deleteVersion} onclick={() => (removing = x)}><Trash size={18} /></button>
    </li>
  {:else}
    <li class="hint">{t.noVersions}</li>
  {/each}
</ul>
<p class="hint">{t.versionsNote}</p>

{#if removing}
  {@const x = removing}
  <ConfirmDialog
    title={t.deleteVersion}
    message={t.deleteVersionMessage}
    action={t.remove}
    onconfirm={async () => {
      await api.delVersion(vol, x.id)
      toast(t.versionDeleted)
      onchange()
    }}
    onclose={() => (removing = null)}
  />
{/if}
