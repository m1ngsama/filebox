<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye'
  import Download from '@lucide/svelte/icons/download'
  import Inbox from '@lucide/svelte/icons/inbox'
  import LogIn from '@lucide/svelte/icons/log-in'
  import ShieldAlert from '@lucide/svelte/icons/shield-alert'
  import Link from '@lucide/svelte/icons/link'
  import KeyRound from '@lucide/svelte/icons/key-round'
  import ActivityIcon from '@lucide/svelte/icons/activity'
  import RowList from './RowList.svelte'
  import EmptyState from './EmptyState.svelte'
  import { api, type Activity, type Share } from '../lib/api'
  import { ago, date, size } from '../lib/format'
  import { t } from '../lib/i18n'

  const icons = { view: Eye, download: Download, upload: Inbox, login: LogIn, login_failed: ShieldAlert, token_create: KeyRound, token_revoke: KeyRound }
  let events = $state<Activity[] | null>(null)
  let error = $state('')
  let more = $state(false)
  let kind = $state('')
  let share = $state('')
  let shares = $state<Share[]>([])

  api.shares().then((r) => (shares = r.shares), () => {})

  async function load(before = '') {
    const want = { kind, share }
    try {
      const r = await api.activity({ ...want, before })
      if (want.kind !== kind || want.share !== share) return
      events = before && events ? [...events, ...r.events] : r.events
      more = r.more
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }

  $effect(() => {
    kind
    share
    events = null
    load()
  })

  const loc = (s: Share) => `${s.vol}:/${s.path === '.' ? '' : s.path}`
</script>

<div class="activity-filters">
  <select bind:value={kind} aria-label={t.activityKinds['']}>
    {#each Object.entries(t.activityKinds) as [k, label] (k)}<option value={k}>{label}</option>{/each}
  </select>
  <select bind:value={share} aria-label={t.allShares}>
    <option value="">{t.allShares}</option>
    {#each shares as s (s.id)}<option value={String(s.id)}>{loc(s)}</option>{/each}
  </select>
</div>
<RowList items={events} {error} key={(e) => e.id} label={t.activity}>
  {#snippet row(e)}
    {@const Icon = icons[e.kind as keyof typeof icons] ?? Link}
    <Icon size={18} class="row-icon" />
    <div class="row-main">
      <span class="row-title">{t.events[e.kind]?.(e.name) ?? e.kind}{e.kind === 'upload' ? ` · ${size(e.size)}` : ''}</span>
      <span class="tags">
        {#if e.share && !e.kind.startsWith('share_')}<span class="tag">{e.share}</span>{/if}
        {#if e.visitor}<span class="hint">{t.visitor(e.visitor)}</span>{/if}
        <span class="hint" title={date(e.at * 1000)}>{ago(e.at * 1000)}</span>
      </span>
    </div>
  {/snippet}
  {#snippet empty()}<EmptyState compact icon={ActivityIcon} title={t.noActivity} />{/snippet}
</RowList>
{#if more && events}
  <div><button onclick={() => load(String(events!.at(-1)!.id))}>{t.loadMore}</button></div>
{/if}
