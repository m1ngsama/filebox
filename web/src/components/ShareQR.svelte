<script lang="ts">
  import { Popover } from 'bits-ui'
  import { t } from '../lib/i18n'

  let { text, anchor, onclose }: { text: string; anchor: HTMLElement | null; onclose: () => void } = $props()
</script>

<Popover.Root open onOpenChange={(o) => !o && onclose()}>
  <Popover.Portal>
    <Popover.Content class="menu qr" sideOffset={6} customAnchor={anchor} role="dialog" aria-label={t.qrCode}>
      {#await import('../lib/qr')}
        <div class="spinner" role="status" aria-label={t.loading}></div>
      {:then { qrPath }}
        {@const q = qrPath(text)}
        <svg viewBox="0 0 {q.size} {q.size}" role="img" aria-label={t.qrCode} shape-rendering="crispEdges">
          <rect width="100%" height="100%" fill="#fff" /><path d={q.d} fill="#000" />
        </svg>
      {/await}
      <p class="hint">{t.qrHint}</p>
    </Popover.Content>
  </Popover.Portal>
</Popover.Root>
