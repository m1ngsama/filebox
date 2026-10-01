<script lang="ts">
  import { icon } from '../lib/icon'
  import type { Snippet } from 'svelte'
  import { DropdownMenu } from 'bits-ui'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import QrCode from '@lucide/svelte/icons/qr-code'
  import Pencil from '@lucide/svelte/icons/pencil'
  import Trash from '@lucide/svelte/icons/trash'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import CopyButton from './CopyButton.svelte'
  import ShareEditor from './ShareEditor.svelte'
  import ShareQR from './ShareQR.svelte'
  import { api, shareLink, type Share } from '../lib/api'
  import { shareSummary, lapsed } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'

  let { share, dir, onchange, children }: { share: Share; dir: boolean; onchange: () => void; children?: Snippet } = $props()
  let editing = $state(false)
  let removing = $state(false)
  let qr = $state(false)
  let more = $state<HTMLElement | null>(null)
</script>

{#snippet summary()}{#each shareSummary(share) as part, i (i)}{#if i}{' · '}{/if}<span>{part}</span>{/each}{/snippet}

<div class="share-meta" class:expired={lapsed(share)}>
  {#if children}
    {@render children()}
    <span class="hint share-summary">{@render summary()}</span>
  {:else}
    <a class="share-summary" href={shareLink(share.token)} title={shareLink(share.token)} target="_blank" rel="noreferrer">{@render summary()}</a>
  {/if}
  {#if share.note}<span class="hint share-note" title={share.note}>{share.note}</span>{/if}
</div>
{#if !lapsed(share)}<CopyButton text={shareLink(share.token)} />{/if}
<DropdownMenu.Root>
  <DropdownMenu.Trigger class="icon-btn" aria-label={t.shareActions} title={t.shareActions} bind:ref={more}><Ellipsis size={icon.md} /></DropdownMenu.Trigger>
  <DropdownMenu.Portal to="main">
    <DropdownMenu.Content class="menu" preventScroll={false} align="end" sideOffset={4}>
      <DropdownMenu.Item class="menu-item" onSelect={() => (qr = true)}><QrCode size={icon.sm} />{t.qrCode}</DropdownMenu.Item>
      <DropdownMenu.Item class="menu-item" onSelect={() => (editing = true)}><Pencil size={icon.sm} />{t.editShare}</DropdownMenu.Item>
      <DropdownMenu.Separator class="menu-sep" />
      <DropdownMenu.Item class="menu-item danger" onSelect={() => (removing = true)}><Trash size={icon.sm} />{t.deleteShare}</DropdownMenu.Item>
    </DropdownMenu.Content>
  </DropdownMenu.Portal>
</DropdownMenu.Root>

{#if qr}
  <ShareQR text={shareLink(share.token)} anchor={more} onclose={() => (qr = false)} />
{/if}
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
