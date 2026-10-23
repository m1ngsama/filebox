<script lang="ts">
  import { t } from '../lib/i18n'

  let { people }: { people: string[] } = $props()
  const shown = $derived(people.slice(0, 3))
  const label = $derived(t.alsoHere(t.list(people), people.length))
  // A steady color per name, so the same person looks the same everywhere.
  const hue = (s: string) => [...s].reduce((h, c) => (h * 31 + c.codePointAt(0)!) % 360, 7)
</script>

<div class="presence" role="status" aria-label={label} title={label}>
  {#each shown as n (n)}<span class="avatar" style:--h={hue(n)}>{[...n][0]?.toUpperCase()}</span>{/each}
  {#if people.length > shown.length}<span class="avatar more">+{people.length - shown.length}</span>{/if}
</div>
