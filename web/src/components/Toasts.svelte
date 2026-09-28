<script lang="ts">
  import X from '@lucide/svelte/icons/x'
  import CircleCheck from '@lucide/svelte/icons/circle-check'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import Info from '@lucide/svelte/icons/info'
  import { toasts, dismiss, pause } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  const icons = { success: CircleCheck, error: CircleAlert, info: Info }
</script>

<section
  class="toasts"
  aria-label={t.notifications}
  onmouseenter={() => pause(true)}
  onmouseleave={() => pause(false)}
  onfocusin={() => pause(true)}
  onfocusout={() => pause(false)}
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
  <div class={`toast ${x.kind}`}>
    <Icon size={18} aria-hidden="true" />
    <span class="toast-text">{x.text}</span>
    {#if x.action}
      {@const a = x.action}
      <button
        class="toast-action"
        onclick={() => {
          dismiss(x.id)
          a.run()
        }}>{a.label}</button
      >
    {/if}
    <button class="icon-btn" aria-label={t.close} onclick={() => dismiss(x.id)}><X size={16} /></button>
  </div>
{/snippet}
