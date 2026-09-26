<script lang="ts">
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import { t } from '../lib/i18n'

  let {
    title,
    label,
    action,
    value = '',
    stem = false,
    onsave,
    onclose,
  }: {
    title: string
    label: string
    action: string
    value?: string
    stem?: boolean
    onsave: (name: string) => Promise<unknown>
    onclose: () => void
  } = $props()

  let name = $state(untrack(() => value))
  let error = $state('')
  let busy = $state(false)
  let input = $state<HTMLInputElement>()

  function focus(e: Event) {
    e.preventDefault()
    input?.focus()
    const dot = name.lastIndexOf('.')
    input?.setSelectionRange(0, stem && dot > 0 ? dot : name.length)
  }

  async function submit() {
    const n = name.trim()
    if (!n || n.includes('/')) return
    if (n === value) return onclose()
    busy = true
    try {
      await onsave(n)
      onclose()
    } catch (e) {
      error = (e as Error).message
    }
    busy = false
  }
</script>

<Modal {title} {onclose} onsubmit={submit} onOpenAutoFocus={focus}>
  <label class="field">
    <span>{label}</span>
    <input bind:this={input} bind:value={name} required autocomplete="off" />
  </label>
  {#if error}<p class="error">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button class="primary" disabled={busy || !name.trim()}>{action}</button>
  {/snippet}
</Modal>
