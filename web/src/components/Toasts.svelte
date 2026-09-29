<script lang="ts">
  import X from '@lucide/svelte/icons/x'
  import CircleCheck from '@lucide/svelte/icons/circle-check'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import Info from '@lucide/svelte/icons/info'
  import { toasts, dismiss, hold, type Toast } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  const icons = { success: CircleCheck, error: CircleAlert, info: Info }
  let region = $state<HTMLElement>()
  let back: HTMLElement | null = null

  function close(x: Toast) {
    if (region?.contains(document.activeElement)) {
      const next = region.querySelector<HTMLElement>(`.toast:not([data-id="${x.id}"]) button`)
      if (next) next.focus()
      else (back?.isConnected && back !== document.body ? back : document.querySelector<HTMLElement>('[role=grid] [tabindex="0"], main'))?.focus()
    }
    dismiss(x.id)
    hold('focus', !!region?.contains(document.activeElement))
  }
</script>

<section
  class="toasts"
  aria-label={t.notifications}
  bind:this={region}
  onpointerenter={(e) => e.pointerType === 'mouse' && hold('hover', true)}
  onpointerleave={(e) => e.pointerType === 'mouse' && hold('hover', false)}
  onfocusin={(e) => {
    if (!region?.contains(e.relatedTarget as Node)) back = e.relatedTarget as HTMLElement | null
    hold('focus', true)
  }}
  onfocusout={(e) => hold('focus', !!region?.contains(e.relatedTarget as Node))}
>
  <div role="status" aria-live="polite">
    {#each toasts.filter((x) => x.kind !== 'error') as x (x.id)}{@render item(x)}{/each}
  </div>
  <div role="alert">
    {#each toasts.filter((x) => x.kind === 'error') as x (x.id)}{@render item(x)}{/each}
  </div>
</section>

{#snippet item(x: (typeof toasts)[number])}
  {@const Icon = icons[x.kind]}
  <div class={`toast ${x.kind}`} data-id={x.id}>
    <Icon size={18} aria-hidden="true" />
    <span class="toast-text">{x.text}</span>
    {#if x.action}
      {@const a = x.action}
      <button
        class="toast-action"
        onclick={() => {
          close(x)
          a.run()
        }}>{a.label}</button
      >
    {/if}
    <button class="icon-btn" aria-label={t.close} onclick={() => close(x)}><X size={16} /></button>
  </div>
{/snippet}
