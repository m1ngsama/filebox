<script lang="ts">
  import Modal from './Modal.svelte'
  import { t } from '../lib/i18n'

  let { title, message, action, onconfirm, onclose }: { title: string; message: string; action: string; onconfirm: () => Promise<unknown>; onclose: () => void } =
    $props()

  let error = $state('')
  let busy = $state(false)

  async function submit() {
    busy = true
    try {
      await onconfirm()
      onclose()
    } catch (e) {
      error = (e as Error).message
    }
    busy = false
  }
</script>

<Modal {title} {onclose} onsubmit={submit}>
  <p>{message}</p>
  {#if error}<p class="error">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button class="danger-fill" disabled={busy}>{action}</button>
  {/snippet}
</Modal>
