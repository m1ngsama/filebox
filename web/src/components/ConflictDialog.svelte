<script lang="ts" module>
  export type Choice = 'replace' | 'keep' | 'skip'
</script>

<script lang="ts">
  import Modal from './Modal.svelte'
  import { t } from '../lib/i18n'

  let { name, rest, onchoose, onclose }: { name: string; rest: number; onchoose: (c: Choice, all: boolean) => void; onclose: () => void } = $props()
  let all = $state(false)
  let keep = $state<HTMLButtonElement>()
</script>

<Modal
  title={t.conflictTitle}
  {onclose}
  onsubmit={() => onchoose('keep', all)}
  onOpenAutoFocus={(e) => {
    e.preventDefault()
    keep?.focus()
  }}
>
  <p>{t.conflict(name)}</p>
  {#if rest}
    <label class="check"><input type="checkbox" bind:checked={all} />{t.applyToRest(rest)}</label>
  {/if}
  {#snippet footer()}
    <button type="button" onclick={() => onchoose('skip', all)}>{t.skip}</button>
    <button type="button" onclick={() => onchoose('replace', all)}>{t.replace}</button>
    <button class="primary" bind:this={keep}>{t.keepBoth}</button>
  {/snippet}
</Modal>
