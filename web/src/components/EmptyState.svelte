<script lang="ts">
  import { icon } from '../lib/icon'
  import type { Component, Snippet } from 'svelte'

  let {
    icon: Icon,
    title,
    hint = '',
    compact = false,
    children,
  }: { icon: Component<{ size?: number }>; title: string; hint?: string; compact?: boolean; children?: Snippet } = $props()
</script>

<div class="empty-state" class:compact>
  <span class="empty-icon"><Icon size={compact ? icon.md : icon.lg} /></span>
  <p class="empty-title">{title}</p>
  {#if hint}<p class="hint">{hint}</p>{/if}
  {@render children?.()}
</div>
