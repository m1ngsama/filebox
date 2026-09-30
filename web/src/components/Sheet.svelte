<script lang="ts">
  import { linger } from '../lib/motion'
  import { Dialog } from 'bits-ui'
  import type { Snippet } from 'svelte'

  let { title, sub, lead, onclose, children }: { title: string; sub?: string; lead?: Snippet; onclose: () => void; children: Snippet } = $props()
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" {@attach linger} />
    <Dialog.Content class="bottom-sheet" preventScroll={false} {@attach linger}>
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
