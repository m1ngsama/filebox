<script lang="ts">
  import Link from '@lucide/svelte/icons/link'
  import Trash from '@lucide/svelte/icons/trash'
  import Link2 from '@lucide/svelte/icons/link-2'
  import EmptyState from '../components/EmptyState.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import CopyButton from '../components/CopyButton.svelte'
  import RowList from '../components/RowList.svelte'
  import { api, shareLink, filesURL, type Share } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { date } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'

  let shares = $state<Share[] | null>(null)
  let error = $state('')
  let removing = $state<Share | null>(null)

  async function load() {
    try {
      shares = (await api.shares()).shares
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }
  load()

  const loc = (s: Share) => `${s.vol}:/${s.path === '.' ? '' : s.path}`
  const where = (s: Share) => {
    if (s.path === '.') return filesURL(s.vol, '')
    const i = s.path.lastIndexOf('/')
    return `${filesURL(s.vol, i < 0 ? '' : s.path.slice(0, i))}?${new URLSearchParams({ details: s.path.slice(i + 1) })}`
  }
</script>

<RowList items={shares} {error} key={(s) => s.id}>
  {#snippet row(s)}
    {@const gone = !!s.expires && s.expires * 1000 < Date.now()}
    <Link size={18} class="row-icon" />
    <div class="row-main">
      <a href={where(s)} onclick={link} title={loc(s)}>{loc(s)}</a>
      <span class="tags">
        <span class="tag">{t.modes[s.mode]}</span>
        {#if s.has_password}<span class="tag">{t.hasPassword}</span>{/if}
        <span class="tag" class:warn={gone}>{gone ? t.expired : s.expires ? t.expiresAt(date(s.expires * 1000)) : t.forever}</span>
        <span class="hint">{t.hits(s.hits)}</span>
      </span>
    </div>
    <CopyButton text={shareLink(s.token)} />
    <button class="icon-btn danger" aria-label={t.deleteShare} title={t.deleteShare} onclick={() => (removing = s)}>
      <Trash size={18} />
    </button>
  {/snippet}
  {#snippet empty()}<EmptyState icon={Link2} title={t.noShares} hint={t.sharesEmptyHint} />{/snippet}
</RowList>

{#if removing}
  {@const id = removing.id}
  <ConfirmDialog
    title={t.deleteShareTitle}
    message={t.deleteShareMessage}
    action={t.remove}
    onconfirm={async () => {
      await api.delShare(id)
      toast(t.shareDeleted)
      await load()
    }}
    onclose={() => (removing = null)}
  />
{/if}
