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
    fresh = false,
    valid = (n) => !n.includes('/'),
    onsave,
    onclose,
  }: {
    title: string
    label: string
    action: string
    value?: string
    stem?: boolean
    fresh?: boolean
    valid?: (name: string) => boolean
    onsave: (name: string) => unknown
    onclose: () => void
  } = $props()

  let name = $state(untrack(() => value))
  let error = $state('')
  let busy = $state(false)
  let input = $state<HTMLInputElement>()
  const ok = $derived(!!name.trim() && valid(name.trim()))

  function focus(e: Event) {
    e.preventDefault()
    input?.focus()
    const dot = name.lastIndexOf('.')
    input?.setSelectionRange(0, stem && dot > 0 ? dot : name.length)
  }

  async function submit() {
    const n = name.trim()
    if (!ok) return
    if (n === value && !fresh) return onclose()
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
    <input bind:this={input} bind:value={name} required autocomplete="off" aria-invalid={!!error} aria-describedby={error ? 'name-error' : undefined} />
  </label>
  {#if error}<p class="error" id="name-error" role="alert">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button class="primary" class:busy disabled={busy || !ok}>{action}</button>
  {/snippet}
</Modal>
