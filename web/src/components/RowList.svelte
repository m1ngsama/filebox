<script lang="ts" generics="T">
  import type { Snippet } from 'svelte'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import EmptyState from './EmptyState.svelte'
  import { t } from '../lib/i18n'

  let {
    items,
    error = '',
    key,
    label,
    row,
    empty,
    onretry,
  }: { items: T[] | null; error?: string; key: (x: T) => unknown; label?: string; row: Snippet<[T]>; empty: Snippet; onretry?: () => void } = $props()
</script>

{#if error}
  <EmptyState icon={CloudOff} compact title={t.loadFailedTitle} hint={error}>
    {#if onretry}<button onclick={onretry}>{t.retry}</button>{/if}
  </EmptyState>
{/if}
{#if items === null}
  {#if !error}
  <ul class="rows skeleton" aria-busy="true" aria-label={t.loading}>
    {#each { length: 3 }, i (i)}<li><span class="sk sk-icon"></span><span class="sk sk-line"></span></li>{/each}
  </ul>
  {/if}
{:else if items.length}
  <ul class="rows" aria-label={label}>
    {#each items as x (key(x))}<li>{@render row(x)}</li>{/each}
  </ul>
{:else}
  {@render empty()}
{/if}
