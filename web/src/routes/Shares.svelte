<script lang="ts">
  import Link from '@lucide/svelte/icons/link'
  import Link2 from '@lucide/svelte/icons/link-2'
  import EmptyState from '../components/EmptyState.svelte'
  import RowList from '../components/RowList.svelte'
  import ShareRow from '../components/ShareRow.svelte'
  import { api, filesURL, type Share } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { loc } from '../lib/format'
  import { t } from '../lib/i18n'

  let shares = $state<Share[] | null>(null)
  let error = $state('')

  async function load() {
    try {
      shares = (await api.shares()).shares
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }
  load()

  const where = (s: Share) => {
    if (s.path === '.') return filesURL(s.vol, '')
    const i = s.path.lastIndexOf('/')
    return `${filesURL(s.vol, i < 0 ? '' : s.path.slice(0, i))}?${new URLSearchParams({ details: s.path.slice(i + 1) })}`
  }
</script>

<RowList items={shares} {error} key={(s) => s.id}>
  {#snippet row(s)}
    <Link size={18} class="row-icon" />
    <ShareRow share={s} dir={s.dir} onchange={load}>
      <a href={where(s)} onclick={link} title={loc(s)}>{loc(s)}</a>
    </ShareRow>
  {/snippet}
  {#snippet empty()}<EmptyState icon={Link2} title={t.noShares} hint={t.sharesEmptyHint} />{/snippet}
</RowList>

