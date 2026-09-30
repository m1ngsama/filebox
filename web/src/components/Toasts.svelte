<script lang="ts">
  import X from '@lucide/svelte/icons/x'
  import CircleCheck from '@lucide/svelte/icons/circle-check'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import Info from '@lucide/svelte/icons/info'
  import { toasts, dismiss, hold, onLeave } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  const icons = { success: CircleCheck, error: CircleAlert, info: Info }
  let region = $state<HTMLElement>()
  let back: HTMLElement | null = null

  onLeave((id) => {
    if (!region?.querySelector(`[data-id="${id}"]`)?.contains(document.activeElement)) return
    const next = region.querySelector<HTMLElement>(`.toast:not([data-id="${id}"]) button`)
    if (next) next.focus()
    else (back?.isConnected && back !== document.body ? back : document.querySelector<HTMLElement>('[role=grid] [tabindex="0"], main'))?.focus()
    hold('focus', region.contains(document.activeElement))
  })
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
  {#each toasts as x (x.id)}{@render item(x)}{/each}
</section>
<div class="sr-only" role="status">{#each toasts.filter((x) => x.kind !== 'error') as x (x.id)}<p>{x.text}</p>{/each}</div>
<div class="sr-only" role="alert">{#each toasts.filter((x) => x.kind === 'error') as x (x.id)}<p>{x.text}</p>{/each}</div>

{#snippet item(x: (typeof toasts)[number])}
  {@const Icon = icons[x.kind]}
  <div class={`toast ${x.kind}`} data-id={x.id}>
    <Icon size={18} aria-hidden="true" />
    <span class="toast-text" id={`toast-${x.id}`} aria-hidden="true">{x.text}</span>
    {#each x.actions as a (a.label)}
      <button
        class="toast-action"
        aria-describedby={`toast-${x.id}`}
        aria-keyshortcuts={a.keys}
        onclick={() => {
          dismiss(x.id)
          a.run()
        }}>{a.label}</button
      >
    {/each}
    <button class="icon-btn" aria-label={t.close} onclick={() => dismiss(x.id)}><X size={16} /></button>
  </div>
{/snippet}
