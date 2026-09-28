<script lang="ts">
  import Clock from '@lucide/svelte/icons/clock'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import Share2 from '@lucide/svelte/icons/share-2'
  import Trash from '@lucide/svelte/icons/trash'
  import Settings from '@lucide/svelte/icons/settings'
  import LogOut from '@lucide/svelte/icons/log-out'
  import { MediaQuery } from 'svelte/reactivity'
  import { link } from '../lib/router.svelte'
  import { shell } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'

  let { vols, parts, onlogout }: { vols: string[]; parts: string[]; onlogout: () => void } = $props()
  const drawer = new MediaQuery('max-width: 767px')
  const cur = $derived(parts[0] === 'files' || parts[0] === 'trash' ? parts[1] : undefined)

  function go(e: MouseEvent) {
    shell.nav = false
    link(e)
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && (shell.nav = false)} />

{#if shell.nav}<button class="scrim" aria-label={t.close} onclick={() => (shell.nav = false)}></button>{/if}
<nav class="nav" class:open={shell.nav} aria-label={t.navigation} inert={drawer.current && !shell.nav}>
  <div class="brand">{t.brand}</div>
  <ul>
    {#if vols.length}
      <li>
        <a href="/recent" onclick={go} aria-current={parts[0] === 'recent' ? 'page' : undefined}><Clock size={18} /><span>{t.recent}</span></a>
      </li>
    {/if}
    {#each vols as v}
      <li>
        <a href={`/files/${encodeURIComponent(v)}/`} onclick={go} aria-current={parts[0] === 'files' && cur === v ? 'page' : undefined}>
          <HardDrive size={18} /><span>{v}</span>
        </a>
      </li>
    {/each}
  </ul>
  {#if vols.length}
    <ul class="nav-foot">
      <li>
        <a href="/shares" onclick={go} aria-current={parts[0] === 'shares' ? 'page' : undefined}><Share2 size={18} /><span>{t.myShares}</span></a>
      </li>
      <li>
        <a href={`/trash/${encodeURIComponent(cur ?? vols[0])}`} onclick={go} aria-current={parts[0] === 'trash' ? 'page' : undefined}>
          <Trash size={18} /><span>{t.trash}</span>
        </a>
      </li>
      <li>
        <a href="/settings" onclick={go} aria-current={parts[0] === 'settings' ? 'page' : undefined}><Settings size={18} /><span>{t.settings}</span></a>
      </li>
    </ul>
  {/if}
  <button class="nav-item" onclick={onlogout}><LogOut size={18} /><span>{t.logout}</span></button>
</nav>
