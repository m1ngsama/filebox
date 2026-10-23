<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { icon } from '../lib/icon'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import Copy from '@lucide/svelte/icons/copy'
  import ChevronDown from '@lucide/svelte/icons/chevron-down'
  import Trash from '@lucide/svelte/icons/trash'
  import EmptyState from '../components/EmptyState.svelte'
  import FileIcon from '../components/FileIcon.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import { api, selectURL, type DuplicateReport } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { size, ago, date } from '../lib/format'
  import { toast } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  let min = $state('1048576')
  let report = $state.raw<DuplicateReport>()
  let error = $state('')
  let round = $state(0)
  let confirm = $state(false)
  const chosen = new SvelteSet<string>()

  const key = (f: { vol: string; path: string }) => `${f.vol}\0${f.path}`

  $effect(() => {
    void round
    const stop = new AbortController()
    let timer = 0
    api.duplicates(min, stop.signal).then(
      (r) => {
        if (stop.signal.aborted) return
        report = r
        error = ''
        if (r.pending > 0) timer = window.setTimeout(() => round++, 500)
      },
      (e: Error) => !stop.signal.aborted && (error = e.message),
    )
    return () => {
      stop.abort()
      clearTimeout(timer)
    }
  })

  function choose(v: string) {
    min = v
    report = undefined
    chosen.clear()
  }

  function keepOldest() {
    chosen.clear()
    for (const g of report?.groups ?? []) for (const f of g.files.slice(1)) chosen.add(key(f))
  }

  const picked = $derived(
    (report?.groups ?? []).flatMap((g) => g.files.filter((f) => chosen.has(key(f)))),
  )
  const allPicked = (g: NonNullable<typeof report>['groups'][number]) => g.files.every((f) => chosen.has(key(f)))

  async function trash() {
    const by = new Map<string, string[]>()
    for (const f of picked) by.set(f.vol, [...(by.get(f.vol) ?? []), f.path])
    let moved = 0
    for (const [vol, paths] of by) {
      const r = await api.rm(vol, paths)
      moved += r.trashed.length
      if (r.failed.length) throw new Error(r.failed[0].error)
    }
    toast(t.dupTrashed(moved))
    chosen.clear()
    round++
  }
</script>

<div class="dups">
  <div class="filters" role="group" aria-label={t.dupMin}>
    <label class="chip-select on">
      <span>{t.dupMin}: {t.dupSizes[min]}</span>
      <ChevronDown size={icon.sm} aria-hidden="true" />
      <select aria-label={t.dupMin} value={min} onchange={(e) => choose(e.currentTarget.value)}>
        {#each Object.entries(t.dupSizes) as [k, v] (k)}<option value={k}>{v}</option>{/each}
      </select>
    </label>
    {#if report?.groups.length}<span class="hint">{t.dupWasted(size(report.wasted))}</span>{/if}
  </div>

  {#if error}
    <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error}>
      <button class="primary" onclick={() => round++}>{t.retry}</button>
    </EmptyState>
  {:else if report}
    {#if report.pending > 0}<p class="hint banner" role="status">{t.dupScanning(report.pending)}</p>{:else if report.scanning}<p class="hint banner">{t.indexing}</p>{/if}
    {#if !report.groups.length && !report.pending}
      <EmptyState icon={Copy} title={t.dupEmpty} hint={t.dupEmptyHint} />
    {/if}
    {#each report.groups as g (key(g.files[0]))}
      <section class="dup-group" aria-label={g.files[0].name}>
        <h2>{t.dupCopies(g.files.length, size(g.size))}</h2>
        <ul>
          {#each g.files as f (key(f))}
            <li>
              <label class="dup-file">
                <input type="checkbox" checked={chosen.has(key(f))} disabled={!chosen.has(key(f)) && g.files.length - g.files.filter((x) => chosen.has(key(x))).length < 2} onchange={(e) => (e.currentTarget.checked ? chosen.add(key(f)) : chosen.delete(key(f)))} />
                <FileIcon name={f.name} dir={false} size={icon.md} />
                <span class="dup-name">{f.name}<span class="hint"> · {f.vol}/{f.path.slice(0, -f.name.length).replace(/\/$/, '')}</span></span>
                <span class="hint" title={date(f.mtime)}>{ago(f.mtime)}</span>
              </label>
              <a class="dup-open" href={selectURL(f.vol, f.path)} onclick={link}>{t.open}</a>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}

  {#if picked.length || (report?.groups.length ?? 0) > 0}
    <div class="dup-bar">
      <button onclick={keepOldest}>{t.dupKeepOldest}</button>
      {#if picked.length}
        <button onclick={() => chosen.clear()}>{t.dupClear}</button>
        <button class="danger-fill" onclick={() => (confirm = true)}><Trash size={icon.sm} />{t.dupTrash(picked.length)}</button>
      {/if}
    </div>
  {/if}
</div>

{#if confirm}
  <ConfirmDialog title={t.dupConfirmTitle} message={t.dupConfirm(picked.length)} action={t.dupTrash(picked.length)} onconfirm={trash} onclose={() => (confirm = false)} />
{/if}
