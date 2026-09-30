<script lang="ts">
  import { icon } from '../lib/icon'
  import Folder from '@lucide/svelte/icons/folder'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import { api, type Loc } from '../lib/api'
  import { t } from '../lib/i18n'
  import { child, arrange } from '../lib/format'

  let { vols, at = $bindable(), error = $bindable('') }: { vols: string[]; at: Loc; error?: string } = $props()

  let folders = $state<string[]>([])
  const crumbs = $derived(at.path ? at.path.split('/') : [])

  $effect(() => {
    const stop = new AbortController()
    api.ls(at.vol, at.path, stop.signal).then(
      (es) => {
        if (stop.signal.aborted) return
        folders = arrange(es.filter((e) => e.dir), '', 'name', false).map((e) => e.name)
        error = ''
      },
      (e) => {
        if (stop.signal.aborted) return
        folders = []
        error = e.message
      },
    )
    return () => stop.abort()
  })
</script>

<div class="picker">
  <div class="picker-vols" role="group" aria-label={t.chooseFolder}>
    {#each vols as v}
      <button type="button" class="chip" aria-pressed={at.vol === v} onclick={() => (at = { vol: v, path: '' })}><HardDrive size={icon.sm} />{v}</button>
    {/each}
  </div>
  <nav class="crumbs" aria-label={t.pickerPath}>
    <button type="button" onclick={() => (at = { vol: at.vol, path: '' })}>{at.vol}</button>
    {#each crumbs as c, i}
      <ChevronRight size={icon.sm} />
      <button type="button" onclick={() => (at = { vol: at.vol, path: crumbs.slice(0, i + 1).join('/') })}>{c}</button>
    {/each}
  </nav>
  <ul class="picker-list">
    {#each folders as f (f)}
      <li>
        <button type="button" onclick={() => (at = { vol: at.vol, path: child(at.path, f) })}><Folder size={icon.md} /><span>{f}</span></button>
      </li>
    {:else}
      <li class="hint">{t.noSubfolders}</li>
    {/each}
  </ul>
</div>
