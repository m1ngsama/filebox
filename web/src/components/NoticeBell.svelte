<script lang="ts">
  import { icon } from '../lib/icon'
  import Bell from '@lucide/svelte/icons/bell'
  import { DropdownMenu } from 'bits-ui'
  import { live, onNotice } from '../lib/live.svelte'
  import { noticeText, noticeLink } from '../lib/notices'
  import { navigate } from '../lib/router.svelte'
  import { toast } from '../lib/toast.svelte'
  import { t } from '../lib/i18n'

  let open = $state(false)
  let shown = 0
  // A new notice also shows briefly at the bottom, unless the list is open or one just showed.
  $effect(() =>
    onNotice((n) => {
      if (open || document.visibilityState !== 'visible' || Date.now() - shown < 4000) return
      shown = Date.now()
      toast(noticeText(n), { kind: 'info', actions: [{ label: t.open, run: () => navigate(noticeLink(n)) }] })
    }),
  )
  const label = $derived(live.unread ? `${t.notifications} (${live.unread})` : t.notifications)
</script>

<DropdownMenu.Root bind:open>
  <DropdownMenu.Trigger class="icon-btn bell" aria-label={label} title={label}>
    <Bell size={icon.md} />
    {#if live.unread}<span class="bell-count" aria-hidden="true">{live.unread > 99 ? '99+' : live.unread}</span>{/if}
  </DropdownMenu.Trigger>
  <DropdownMenu.Portal>
    <DropdownMenu.Content class="menu notices-pop" side="bottom" align="start" sideOffset={6} collisionPadding={12} preventScroll={false}>
      {#await import('./NoticeList.svelte') then { default: NoticeList }}<NoticeList onclose={() => (open = false)} />{/await}
    </DropdownMenu.Content>
  </DropdownMenu.Portal>
</DropdownMenu.Root>
