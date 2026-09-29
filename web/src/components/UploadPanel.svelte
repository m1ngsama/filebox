<script lang="ts">
  import { uploads, cancel, clearDone } from '../lib/uploads.svelte'
  import { size } from '../lib/format'
  import { t } from '../lib/i18n'

  let h = $state(0)
  $effect(() => document.documentElement.style.setProperty('--up-h', `${h}px`))
</script>

{#if uploads.length}
  <aside class="uploads" bind:offsetHeight={h}>
    <header>
      <span>{t.uploads}</span>
      <button onclick={clearDone}>{t.clearDone}</button>
    </header>
    <ul>
      {#each uploads as u (u.id)}
        <li class={u.state}>
          <span class="name" title={u.name}>{u.name}</span>
          <progress max={u.total || 1} value={u.state === 'done' ? u.total || 1 : u.sent}></progress>
          <span class="meta">{u.error ?? `${size(u.sent)} / ${size(u.total)}`}</span>
          {#if u.state === 'queued' || u.state === 'uploading'}
            <button onclick={() => cancel(u)}>{t.cancel}</button>
          {/if}
        </li>
      {/each}
    </ul>
    <p class="hint">{t.resumeHint}</p>
  </aside>
{/if}
