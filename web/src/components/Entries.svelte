<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import type { Entry } from '../lib/api'
  import { size, date } from '../lib/format'

  let {
    entries,
    thumb,
    selected,
    onopen,
    grid = false,
  }: {
    entries: Entry[]
    thumb: (e: Entry) => string | null
    selected?: SvelteSet<string>
    onopen: (e: Entry) => void
    grid?: boolean
  } = $props()

  const broken = new SvelteSet<string>()
  const toggle = (n: string) => (selected!.has(n) ? selected!.delete(n) : selected!.add(n))
</script>

<ul class="entries" class:grid>
  {#each entries as e (e.name)}
    {@const src = e.dir || broken.has(e.name) ? null : thumb(e)}
    <li class:sel={selected?.has(e.name)}>
      {#if selected}
        <input type="checkbox" checked={selected.has(e.name)} onchange={() => toggle(e.name)} aria-label={e.name} />
      {/if}
      <button class="open" onclick={() => onopen(e)}>
        {#if src}
          <img loading="lazy" {src} alt="" onerror={() => broken.add(e.name)} />
        {:else}
          <span class="icon" class:dir={e.dir}></span>
        {/if}
        <span class="name">{e.name}</span>
        <span class="meta">{e.dir ? '' : size(e.size)}</span>
        <span class="meta">{date(e.mtime)}</span>
      </button>
    </li>
  {/each}
</ul>
