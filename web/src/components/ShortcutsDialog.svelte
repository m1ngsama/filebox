<script lang="ts">
  import Modal from './Modal.svelte'
  import { t } from '../lib/i18n'

  let { onclose }: { onclose: () => void } = $props()
  const platform = (navigator as Navigator & { userAgentData?: { platform: string } }).userAgentData?.platform ?? navigator.platform
  const apple = /mac|ip/i.test(platform)
  const mod = apple ? '⌘' : 'Ctrl'
  const k = t.keys
  const rows: [string[][], string][] = [
    [[['/']], k.filter],
    [[['Enter']], k.first],
    [[['N']], t.newFolder],
    [[['U']], t.upload],
    [[['F2']], k.rename],
    [[[mod, 'A']], t.selectAll],
    [[['Delete'], ['Backspace']], k.remove],
    [[['Enter']], k.open],
    [[['↑'], ['↓']], k.move],
    [[['Space']], k.toggle],
    [[['Esc']], k.escape],
    [[['←'], ['→']], k.step],
    [[[mod, 'Z']], k.undo],
    [[[apple ? '⌥' : 'Alt', k.drag]], k.dragCopy],
    [[['?']], k.help],
  ]
</script>

<Modal title={t.shortcuts} {onclose} onsubmit={onclose}>
  <dl class="keys">
    {#each rows as [combos, label] (label)}
      <dt>
        {#each combos as combo, i (i)}
          {#if i}<span class="hint">/</span>{/if}
          {#each combo as key, j (j)}{#if j}+{/if}<kbd>{key}</kbd>{/each}
        {/each}
      </dt>
      <dd>{label}</dd>
    {/each}
  </dl>
  {#snippet footer()}<button class="primary">{t.close}</button>{/snippet}
</Modal>
