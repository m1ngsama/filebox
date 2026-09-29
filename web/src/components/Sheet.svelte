<script lang="ts">
  import { Dialog } from 'bits-ui'
  import type { Snippet } from 'svelte'

  let { title, onclose, children }: { title: string; onclose: () => void; children: Snippet } = $props()
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" />
    <Dialog.Content class="sheet" preventScroll={false}>
      <Dialog.Title class="sheet-title">{title}</Dialog.Title>
      {@render children()}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
