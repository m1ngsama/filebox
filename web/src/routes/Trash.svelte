<script lang="ts">
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import FileIcon from '../components/FileIcon.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import { api, type TrashItem } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { size, date, ago } from '../lib/format'
  import { t } from '../lib/i18n'

  let { vol, vols }: { vol: string; vols: string[] } = $props()

  let items = $state<TrashItem[] | null>(null)
  let error = $state('')
  let busy = $state('')
  let emptying = $state(false)

  async function load() {
    const want = vol
    try {
      const list = (await api.trash(want)).items
      if (want === vol) items = list
    } catch (e) {
      if (want === vol) error = (e as Error).message
    }
  }

  $effect(() => {
    vol
    items = null
    error = ''
    load()
  })

  async function restore(it: TrashItem) {
    busy = it.id
    error = ''
    try {
      await api.restore(vol, it.id)
    } catch (e) {
      error = `${it.name}：${(e as Error).message}`
    }
    busy = ''
    await load()
  }

  const parent = (p: string) => `${vol}:/${p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : ''}`
</script>

<div class="toolbar">
  <div class="chips" role="group" aria-label={t.trash}>
    {#each vols as v}
      <a class="chip" href={`/trash/${encodeURIComponent(v)}`} onclick={link} aria-current={v === vol ? 'page' : undefined}><HardDrive size={14} />{v}</a>
    {/each}
  </div>
  <span class="grow"></span>
  <button class="danger" disabled={!items?.length} onclick={() => (emptying = true)}><TrashIcon size={16} />{t.emptyTrash}</button>
</div>

{#if error}<p class="error">{error}</p>{/if}
{#if items}
  <ul class="rows">
    {#each items as it (it.id)}
      <li>
        <FileIcon name={it.name} dir={it.dir} />
        <div class="row-main">
          <span class="row-title" title={it.name}>{it.name}</span>
          <span class="hint" title={`${t.origin} ${parent(it.path)}`}>{t.origin} {parent(it.path)}</span>
        </div>
        <span class="num">{it.dir ? '' : size(it.size)}</span>
        <span class="num wide" title={date(it.deleted)}>{t.deletedAt(ago(it.deleted))}</span>
        <button disabled={!!busy} onclick={() => restore(it)}><RotateCcw size={16} />{t.restore}</button>
      </li>
    {:else}
      <li class="hint">{t.trashEmpty}</li>
    {/each}
  </ul>
{/if}

{#if emptying}
  <ConfirmDialog
    title={t.emptyTrash}
    message={t.emptyTrashMessage(items?.length ?? 0, vol)}
    action={t.emptyTrash}
    onconfirm={async () => {
      await api.emptyTrash(vol)
      await load()
    }}
    onclose={() => (emptying = false)}
  />
{/if}
