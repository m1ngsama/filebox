<script lang="ts">
  import FileIcon from './FileIcon.svelte'
  import { parent, place } from '../lib/format'
  import { t } from '../lib/i18n'
  import type { ContentHit, Progress } from '../lib/api'

  let { hits, indexing, onopen }: { hits: ContentHit[]; indexing: Progress | null; onopen: (h: ContentHit) => void } = $props()
</script>

<section class="content-hits" aria-labelledby="content-hits-title">
  <h2 id="content-hits-title">{t.contentMatches}</h2>
  {#if indexing}<p class="progress" role="status">{t.contentIndexing(indexing.done, indexing.total)}</p>{/if}
  <ul>
    {#each hits as h (`${h.vol}:${h.path}`)}
      <li>
        <button class="hit" onclick={() => onopen(h)} title={place(h.vol, h.path)}>
          <FileIcon name={h.name} dir={false} size={20} />
          <span class="head"><span class="name">{h.name}</span><span class="where">{place(h.vol, parent(h.path), 3)}</span></span>
          <span class="snippet">{#each h.snippet as s, i (i)}{#if i % 2}<mark>{s}</mark>{:else}{s}{/if}{/each}</span>
        </button>
      </li>
    {/each}
  </ul>
</section>

<style>
  .content-hits { padding: var(--space-3) var(--space-2) var(--space-6); border-top: 1px solid var(--line); }
  h2 { padding: 0 var(--space-2); font-size: var(--text-sm); font-weight: 600; color: var(--muted); }
  .progress { margin: var(--space-1) var(--space-2) 0; font-size: var(--text-xs); color: var(--muted); }
  ul { list-style: none; margin: var(--space-2) 0 0; padding: 0; }
  .hit {
    display: grid; grid-template-columns: 28px minmax(0, 1fr); column-gap: var(--space-2); align-items: center;
    width: 100%; padding: var(--space-2); border: 0; border-radius: var(--radius-md); background: none; color: inherit; text-align: left; font: inherit; white-space: normal; cursor: pointer;
  }
  @media (hover: hover) and (pointer: fine) {
    .hit:hover { background: var(--hover); }
  }
  .hit:active { background: var(--hover); }
  .hit > :global(.ficon) { justify-self: center; }
  .head { display: flex; align-items: baseline; gap: var(--space-2); min-width: 0; }
  .name { flex: none; max-width: 70%; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .where { color: var(--muted); font-size: var(--text-xs); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .snippet {
    grid-column: 2; margin-top: 2px; color: var(--muted); font-size: var(--text-sm);
    display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; overflow-wrap: anywhere;
  }
  mark { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--fg); border-radius: 2px; padding: 0 1px; }
</style>
