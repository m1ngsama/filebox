<script lang="ts" generics="P extends Record<string, any>">
  import type { Component } from 'svelte'
  import { t } from '../lib/i18n'
  import EmptyState from './EmptyState.svelte'
  import CloudOff from '@lucide/svelte/icons/cloud-off'

  let { load, ...props }: { load: () => Promise<{ default: Component<P> }> } & (string extends keyof P ? {} : P) = $props()
</script>

{#await load()}
  <div class="loading" role="status" aria-label={t.loading}></div>
{:then { default: C }}
  <C {...props as unknown as P} />
{:catch}
  <EmptyState icon={CloudOff} title={t.loadFailedTitle} hint={t.loadFailed}>
    <button class="primary" onclick={() => location.reload()}>{t.retry}</button>
  </EmptyState>
{/await}
