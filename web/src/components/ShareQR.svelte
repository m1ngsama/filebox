<script lang="ts">
  import { icon } from '../lib/icon'
  import { Popover } from 'bits-ui'
  import QrCode from '@lucide/svelte/icons/qr-code'
  import { t } from '../lib/i18n'

  let { text }: { text: string } = $props()
</script>

<Popover.Root>
  <Popover.Trigger class="icon-btn" aria-label={t.qrCode} title={t.qrCode}><QrCode size={icon.md} /></Popover.Trigger>
  <Popover.Portal>
    <Popover.Content class="menu qr" sideOffset={6}>
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
