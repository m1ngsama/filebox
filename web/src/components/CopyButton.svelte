<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy'
  import Check from '@lucide/svelte/icons/check'
  import { t } from '../lib/i18n'

  let { text, label = t.copyLink }: { text: string; label?: string } = $props()
  let done = $state(false)

  async function copy() {
    await navigator.clipboard.writeText(text)
    done = true
    setTimeout(() => (done = false), 1500)
  }
</script>

<button type="button" class="icon-btn" aria-label={done ? t.copied : label} title={label} onclick={copy}>
  {#if done}<Check size={18} />{:else}<Copy size={18} />{/if}
</button>
