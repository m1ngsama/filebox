<script lang="ts">
  import X from '@lucide/svelte/icons/x'
  import RotateCw from '@lucide/svelte/icons/rotate-cw'
  import ChevronDown from '@lucide/svelte/icons/chevron-down'
  import ChevronUp from '@lucide/svelte/icons/chevron-up'
  import CircleCheck from '@lucide/svelte/icons/circle-check'
  import FileIcon from './FileIcon.svelte'
  import { uploads, totals, cancel, retry, clearFailed } from '../lib/uploads.svelte'
  import { size } from '../lib/format'
  import { narrow } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'

  let h = $state(0)
  let open = $state<boolean | null>(null)
  $effect(() => document.documentElement.style.setProperty('--up-h', `${h}px`))

  const collapsed = $derived(open === null ? narrow.current : !open)
  const active = $derived(totals.files > totals.ok)
  const failed = $derived(uploads.filter((u) => u.state === 'error').length)
  const left = $derived(totals.speed > 0 ? (totals.bytes - totals.sent) / totals.speed : 0)
  const title = $derived(
    active
      ? [t.uploading(totals.ok, totals.files), totals.speed > 0 && `${size(totals.speed)}/s`, left >= 1 && t.eta(left)].filter(Boolean).join(' · ')
      : t.uploadsFailed(failed),
  )
</script>

{#if uploads.length}
  <aside class="uploads" class:collapsed aria-label={t.uploads} bind:offsetHeight={h}>
    <header>
      <button class="up-title" aria-expanded={!collapsed} onclick={() => (open = collapsed)}>
        <span>{title}</span>
        {#if collapsed}<ChevronUp size={18} />{:else}<ChevronDown size={18} />{/if}
      </button>
      {#if !active}<button class="icon-btn" aria-label={t.close} onclick={clearFailed}><X size={18} /></button>{/if}
    </header>
    {#if active}<progress class="up-total" max={totals.bytes || 1} value={totals.sent}></progress>{/if}
    {#if !collapsed}
      <ul>
        {#each uploads as u (u.id)}
          <li class={u.state}>
            <FileIcon name={u.name} dir={u.dir} size={20} />
            <span class="name" title={u.name}>{u.name}</span>
            {#if u.state === 'error'}
              <span class="meta">{u.error}</span>
            {:else}
              <progress max={u.total || 1} value={u.state === 'done' ? u.total || 1 : u.sent}></progress>
            {/if}
            {#if u.dir}<span class="count">{u.ok}/{u.files}</span>{/if}
            {#if u.state === 'error'}
              <button class="icon-btn" aria-label={t.retryItem(u.name)} title={t.retry} onclick={() => retry(u)}><RotateCw size={16} /></button>
            {:else if u.state === 'done'}
              <CircleCheck size={16} class="up-ok" aria-label={t.uploadDone} />
            {:else}
              <button class="icon-btn" aria-label={t.cancelItem(u.name)} title={t.cancel} onclick={() => cancel(u)}><X size={16} /></button>
            {/if}
          </li>
        {/each}
      </ul>
      {#if failed}<p class="hint">{t.resumeHint}</p>{/if}
    {/if}
  </aside>
{/if}
