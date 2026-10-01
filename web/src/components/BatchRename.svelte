<script lang="ts">
  import Modal from './Modal.svelte'
  import ArrowRight from '@lucide/svelte/icons/arrow-right'
  import { t } from '../lib/i18n'
  import { ends, stem } from '../lib/format'

  let { names, taken, onsave, onclose }: { names: string[]; taken: Set<string>; onsave: (pairs: [string, string][]) => unknown; onclose: () => void } = $props()

  type Mode = 'replace' | 'add' | 'format'
  let mode = $state<Mode>('replace')
  let find = $state('')
  let swap = $state('')
  let text = $state('')
  let after = $state(true)
  let base = $state('')
  let first = $state(1)
  let error = $state('')
  let busy = $state(false)

  const ext = (n: string) => n.slice(stem(n).length)
  const digits = $derived(Math.max(String(first + names.length - 1).length, names.length >= 10 ? 2 : 1))

  function next(n: string, i: number) {
    if (mode === 'replace') return find ? n.replaceAll(find, swap) : n
    if (mode === 'add') return after ? stem(n) + text + ext(n) : text + n
    return base ? `${base} ${String(first + i).padStart(digits, '0')}${ext(n)}` : n
  }

  const plan = $derived(names.map((n, i) => ({ from: n, to: next(n, i).trim() })))
  const clash = $derived.by(() => {
    const seen = new Map<string, number>()
    for (const p of plan) seen.set(p.to, (seen.get(p.to) ?? 0) + 1)
    const moving = new Set(names)
    return new Set(plan.filter((p) => !p.to || p.to.includes('/') || (seen.get(p.to) ?? 0) > 1 || (p.to !== p.from && taken.has(p.to) && !moving.has(p.to))).map((p) => p.from))
  })
  const changed = $derived(plan.filter((p) => p.to !== p.from))
  const ok = $derived(changed.length > 0 && clash.size === 0)

  async function submit() {
    if (!ok) return
    busy = true
    try {
      await onsave(changed.map((p) => [p.from, p.to]))
      onclose()
    } catch (e) {
      error = (e as Error).message
    }
    busy = false
  }
</script>

<Modal title={t.renameMany(names.length)} {onclose} onsubmit={submit}>
  <div class="modes" role="radiogroup" aria-label={t.renameHow}>
    {#each [['replace', t.renameReplace], ['add', t.renameAdd], ['format', t.renameFormat]] as const as [k, label] (k)}
      <button type="button" role="radio" aria-checked={mode === k} onclick={() => (mode = k)}>{label}</button>
    {/each}
  </div>
  {#if mode === 'replace'}
    <label class="field"><span>{t.renameFind}</span><input bind:value={find} autocomplete="off" /></label>
    <label class="field"><span>{t.renameWith}</span><input bind:value={swap} autocomplete="off" /></label>
  {:else if mode === 'add'}
    <label class="field"><span>{t.renameText}</span><input bind:value={text} autocomplete="off" /></label>
    <div class="modes small" role="radiogroup" aria-label={t.renameWhere}>
      <button type="button" role="radio" aria-checked={!after} onclick={() => (after = false)}>{t.renameBefore}</button>
      <button type="button" role="radio" aria-checked={after} onclick={() => (after = true)}>{t.renameAfter}</button>
    </div>
  {:else}
    <label class="field"><span>{t.renameBase}</span><input bind:value={base} autocomplete="off" placeholder={t.renameBaseHint} /></label>
    <label class="field"><span>{t.renameStart}</span><input type="number" min="0" bind:value={first} /></label>
  {/if}
  <ol class="plan" aria-label={t.renamePreview}>
    {#each plan.slice(0, 50) as p (p.from)}
      <li class:bad={clash.has(p.from)} class:same={p.to === p.from}>
        <span class="old" title={p.from}><span class="head">{ends(p.from)[0]}</span><span>{ends(p.from)[1]}</span></span>
        <ArrowRight size={14} aria-hidden="true" />
        <span class="new" title={p.to}>{p.to || '—'}</span>
      </li>
    {/each}
    {#if plan.length > 50}<li class="more">{t.andMore(plan.length - 50)}</li>{/if}
  </ol>
  {#if clash.size}<p class="error" role="alert">{t.renameClash(clash.size)}</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button class="primary" class:busy disabled={!ok || busy}>{t.renameApply(changed.length)}</button>
  {/snippet}
</Modal>

<style>
  .modes {
    display: flex;
    gap: var(--space-0);
    padding: var(--space-0);
    margin-bottom: var(--space-3);
    border-radius: var(--radius-md);
    background: var(--hover);
  }
  .modes button {
    flex: 1;
    height: 32px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--muted);
    font-size: var(--text-sm);
  }
  .modes button[aria-checked='true'] {
    background: var(--panel);
    color: var(--fg);
    box-shadow: var(--shadow-sm);
    font-weight: 600;
  }
  .plan {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: var(--space-1) 0;
    max-height: 240px;
    overflow: auto;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    font-size: var(--text-sm);
  }
  .plan li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-3);
    color: var(--muted);
  }
  .plan li :global(svg) {
    color: var(--muted);
  }
  .old,
  .new {
    display: flex;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .old span {
    flex: none;
  }
  .old .head {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .new {
    display: block;
    color: var(--fg);
    font-weight: 500;
  }
  .same .new {
    color: var(--muted);
    font-weight: 400;
  }
  .bad .new {
    color: var(--danger);
  }
  .more {
    justify-content: center;
  }
  .error {
    margin: var(--space-2) 0 0;
    color: var(--danger);
    font-size: var(--text-sm);
  }
</style>
