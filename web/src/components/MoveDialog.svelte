<script lang="ts">
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import FolderPicker from './FolderPicker.svelte'
  import { api, type Move } from '../lib/api'
  import { t } from '../lib/i18n'

  let {
    vols,
    vol,
    dir,
    names: picked,
    extra = [],
    ondone,
    onclose,
  }: { vols: string[]; vol: string; dir: string; names: string[]; extra?: string[]; ondone: (done: Move[], copy: boolean) => void; onclose: () => void } = $props()

  let at = $state(untrack(() => ({ vol, path: dir })))
  let error = $state('')
  let busy = $state(false)
  let carry = $state(true)
  const names = $derived(carry ? [...picked, ...extra] : picked)
  let status = $state('')
  const same = $derived(at.vol === vol && at.path === dir)

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

<Modal title={t.moveCopyTitle(t.what(picked))} {onclose} onsubmit={() => !same && run(false)}>
  <FolderPicker {vols} bind:at bind:error />
  {#if extra.length}<label class="check"><input type="checkbox" bind:checked={carry} />{t.withSubtitles(extra.length)}</label>{/if}
  {#if status}<p class="hint">{status}</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button type="button" disabled={busy || same} onclick={() => run(true)}>{t.copy}</button>
    <button class="primary" disabled={busy || same}>{t.move}</button>
  {/snippet}
</Modal>
