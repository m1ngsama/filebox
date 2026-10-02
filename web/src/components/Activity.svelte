<script lang="ts">
  import { icon } from '../lib/icon'
  import Download from '@lucide/svelte/icons/download'
  import Inbox from '@lucide/svelte/icons/inbox'
  import LogIn from '@lucide/svelte/icons/log-in'
  import ShieldAlert from '@lucide/svelte/icons/shield-alert'
  import Link from '@lucide/svelte/icons/link'
  import KeyRound from '@lucide/svelte/icons/key-round'
  import Upload from '@lucide/svelte/icons/upload'
  import Plus from '@lucide/svelte/icons/plus'
  import Pencil from '@lucide/svelte/icons/pencil'
  import History from '@lucide/svelte/icons/history'
  import TextCursorInput from '@lucide/svelte/icons/text-cursor-input'
  import FolderInput from '@lucide/svelte/icons/folder-input'
  import Copy from '@lucide/svelte/icons/copy'
  import Trash2 from '@lucide/svelte/icons/trash-2'
  import ArchiveRestore from '@lucide/svelte/icons/archive-restore'
  import ActivityIcon from '@lucide/svelte/icons/activity'
  import EmptyState from './EmptyState.svelte'
  import { api, selectURL, filesURL, type Activity, type Share } from '../lib/api'
  import { ago, date, size, place, placeOf, parent, base, dated } from '../lib/format'
  import { link } from '../lib/router.svelte'
  import { t } from '../lib/i18n'
  import type { FileKind } from '../lib/i18n/zh'

  let { vol, path = '' }: { vol?: string; path?: string } = $props()

  const icons: Record<string, typeof Link> = {
    download: Download, login: LogIn, login_failed: ShieldAlert, token_create: KeyRound, token_revoke: KeyRound,
    create: Plus, edit: Pencil, revert: History, rename: TextCursorInput, move: FolderInput, copy: Copy, trash: Trash2, restore: ArchiveRestore,
  }
  const fileKinds = new Set(Object.keys(t.fileEvents))
  let events = $state<Activity[] | null>(null)
  let error = $state('')
  let more = $state(false)
  let kind = $state('')
  let share = $state('')
  let shares = $state<Share[]>([])

  $effect(() => void (!vol && api.shares().then((r) => (shares = r.shares), () => {})))

  async function load(before = '') {
    const want = { kind, share, vol, path }
    try {
      const r = await api.activity({ kind, share, before, vol, path: vol ? path || '.' : undefined })
      if (want.kind !== kind || want.share !== share || want.vol !== vol || want.path !== path) return
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
    vol
    path
    events = null
    load()
  })

  const fileEvent = (e: Activity) => fileKinds.has(e.kind) && !!e.vol
  const received = (e: Activity) => e.kind === 'upload' && !!e.share_id
  const together = (a: Activity, b: Activity) =>
    fileEvent(a) && a.kind === b.kind && a.kind !== 'rename' && a.vol === b.vol && a.share_id === b.share_id && parent(a.path) === parent(b.path) && (a.kind !== 'edit' || a.path === b.path) && a.at - b.at < 600
  type Run = { day: string; items: Activity[] }
  const runs = $derived.by(() => {
    const day = dated()
    const out: Run[] = []
    for (const e of events ?? []) {
      const last = out.at(-1)
      const d = day(e.at * 1000)
      if (last && last.day === d && together(last.items.at(-1)!, e)) last.items.push(e)
      else out.push({ day: d, items: [e] })
    }
    return out
  })

  function title(r: Run) {
    const e = r.items[0]
    const n = r.items.length
    if (received(e)) return n > 1 ? t.receivedMany(n) : t.events.upload(e.name)
    if (fileEvent(e)) {
      const k = e.kind as FileKind
      return n > 1 && k !== 'edit' ? t.fileEventsMany[k](n) : t.fileEvents[k](base(e.path), k === 'rename' ? base(e.name) : e.name) + (k === 'edit' && n > 1 ? ` · ${t.fileEventsMany.edit(n)}` : '')
    }
    return (t.events as Record<string, (name: string) => string>)[e.kind]?.(e.kind.startsWith('share_') ? placeOf(e.target) : e.name) ?? e.kind
  }

  function href(e: Activity) {
    if (!e.vol) return ''
    if (e.kind === 'trash') return `/trash/${encodeURIComponent(e.vol)}`
    return selectURL(e.vol, e.path)
  }
</script>

{#if !vol}
  <div class="activity-filters">
    <select bind:value={kind} aria-label={t.activityKinds['']}>
      {#each Object.entries(t.activityKinds) as [k, label] (k)}<option value={k}>{label}</option>{/each}
    </select>
    {#if shares.length}
      <select bind:value={share} aria-label={t.allShares}>
        <option value="">{t.allShares}</option>
        {#each shares as s (s.id)}<option value={String(s.id)}>{place(s.vol, s.path)}</option>{/each}
      </select>
    {/if}
  </div>
{/if}
{#if error}
  <EmptyState icon={ActivityIcon} compact title={t.loadFailedTitle} hint={error}><button onclick={() => load()}>{t.retry}</button></EmptyState>
{:else if events === null}
  <ul class="rows skeleton" aria-busy="true" aria-label={t.loading}>
    {#each { length: 3 }, i (i)}<li><span class="sk sk-icon"></span><span class="sk sk-line"></span></li>{/each}
  </ul>
{:else if !events.length}
  <EmptyState compact icon={ActivityIcon} title={t.noActivity} />
{:else}
  <ul class="rows activity" aria-label={t.activity}>
    {#each runs as r, i (r.items[0].id)}
      {@const e = r.items[0]}
      {@const Icon = received(e) ? Inbox : e.kind === 'upload' ? Upload : (icons[e.kind] ?? Link)}
      {@const to = href(e)}
      {#if r.day !== runs[i - 1]?.day}<li class="day" role="presentation">{r.day}</li>{/if}
      <li class:trashed={e.kind === 'trash'}>
        <Icon size={icon.md} class="row-icon" />
        <div class="row-main">
          {#if to}<a class="row-title" href={to} onclick={link}>{title(r)}</a>{:else}<span class="row-title">{title(r)}</span>{/if}
          <span class="tags">
            {#if r.items.length > 1 && e.kind !== 'edit'}<span class="hint names">{t.andOthers(r.items.slice(0, 3).map((x) => base(x.path) || x.name), r.items.length)}</span>{/if}
            {#if fileEvent(e) && !vol}<a class="tag" href={filesURL(e.vol, parent(e.path))} onclick={link}>{place(e.vol, parent(e.path), 3)}</a>
            {:else if e.target && !e.kind.startsWith('share_')}<span class="tag">{placeOf(e.target)}</span>{/if}
            {#if (e.kind === 'move' || e.kind === 'copy') && r.items.length === 1}<span class="hint">{t.cameFrom(place(e.target || e.vol, parent(e.name), 3))}</span>{/if}
            {#if (e.kind === 'upload' || e.kind === 'download') && e.size}<span class="hint">{size(r.items.reduce((s, x) => s + x.size, 0))}</span>{/if}
            {#if e.visitor}<span class="hint">{e.kind.startsWith('login') ? t.source(e.visitor) : t.visitor(e.visitor)}</span>{/if}
            <span class="hint" title={date(e.at * 1000)}>{ago(e.at * 1000)}</span>
          </span>
        </div>
      </li>
    {/each}
  </ul>
{/if}
{#if more && events}
  <div class="activity-more"><button onclick={() => load(String(events!.at(-1)!.id))}>{t.loadMore}</button></div>
{/if}
