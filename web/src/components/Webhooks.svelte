<script lang="ts">
  import type { Snippet } from 'svelte'
  import { icon } from '../lib/icon'
  import Bell from '@lucide/svelte/icons/bell'
  import RowList from './RowList.svelte'
  import EmptyState from './EmptyState.svelte'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { api, type Webhook } from '../lib/api'
  import { ago, date } from '../lib/format'
  import { t, lang } from '../lib/i18n'
  import { toast, fail } from '../lib/toast.svelte'

  let { head }: { head: (id: string, title: string, hint: string, action?: Snippet) => ReturnType<Snippet> } = $props()

  const fallback = ['upload', 'download', 'login_failed', 'disk_low']
  let hooks = $state<Webhook[] | null>(null)
  let kinds = $state<string[]>(fallback)
  let max = $state(10)
  let loadError = $state('')
  let url = $state('')
  let format = $state('json')
  let secret = $state('')
  let chosen = $state<string[]>(fallback)
  let error = $state('')
  let busy = $state(false)
  let removing = $state<Webhook | null>(null)
  let testing = $state(0)

  async function load() {
    try {
      const r = await api.webhooks()
      hooks = r.webhooks
      kinds = r.kinds
      max = r.max
      loadError = ''
    } catch (e) {
      loadError = (e as Error).message
    }
  }
  $effect(() => void load())

  async function add(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      await api.newWebhook({ url: url.trim(), format, kinds: chosen, secret, lang })
      url = secret = ''
      await load()
    } catch (err) {
      error = (err as Error).message
    }
    busy = false
  }

  async function test(h: Webhook) {
    testing = h.id
    try {
      await api.testWebhook(h.id)
      toast(t.webhookTested)
    } catch (e) {
      fail(e)
    }
    testing = 0
    await load()
  }

  const toggle = (k: string, on: boolean) => (chosen = on ? [...chosen, k] : chosen.filter((x) => x !== k))
</script>

<section class="card-section" id="notifications" aria-labelledby="notifications-title">
  {@render head('notifications', t.notifications, t.webhooksHint)}
  {#if hooks && hooks.length < max}
    <form class="webhook-form" onsubmit={add}>
      <label class="field">
        <span>{t.webhookURL}</span>
        <input type="url" bind:value={url} placeholder="https://ntfy.example.com/filebox" required maxlength="2048" autocomplete="off" />
      </label>
      <div class="webhook-row">
        <label class="field">
          <span>{t.webhookFormat}</span>
          <select bind:value={format}>
            {#each Object.entries(t.webhookFormats) as [k, v] (k)}<option value={k}>{v}</option>{/each}
          </select>
        </label>
        <label class="field grow">
          <span>{t.webhookSecret}</span>
          <input type="password" bind:value={secret} maxlength="200" autocomplete="new-password" />
        </label>
      </div>
      <p class="hint">{t.webhookSecretHint}</p>
      <fieldset class="webhook-kinds">
        <legend>{t.webhookKinds}</legend>
        {#each kinds as k (k)}
          <label class="check"><input type="checkbox" checked={chosen.includes(k)} onchange={(e) => toggle(k, e.currentTarget.checked)} />{t.webhookKindNames[k] ?? k}</label>
        {/each}
      </fieldset>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div><button class="primary" disabled={busy || !url.trim() || !chosen.length}>{t.addWebhook}</button></div>
    </form>
  {:else if hooks}
    <p class="hint">{t.webhookLimit(max)}</p>
  {/if}

  <RowList items={hooks} error={loadError} onretry={load} key={(h) => h.id}>
    {#snippet row(h)}
      <Bell size={icon.md} class="row-icon" />
      <div class="row-main">
        <span class="row-title" title={h.url}>{h.url}</span>
        <span class="tags">
          {#each h.kinds as k (k)}<span class="tag">{t.webhookKindNames[k] ?? k}</span>{/each}
          {#if h.signed}<span class="tag">{t.webhookSigned}</span>{/if}
        </span>
        <span class="hint" title={h.last_at ? date(h.last_at * 1000) : undefined}>{h.last_at ? t.webhookStatus(h.last_status, ago(h.last_at * 1000)) : t.webhookNever}</span>
      </div>
      <button disabled={testing === h.id} onclick={() => test(h)}>{t.testWebhook}</button>
      <button class="quiet" onclick={() => (removing = h)}>{t.remove}</button>
    {/snippet}
    {#snippet empty()}<EmptyState compact icon={Bell} title={t.noWebhooks} />{/snippet}
  </RowList>
</section>

{#if removing}
  {@const h = removing}
  <ConfirmDialog
    title={t.deleteWebhookTitle}
    message={t.deleteWebhookMessage(h.url)}
    action={t.remove}
    onconfirm={async () => {
      await api.delWebhook(h.id)
      toast(t.webhookRemoved)
      await load()
    }}
    onclose={() => (removing = null)}
  />
{/if}
