<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy'
  import Check from '@lucide/svelte/icons/check'
  import TextWrap from '@lucide/svelte/icons/text-wrap'
  import Code from '@lucide/svelte/icons/code'
  import FileText from '@lucide/svelte/icons/file-text'
  import Table from '@lucide/svelte/icons/table'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import Pencil from '@lucide/svelte/icons/pencil'
  import type { Entry, Src } from '../lib/api'
  import { look, size } from '../lib/format'
  import { load, save } from '../lib/storage'
  import { copyLater } from '../lib/clipboard'
  import '../lib/render.css'
  import { t } from '../lib/i18n'

  let { entry, url, saveTo, onready, onfail }: { entry: Entry; url: Src; saveTo?: string; onready: () => void; onfail: () => void } = $props()

  const LIMIT = 1 << 20
  const PAGE = 500
  const names: Record<string, string> = {
    go: 'Go', js: 'JavaScript', ts: 'TypeScript', py: 'Python', rs: 'Rust', c: 'C', h: 'C', cpp: 'C++', java: 'Java', kt: 'Kotlin',
    sh: 'Shell', fish: 'Fish', zsh: 'Zsh', json: 'JSON', yaml: 'YAML', yml: 'YAML', toml: 'TOML', xml: 'XML', html: 'HTML', css: 'CSS',
    sql: 'SQL', md: 'Markdown', markdown: 'Markdown', csv: 'CSV', tsv: 'TSV', svelte: 'Svelte', nix: 'Nix',
  }

  const ext = $derived(entry.name.slice(entry.name.lastIndexOf('.') + 1).toLowerCase())
  const md = $derived(ext === 'md' || ext === 'markdown')
  const sheet = $derived(ext === 'csv' || ext === 'tsv')
  const code = $derived(!md && !sheet && look(entry.name) === 'code')
  const shape = $derived(md ? 'md' : sheet ? 'sheet' : code ? 'code' : 'text')

  let raw = $state<string | null>(null)
  let html = $state<string | null>(null)
  let partial = $state(false)
  let plain = $state('')
  let source = $state(false)
  let wrap = $state(false)
  let copied = $state(false)
  let shown = $state(PAGE)
  let order = $state<{ col: number; desc: boolean } | null>(null)
  let more = $state<HTMLElement>()
  let editing = $state(false)
  let fresh = $state(0)
  const editable = $derived(!!saveTo && entry.size <= 8 << 20)

  $effect(() => {
    source = false
    const kept = load('wrap:' + shape)
    wrap = kept === null ? shape !== 'code' || matchMedia('(max-width: 767px)').matches : kept === '1'
    shown = PAGE
    order = null
  })

  function fetchRaw() {
    return fetch(url(entry), { headers: { Range: `bytes=0-${LIMIT - 1}` } }).then((r) => {
      if (!r.ok) throw new Error(String(r.status))
      partial = Number(r.headers.get('Content-Range')?.split('/')[1] ?? 0) > LIMIT
      return r.text()
    })
  }

  $effect(() => {
    fresh
    let stale = false
    raw = html = null
    plain = ''
    const rich = md || code
    const got = rich
      ? fetch(url(entry, 'render')).then((r) => {
          if (!r.ok) throw new Error(String(r.status))
          partial = r.headers.has('X-Truncated')
          plain = r.headers.get('X-Plain') ?? ''
          return r.text()
        })
      : fetchRaw()
    got.then(
      (s) => {
        if (stale) return
        if (rich) html = s
        else raw = s
        onready()
      },
      () => !stale && onfail(),
    )
    return () => {
      stale = true
    }
  })

  $effect(() => {
    if (source && raw === null) fetchRaw().then((s) => (raw = s), onfail)
  })

  const text = (): Promise<string> => (raw !== null ? Promise.resolve(raw) : fetchRaw().then((s) => (raw = s)))

  function copy() {
    copyLater(text()).then((ok) => {
      if (!ok) return
      copied = true
      setTimeout(() => (copied = false), 1500)
    })
  }

  function toggleWrap() {
    wrap = !wrap
    save('wrap:' + shape, wrap ? '1' : '0')
  }

  function parse(s: string, d: string) {
    const rows: string[][] = []
    let row: string[] = []
    let f = ''
    let q = false
    for (let i = s.charCodeAt(0) === 0xfeff ? 1 : 0; i < s.length; i++) {
      const c = s[i]
      if (q) {
        if (c !== '"') f += c
        else if (s[i + 1] === '"') (f += '"'), i++
        else q = false
      } else if (c === '"' && f === '') q = true
      else if (c === d) row.push(f), (f = '')
      else if (c === '\n' || c === '\r') {
        if (c === '\r' && s[i + 1] === '\n') i++
        row.push(f), rows.push(row), (row = []), (f = '')
      } else f += c
    }
    if (f || row.length) row.push(f), rows.push(row)
    return rows
  }

  const table = $derived.by(() => {
    if (!sheet || raw === null) return null
    const first = raw.slice(0, raw.indexOf('\n') >>> 0)
    const d = ext === 'tsv' ? '\t' : first.split(';').length > first.split(',').length ? ';' : ','
    const rows = parse(raw, d)
    if (partial) rows.pop()
    const head = rows.shift() ?? []
    const width = Math.max(head.length, ...rows.slice(0, 200).map((r) => r.length))
    const numeric = Array.from({ length: width }, (_, c) => {
      const vals = rows.slice(0, 200).map((r) => r[c]?.trim() ?? '').filter(Boolean)
      return vals.length > 0 && vals.every((v) => /^[-+]?[\d,]*\.?\d+(e[-+]?\d+)?%?$/i.test(v))
    })
    return { head: Array.from({ length: width }, (_, c) => head[c] ?? ''), rows: rows.map((r, i) => ({ i: i + 1, r })), numeric }
  })

  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
  const sorted = $derived.by(() => {
    if (!table || !order) return table?.rows ?? []
    const { col, desc } = order
    const num = table.numeric[col]
    const val = (r: string[]) => (num ? Number((r[col] ?? '').replace(/[,%]/g, '')) : (r[col] ?? ''))
    return [...table.rows].sort((a, b) => {
      const x = val(a.r)
      const y = val(b.r)
      const c = num ? (x as number) - (y as number) : collator.compare(x as string, y as string)
      return desc ? -c : c
    })
  })

  function sortBy(col: number) {
    order = order?.col !== col ? { col, desc: false } : order.desc ? null : { col, desc: true }
    shown = PAGE
  }

  $effect(() => {
    if (!more) return
    const io = new IntersectionObserver((es) => es[0].isIntersecting && (shown += PAGE), { rootMargin: '600px' })
    io.observe(more)
    return () => io.disconnect()
  })

  const lines = $derived(raw !== null ? raw.split('\n').length - (raw.endsWith('\n') ? 1 : 0) : html !== null && code ? (html.match(/class="line"/g)?.length ?? 0) : 0)
  const facts = $derived(
    [names[ext] ?? (shape === 'text' ? '' : ext.toUpperCase()), table && !source ? t.rows(table.rows.length) : lines && !partial && (code || source || shape === 'text') ? t.lines(lines) : '', size(entry.size)]
      .filter(Boolean)
      .join(' · '),
  )
</script>

{#if editing && saveTo}
  {#await import('./Editor.svelte') then { default: Editor }}
    <Editor
      name={entry.name}
      src={url(entry)}
      save={saveTo}
      bind:wrap
      ondone={(wrote) => {
        editing = false
        if (wrote) fresh++
      }}
    />
  {/await}
{:else}
<div class="text" class:wrap>
  {#if sheet && !source && table}
    <div class="grid-wrap">
      <table class="grid">
        <thead>
          <tr>
            <th class="n" scope="col"></th>
            {#each table.head as h, c (c)}
              <th scope="col" class:figure={table.numeric[c]} aria-sort={order?.col === c ? (order.desc ? 'descending' : 'ascending') : undefined}>
                <button type="button" onclick={() => sortBy(c)} title={t.sortColumn(h)}>
                  <span>{h}</span>
                  {#if order?.col === c}{#if order.desc}<ArrowDown size={14} />{:else}<ArrowUp size={14} />{/if}{/if}
                </button>
              </th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each sorted.slice(0, shown) as row (row.i)}
            <tr>
              <td class="n">{row.i}</td>
              {#each table.head as _, c (c)}<td class:figure={table.numeric[c]}>{row.r[c] ?? ''}</td>{/each}
            </tr>
          {/each}
        </tbody>
      </table>
      {#if shown < sorted.length}<div class="more" bind:this={more}></div>{/if}
    </div>
  {:else if html !== null && !source}
    <article class="doc" class:code={!md} tabindex="-1">{@html html}</article>
  {:else if raw !== null}
    <article class="doc code" tabindex="-1"><pre class="raw">{raw}</pre></article>
  {/if}
  {#if plain || partial}
    <p class="note">{plain ? (plain === 'large' ? t.tooLarge : t.tooComplex) : ''} {partial ? t.truncated : ''}</p>
  {/if}
</div>

{#if html !== null || raw !== null}
  <div class="text-bar">
    <div class="pill">
      <span class="facts">{facts}</span>
      {#if md || sheet}
        <button
          type="button"
          class="icon-btn"
          aria-pressed={source}
          aria-label={source ? (sheet ? t.showTable : t.showRendered) : t.showSource}
          title={source ? (sheet ? t.showTable : t.showRendered) : t.showSource}
          onclick={() => (source = !source)}
        >
          {#if !source}<Code size={18} />{:else if sheet}<Table size={18} />{:else}<FileText size={18} />{/if}
        </button>
      {/if}
      {#if source || shape === 'code' || shape === 'text'}
        <button type="button" class="icon-btn" aria-pressed={wrap} aria-label={t.wrapLines} title={t.wrapLines} onclick={toggleWrap}><TextWrap size={18} /></button>
      {/if}
      <button type="button" class="icon-btn" aria-label={copied ? t.copied : t.copyAll} title={t.copyAll} onclick={copy}>
        {#if copied}<Check size={18} />{:else}<Copy size={18} />{/if}
      </button>
      {#if editable}<button type="button" class="icon-btn" aria-label={t.edit} title={t.edit} onclick={() => (editing = true)}><Pencil size={18} /></button>{/if}
    </div>
  </div>
{/if}
{/if}

<style>
  .text {
    position: absolute;
    inset: 0;
    overflow: auto;
    overscroll-behavior: contain;
    padding: 0 var(--space-4) calc(var(--hit) + var(--space-8));
  }
  .text:has(> .grid-wrap) {
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .note {
    margin: var(--space-3) 0 0;
    text-align: center;
    color: var(--viewer-icon);
    font-size: var(--text-xs);
  }
  .raw {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    background: var(--code-bg);
    color: var(--fg);
    font: var(--text-sm) / 1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    white-space: pre;
    word-break: normal;
    tab-size: 4;
    border-radius: var(--radius-lg);
  }
  .wrap .raw,
  .wrap :global(.doc pre) {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .wrap :global(.doc.code .line) {
    display: block;
    word-break: break-all;
    padding-left: 6ch;
    text-indent: -6ch;
  }
  .grid-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    border-radius: var(--radius-lg);
    background: var(--panel);
    color: var(--fg);
  }
  .grid {
    border-collapse: separate;
    border-spacing: 0;
    font-size: var(--text-sm);
    font-variant-numeric: tabular-nums;
    min-width: 100%;
  }
  .grid th,
  .grid td {
    padding: var(--space-1-5) var(--space-3);
    border-bottom: 1px solid var(--line);
    text-align: start;
    white-space: nowrap;
    max-width: 40ch;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .grid thead th {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 0;
    background: var(--panel);
    box-shadow: inset 0 -1px var(--line);
    font-weight: 600;
  }
  .grid th button {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-1);
    width: 100%;
    min-height: var(--hit);
    padding: 0 var(--space-3);
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .grid th.figure button {
    justify-content: flex-end;
  }
  .grid th button span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .grid .figure {
    text-align: end;
  }
  .grid .n {
    width: 1%;
    position: sticky;
    left: 0;
    color: var(--muted);
    background: var(--panel);
    text-align: end;
    user-select: none;
  }
  .grid thead .n {
    z-index: 2;
  }
  .more {
    height: 1px;
  }
  @media (hover: hover) and (pointer: fine) {
    .grid th button:hover {
      background: var(--hover);
    }
    .grid tbody tr:hover td {
      background: var(--hover);
    }
  }
  .text-bar {
    position: absolute;
    left: 0;
    right: 0;
    bottom: calc(env(safe-area-inset-bottom) + var(--space-3));
    display: flex;
    justify-content: center;
    padding: 0 var(--space-3);
    pointer-events: none;
  }
  .pill {
    pointer-events: auto;
    display: flex;
    align-items: center;
    max-width: 100%;
    min-height: var(--hit);
    padding: 0 var(--space-1) 0 var(--space-4);
    border-radius: var(--radius-full);
    background: color-mix(in srgb, var(--viewer-bg) 82%, transparent);
    color: var(--viewer-fg);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    box-shadow: var(--shadow-sm);
    font-size: var(--text-sm);
    font-variant-numeric: tabular-nums;
    animation: appear var(--dur-2) var(--ease-out);
  }
  .facts {
    padding-inline-end: var(--space-2);
    color: var(--viewer-icon);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pill :global(.icon-btn) {
    color: var(--viewer-fg);
  }
  .pill :global(.icon-btn[aria-pressed='true']) {
    background: var(--viewer-hover);
  }
</style>
