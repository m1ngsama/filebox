<script lang="ts">
  import { icon } from '../lib/icon'
  import Menu from '@lucide/svelte/icons/menu'
  import { shell } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'
  import { live } from '../lib/live.svelte'
</script>

<button class="icon-btn nav-toggle" aria-label={live.unread ? `${t.openNav} (${t.notifications} ${live.unread})` : t.openNav} onclick={() => (shell.nav = true)}><Menu size={icon.md} />{#if live.unread}<span class="bell-dot" aria-hidden="true"></span>{/if}</button>
