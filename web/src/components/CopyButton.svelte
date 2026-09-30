<script lang="ts">
  import { icon } from '../lib/icon'
  import Copy from '@lucide/svelte/icons/copy'
  import Check from '@lucide/svelte/icons/check'
  import { t } from '../lib/i18n'
  import { toast, fail } from '../lib/toast.svelte'

  let { text, label = t.copyLink, done: msg = t.linkCopied }: { text: string; label?: string; done?: string } = $props()
  let done = $state(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
    } catch (e) {
      return fail(e)
    }
    toast(msg)
    done = true
    setTimeout(() => (done = false), 1500)
  }
</script>

<button type="button" class="icon-btn" aria-label={done ? t.copied : label} title={label} onclick={copy}>
  {#if done}<Check size={icon.md} />{:else}<Copy size={icon.md} />{/if}
</button>
