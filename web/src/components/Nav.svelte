<script lang="ts">
  import { icon } from '../lib/icon'
  import Clock from '@lucide/svelte/icons/clock'
  import Star from '@lucide/svelte/icons/star'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import Share2 from '@lucide/svelte/icons/share-2'
  import Trash from '@lucide/svelte/icons/trash'
  import Settings from '@lucide/svelte/icons/settings'
  import LogOut from '@lucide/svelte/icons/log-out'
  import { link } from '../lib/router.svelte'
  import { api, type Usage } from '../lib/api'
  import { size } from '../lib/format'
  import { shell, narrow } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'

  let { vols, parts, onlogout }: { vols: string[]; parts: string[]; onlogout: () => void } = $props()
  let usage = $state.raw<Record<string, Usage>>({})
  const measure = () =>
    api.vols().then(
      (r) => (usage = Object.fromEntries(r.vols.map((u) => [u.name, u]))),
      () => {},
    )
  $effect(() => void measure())
  const cur = $derived(parts[0] === 'files' || parts[0] === 'trash' ? parts[1] : undefined)

  function go(e: MouseEvent) {
    shell.nav = false
    link(e)
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && (shell.nav = false)} onfocus={measure} />

{#if shell.nav}<button class="scrim" aria-label={t.close} onclick={() => (shell.nav = false)}></button>{/if}
<nav class="nav" class:open={shell.nav} aria-label={t.navigation} inert={narrow.current && !shell.nav}>
  <div class="brand">{t.brand}</div>
  <ul>
    {#if vols.length}
      <li>
        <a href="/recent" onclick={go} aria-current={parts[0] === 'recent' ? 'page' : undefined}><Clock size={icon.md} /><span>{t.recent}</span></a>
      </li>
      <li>
        <a href="/favorites" onclick={go} aria-current={parts[0] === 'favorites' ? 'page' : undefined}><Star size={icon.md} /><span>{t.favorites}</span></a>
      </li>
    {/if}
    {#each vols as v}
      <li>
        <a href={`/files/${encodeURIComponent(v)}/`} onclick={go} aria-current={parts[0] === 'files' && cur === v ? 'page' : undefined}>
          <HardDrive size={icon.md} /><span>{v}</span>
        </a>
        {#if usage[v]?.total}
          {@const u = usage[v]}
          <div class="usage">
            <div class="usage-bar" style:--p={`${Math.min(100, (100 * u.used) / (u.used + u.free))}%`}></div>
            <span class="hint">{t.usage(size(u.used), size(u.total))}</span>
          </div>
        {/if}
      </li>
    {/each}
  </ul>
  {#if vols.length}
    <ul class="nav-foot">
      <li>
        <a href="/shares" onclick={go} aria-current={parts[0] === 'shares' ? 'page' : undefined}><Share2 size={icon.md} /><span>{t.myShares}</span></a>
      </li>
      <li>
        <a href={`/trash/${encodeURIComponent(cur ?? vols[0])}`} onclick={go} aria-current={parts[0] === 'trash' ? 'page' : undefined}>
          <Trash size={icon.md} /><span>{t.trash}</span>
        </a>
      </li>
      <li>
        <a href="/settings" onclick={go} aria-current={parts[0] === 'settings' ? 'page' : undefined}><Settings size={icon.md} /><span>{t.settings}</span></a>
      </li>
    </ul>
  {/if}
  <button class="nav-item" onclick={onlogout}><LogOut size={icon.md} /><span>{t.logout}</span></button>
</nav>
