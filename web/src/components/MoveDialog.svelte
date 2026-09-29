<script lang="ts">
  import Folder from '@lucide/svelte/icons/folder'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import { api, type Move } from '../lib/api'
  import { t } from '../lib/i18n'

  let {
    vols,
    vol,
    dir,
    names,
    ondone,
    onclose,
  }: { vols: string[]; vol: string; dir: string; names: string[]; ondone: (done: Move[], copy: boolean) => void; onclose: () => void } = $props()

  let at = $state(untrack(() => ({ vol, path: dir })))
  let folders = $state<string[]>([])
  let error = $state('')
  let busy = $state(false)
  let status = $state('')
  const same = $derived(at.vol === vol && at.path === dir)
  const crumbs = $derived(at.path ? at.path.split('/') : [])
  const join = (d: string, n: string) => (d ? `${d}/${n}` : n)

  $effect(() => {
    let stale = false
    api.ls(at.vol, at.path).then(
      (r) => {
        if (stale) return
        folders = r.entries.filter((e) => e.dir).map((e) => e.name)
        error = ''
      },
      (e) => {
        if (stale) return
        folders = []
        error = e.message
      },
    )
    return () => {
      stale = true
    }
  })

  async function run(copy: boolean) {
    busy = true
    error = ''
    const r = await api.transfer(vol, dir, names, at, copy, (i, n, s) =>
      (status = t.progress(i + 1, names.length, n, s?.total ? `${Math.floor((s.done / s.total) * 100)}%` : '')),
    )
    ondone(r.done, copy)
    if (r.error) error = r.error.message
    else onclose()
    busy = false
    status = ''
  }
</script>

<Modal title={t.moveCopyTitle(t.what(names))} {onclose} onsubmit={() => !same && run(false)}>
  <div class="picker">
    <div class="picker-vols" role="group" aria-label={t.chooseFolder}>
      {#each vols as v}
        <button type="button" class="chip" aria-pressed={at.vol === v} onclick={() => (at = { vol: v, path: '' })}><HardDrive size={14} />{v}</button>
      {/each}
    </div>
    <nav class="crumbs" aria-label={t.breadcrumb}>
      <button type="button" onclick={() => (at = { vol: at.vol, path: '' })}>{at.vol}</button>
      {#each crumbs as c, i}
        <ChevronRight size={14} />
        <button type="button" onclick={() => (at = { vol: at.vol, path: crumbs.slice(0, i + 1).join('/') })}>{c}</button>
      {/each}
    </nav>
    <ul class="picker-list">
      {#each folders as f (f)}
        <li>
          <button type="button" onclick={() => (at = { vol: at.vol, path: join(at.path, f) })}><Folder size={18} /><span>{f}</span></button>
        </li>
      {:else}
        <li class="hint">{t.noSubfolders}</li>
      {/each}
    </ul>
  </div>
  {#if status}<p class="hint">{status}</p>{/if}
  {#if error}<p class="error">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button type="button" disabled={busy || same} onclick={() => run(true)}>{t.copy}</button>
    <button class="primary" disabled={busy || same}>{t.move}</button>
  {/snippet}
</Modal>
