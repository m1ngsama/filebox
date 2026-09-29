<script lang="ts">
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import FolderPicker from './FolderPicker.svelte'
  import { filesURL } from '../lib/api'
  import { enqueue } from '../lib/uploads.svelte'
  import { navigate } from '../lib/router.svelte'
  import { t } from '../lib/i18n'

  let { vols, onclose }: { vols: string[]; onclose: () => void } = $props()

  const CACHE = 'share-target'
  let at = $state({ vol: untrack(() => vols[0]), path: '' })
  let error = $state('')
  let files = $state.raw<File[] | null>(null)

  caches.open(CACHE).then(async (c) => {
    const out: File[] = []
    for (const k of await c.keys()) {
      const r = await c.match(k)
      if (!r) continue
      const h = r.headers
      out.push(new File([await r.blob()], decodeURIComponent(h.get('X-Name') ?? 'file'), { type: h.get('Content-Type') ?? '', lastModified: Number(h.get('X-Modified')) || Date.now() }))
    }
    files = out
    if (!out.length) onclose()
  }, () => onclose())

  function done() {
    caches.delete(CACHE)
    onclose()
  }

  function upload() {
    if (!files?.length) return
    enqueue(files.map((file) => ({ file })), '/upload/', { vol: at.vol, dir: at.path }, () => {})
    navigate(filesURL(at.vol, at.path))
    done()
  }
</script>

{#if files?.length}
  <Modal title={t.uploadTo} onclose={done} onsubmit={upload}>
    <p class="hint">{t.sharedFiles(files.map((f) => f.name))}</p>
    <FolderPicker {vols} bind:at bind:error />
    {#if error}<p class="error">{error}</p>{/if}
    {#snippet footer()}
      <button type="button" onclick={done}>{t.cancel}</button>
      <button class="primary">{t.uploadHere}</button>
    {/snippet}
  </Modal>
{/if}
