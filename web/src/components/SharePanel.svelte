<script lang="ts">
  import Link from '@lucide/svelte/icons/link'
  import Copy from '@lucide/svelte/icons/copy'
  import Check from '@lucide/svelte/icons/check'
  import Trash from '@lucide/svelte/icons/trash'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { api, type Share } from '../lib/api'
  import { date } from '../lib/format'
  import { t } from '../lib/i18n'

  let { vol, path, dir }: { vol: string; path: string; dir: boolean } = $props()

  let shares = $state<Share[]>([])
  let mode = $state<Share['mode']>('read')
  let password = $state('')
  let expires = $state(7 * 86400)
  let error = $state('')
  let busy = $state(false)
  let copied = $state(0)
  let removing = $state<Share | null>(null)
  const modes = ['read', 'upload', 'drop'] as const
  const url = (s: Share) => `${location.origin}/s/${s.token}`

  async function load() {
    try {
      shares = (await api.shares()).shares.filter((s) => s.vol === vol && s.path === path)
    } catch (e) {
      error = (e as Error).message
    }
  }

  $effect(() => {
    vol
    path
    mode = 'read'
    load()
  })

  async function act(fn: () => Promise<unknown>) {
    busy = true
    error = ''
    try {
      await fn()
    } catch (e) {
      error = (e as Error).message
    }
    busy = false
    await load()
  }

  function create(e: SubmitEvent) {
    e.preventDefault()
    act(async () => {
      await api.newShare({ vol, path, mode, password, expires_in: expires })
      password = ''
    })
  }

  async function copy(s: Share) {
    await navigator.clipboard.writeText(url(s))
    copied = s.id
    setTimeout(() => copied === s.id && (copied = 0), 1500)
  }
</script>

<ul class="shares">
  {#each shares as s (s.id)}
    <li>
      <Link size={16} />
      <div class="share-meta">
        <a href={url(s)} target="_blank" rel="noreferrer">{url(s)}</a>
        <span class="hint">
          {t.modes[s.mode]}{s.has_password ? `，${t.hasPassword}` : ''}，{s.expires ? t.expiresAt(date(s.expires * 1000)) : t.forever}
        </span>
      </div>
      <button class="icon-btn" aria-label={copied === s.id ? t.copied : t.copyLink} title={t.copyLink} onclick={() => copy(s)}>
        {#if copied === s.id}<Check size={16} />{:else}<Copy size={16} />{/if}
      </button>
      <button class="icon-btn danger" aria-label={t.deleteShare} title={t.deleteShare} disabled={busy} onclick={() => (removing = s)}>
        <Trash size={16} />
      </button>
    </li>
  {:else}
    <li class="hint">{t.noShares}</li>
  {/each}
</ul>

<form class="share-form" onsubmit={create}>
  <fieldset>
    <legend>{t.permission}</legend>
    {#each modes as m (m)}
      <label class="radio">
        <input type="radio" name="mode" value={m} bind:group={mode} disabled={m !== 'read' && !dir} />
        {t.modes[m]}
      </label>
    {/each}
  </fieldset>
  <label class="field">
    <span>{t.passwordOptional}</span>
    <input type="password" bind:value={password} autocomplete="new-password" />
  </label>
  <label class="field">
    <span>{t.expiry}</span>
    <select bind:value={expires}>
      {#each t.expiries as [sec, label] (sec)}<option value={sec}>{label}</option>{/each}
    </select>
  </label>
  {#if error}<p class="error">{error}</p>{/if}
  <button class="primary" disabled={busy}>{t.newShare}</button>
</form>

{#if removing}
  {@const id = removing.id}
  <ConfirmDialog
    title={t.deleteShareTitle}
    message={t.deleteShareMessage}
    action={t.remove}
    onconfirm={async () => {
      await api.delShare(id)
      await load()
    }}
    onclose={() => (removing = null)}
  />
{/if}
