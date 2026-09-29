<script lang="ts">
  import type { Snippet } from 'svelte'
  import Pencil from '@lucide/svelte/icons/pencil'
  import Trash from '@lucide/svelte/icons/trash'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import CopyButton from './CopyButton.svelte'
  import ShareEditor from './ShareEditor.svelte'
  import ShareQR from './ShareQR.svelte'
  import { api, shareLink, type Share } from '../lib/api'
  import { shareSummary } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'

  let { share, dir, onchange, children }: { share: Share; dir: boolean; onchange: () => void; children: Snippet } = $props()
  let editing = $state(false)
  let removing = $state(false)
</script>

<div class="share-meta">
  {@render children()}
  <span class="hint share-summary">
    {#each shareSummary(share) as part, i (i)}{#if i}{' · '}{/if}<span>{part}</span>{/each}
  </span>
  {#if share.note}<span class="hint share-note" title={share.note}>{share.note}</span>{/if}
</div>
<ShareQR text={shareLink(share.token)} />
<CopyButton text={shareLink(share.token)} />
<button class="icon-btn" aria-label={t.editShare} title={t.editShare} onclick={() => (editing = true)}><Pencil size={18} /></button>
<button class="icon-btn danger" aria-label={t.deleteShare} title={t.deleteShare} onclick={() => (removing = true)}><Trash size={18} /></button>

{#if editing}
  <ShareEditor {share} {dir} onclose={() => (editing = false)} onsaved={onchange} />
{/if}
{#if removing}
  <ConfirmDialog
    title={t.deleteShareTitle}
    message={t.deleteShareMessage}
    action={t.remove}
    onconfirm={async () => {
      await api.delShare(share.id)
      toast(t.shareDeleted)
      onchange()
    }}
    onclose={() => (removing = false)}
  />
{/if}
