<script lang="ts">
  import { icon } from '../lib/icon'
  import { Dialog } from 'bits-ui'
  import X from '@lucide/svelte/icons/x'
  import type { Snippet } from 'svelte'
  import { t } from '../lib/i18n'

  let {
    title,
    onclose,
    onsubmit,
    onOpenAutoFocus,
    children,
    footer,
  }: {
    title: string
    onclose: () => void
    onsubmit: () => void
    onOpenAutoFocus?: (e: Event) => void
    children: Snippet
    footer: Snippet
  } = $props()
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" />
    <Dialog.Content class="dialog" preventScroll={false} {onOpenAutoFocus}>
      <form
        onsubmit={(e) => {
          e.preventDefault()
          onsubmit()
        }}
      >
        <header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Close type="button" class="icon-btn" aria-label={t.close}><X size={icon.md} /></Dialog.Close>
        </header>
        {@render children()}
        <footer>{@render footer()}</footer>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
