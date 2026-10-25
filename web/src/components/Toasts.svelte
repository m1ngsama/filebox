<script lang="ts">
  import { fade } from '../lib/motion'
  import { icon } from '../lib/icon'
  import X from '@lucide/svelte/icons/x'
  import CircleCheck from '@lucide/svelte/icons/circle-check'
  import CircleAlert from '@lucide/svelte/icons/circle-alert'
  import Info from '@lucide/svelte/icons/info'
  import { toasts, hold, onLeave, act, retract } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  const icons = { success: CircleCheck, error: CircleAlert, info: Info }
  let region = $state<HTMLElement>()
  let back: HTMLElement | null = null

  // A finger drags a notice sideways, and lets it go when it is thrown or pulled far enough.
  function swipe(node: HTMLElement, id: number) {
    let x0 = 0
    let dx = 0
    let t0 = 0
    let on = false
    node.onpointerdown = (e) => {
      if (e.pointerType === 'mouse' || (e.target as Element).closest('button')) return
      on = true
      x0 = e.clientX
      dx = 0
      t0 = e.timeStamp
      node.setPointerCapture(e.pointerId)
      node.style.transition = 'none'
    }
    node.onpointermove = (e) => {
      if (!on) return
      dx = e.clientX - x0
      node.style.translate = `${dx}px 0`
      node.style.opacity = String(1 - Math.min(1, Math.abs(dx) / 240))
    }
    node.onpointerup = node.onpointercancel = (e) => {
      if (!on) return
      on = false
      node.style.transition = ''
      if (Math.abs(dx) > 80 || Math.abs(dx) / Math.max(1, e.timeStamp - t0) > 0.5) retract(id)
      else node.style.translate = node.style.opacity = ''
    }
  }

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
  <div class={`toast ${x.kind}`} data-id={x.id} out:fade|global use:swipe={x.id}>
    <Icon size={icon.md} aria-hidden="true" />
    <span class="toast-text" id={`toast-${x.id}`} aria-hidden="true">{x.text}</span>
    {#each x.actions as a (a.label)}
      <button
        class="toast-action"
        aria-describedby={`toast-${x.id}`}
        aria-keyshortcuts={a.keys}
        onclick={() => {
          act(x.id, a)
        }}>{a.label}</button
      >
    {/each}
    <button class="icon-btn" aria-label={t.close} onclick={() => retract(x.id)}><X size={icon.sm} /></button>
  </div>
{/snippet}
