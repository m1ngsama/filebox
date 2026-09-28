<script lang="ts" generics="P extends Record<string, any>">
  import type { Component } from 'svelte'
  import { t } from '../lib/i18n'

  let { load, ...props }: { load: () => Promise<{ default: Component<P> }> } & (string extends keyof P ? {} : P) = $props()
</script>

{#await load()}
  <div class="loading" role="status" aria-label={t.loading}></div>
{:then { default: C }}
  <C {...props as unknown as P} />
{:catch}
  <div class="load-error">
    <p class="error">{t.loadFailed}</p>
    <button onclick={() => location.reload()}>{t.retry}</button>
  </div>
{/await}
