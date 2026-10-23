<script lang="ts">
  import { icon } from '../lib/icon'
  import Download from '@lucide/svelte/icons/download'
  import Upload from '@lucide/svelte/icons/upload'
  import UserPlus from '@lucide/svelte/icons/user-plus'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import Bell from '@lucide/svelte/icons/bell'
  import { api, type Notice } from '../lib/api'
  import { live, onNotice } from '../lib/live.svelte'
  import { noticeText, noticeLink } from '../lib/notices'
  import { navigate } from '../lib/router.svelte'
  import { ago, date } from '../lib/format'
  import { t } from '../lib/i18n'
  import { fail } from '../lib/toast.svelte'
  import { DropdownMenu } from 'bits-ui'

  let { onclose }: { onclose: () => void } = $props()
  let items = $state<Notice[] | null>(null)
  let more = $state(false)
  const icons: Record<string, typeof Bell> = { share_download: Download, share_upload: Upload, joined: UserPlus, disk_low: HardDrive }

  async function load() {
    try {
      const r = await api.notices()
      items = r.notices
      more = r.more
      if (r.unread) {
        live.unread = 0
        await api.readNotices()
      }
    } catch (e) {
      fail(e)
      items = []
    }
  }
  $effect(() => void load())
  $effect(() => onNotice((n) => (items = [n, ...(items ?? [])])))

  async function older() {
    const last = items?.at(-1)
    if (!last) return
    const r = await api.notices(last.id).catch((e) => (fail(e), null))
    if (!r) return
    items = [...(items ?? []), ...r.notices]
    more = r.more
  }

  function open(n: Notice) {
    onclose()
    navigate(noticeLink(n))
  }
</script>

<div class="notices">
  <div class="notices-title" role="presentation">{t.notifications}</div>
  {#if items === null}
    <p class="hint notices-empty" role="status">{t.loading}</p>
  {:else if !items.length}
    <div class="notices-empty">
      <Bell size={icon.lg} />
      <p>{t.noticesEmpty}</p>
      <p class="hint">{t.noticesEmptyHint}</p>
    </div>
  {:else}
    <ul>
      {#each items as n (n.id)}
        {@const Icon = icons[n.kind] ?? Bell}
        <li>
          <DropdownMenu.Item class={n.read ? 'notice' : 'notice unread'} onSelect={() => open(n)}>
            <Icon size={icon.md} />
            <span class="notice-text">{noticeText(n)}</span>
            <span class="hint" title={date(n.at * 1000)}>{ago(n.at * 1000)}</span>
          </DropdownMenu.Item>
        </li>
      {/each}
    </ul>
    {#if more}<DropdownMenu.Item class="menu-item notices-more" closeOnSelect={false} onSelect={older}>{t.more}</DropdownMenu.Item>{/if}
  {/if}
</div>
