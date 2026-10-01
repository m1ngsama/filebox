<script lang="ts">
  import { linger } from '../lib/motion'
  import { Dialog } from 'bits-ui'
  import type { Snippet } from 'svelte'

  let { title, sub, lead, onclose, children }: { title: string; sub?: string; lead?: Snippet; onclose: () => void; children: Snippet } = $props()

  function drag(node: HTMLElement) {
    let y0 = 0
    let t0 = 0
    let dy = 0
    let on = false
    const down = (e: PointerEvent) => {
      if (e.pointerType === 'mouse' || node.scrollTop > 0) return
      y0 = e.clientY
      t0 = e.timeStamp
      dy = 0
      on = true
      node.style.transition = 'none'
    }
    const move = (e: PointerEvent) => {
      if (!on) return
      dy = Math.max(0, e.clientY - y0)
      if (dy > 4) node.style.translate = `0 ${dy}px`
    }
    const up = (e: PointerEvent) => {
      if (!on) return
      on = false
      node.style.transition = ''
      if (dy > 90 || dy / Math.max(1, e.timeStamp - t0) > 0.6) onclose()
      else node.style.translate = ''
    }
    const click = (e: MouseEvent) => {
      if (dy > 4) e.stopPropagation(), e.preventDefault()
    }
    requestAnimationFrame(() => node.scrollHeight <= node.clientHeight && (node.style.touchAction = 'pan-x'))
    node.addEventListener('pointerdown', down)
    node.addEventListener('pointermove', move)
    node.addEventListener('pointerup', up)
    node.addEventListener('pointercancel', up)
    node.addEventListener('click', click, true)
    return () => {
      node.removeEventListener('pointerdown', down)
      node.removeEventListener('pointermove', move)
      node.removeEventListener('pointerup', up)
      node.removeEventListener('pointercancel', up)
      node.removeEventListener('click', click, true)
    }
  }
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" {@attach linger} />
    <Dialog.Content class="bottom-sheet" preventScroll={false} {@attach linger} {@attach drag}>
      <span class="grabber" aria-hidden="true"></span>
      {#if lead}
        <header class="sheet-head">
          {@render lead()}
          <div>
            <Dialog.Title class="sheet-name">{title}</Dialog.Title>
            {#if sub}<p class="hint">{sub}</p>{/if}
          </div>
        </header>
      {:else}
        <Dialog.Title class="sheet-title">{title}</Dialog.Title>
      {/if}
      {@render children()}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
