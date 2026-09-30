<script lang="ts">
  import { icon } from '../lib/icon'
  import type { Component, Snippet } from 'svelte'

  let {
    icon: Icon,
    title,
    hint = '',
    compact = false,
    as = 'p',
    children,
  }: { icon: Component<{ size?: number }>; title: string; hint?: string; compact?: boolean; as?: 'p' | 'h1' | 'h2'; children?: Snippet } = $props()
</script>

<div class="empty-state" class:compact>
  <span class="empty-icon"><Icon size={compact ? icon.md : icon.lg} /></span>
  <svelte:element this={as} class="empty-title">{title}</svelte:element>
  {#if hint}<p class="hint">{hint}</p>{/if}
  {@render children?.()}
</div>
