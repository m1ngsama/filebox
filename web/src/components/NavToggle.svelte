<script lang="ts">
  import Menu from '@lucide/svelte/icons/menu'
  import { shell } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'
</script>

<button class="icon-btn nav-toggle" aria-label={t.openNav} onclick={() => (shell.nav = true)}><Menu size={20} /></button>
