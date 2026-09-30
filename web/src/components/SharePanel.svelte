<script lang="ts">
  import { icon } from '../lib/icon'
  import Link from '@lucide/svelte/icons/link'
  import ShareRow from './ShareRow.svelte'
  import { api, shareLink, type Share } from '../lib/api'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'

  let { vol, path, dir }: { vol: string; path: string; dir: boolean } = $props()

  let shares = $state<Share[]>([])
  let mode = $state<Share['mode']>('read')
  let password = $state('')
  let expires = $state(7 * 86400)
  let error = $state('')
  let busy = $state(false)
  const modes = ['read', 'upload', 'drop'] as const

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
      const { token, existing } = await api.newShare({ vol, path, mode, password, expires_in: expires })
      password = ''
      const copied = await navigator.clipboard.writeText(shareLink(token)).then(() => true, () => false)
      toast(existing ? (copied ? t.shareExisting : t.shareExistingShown) : copied ? t.shareCreatedCopied : t.shareCreated)
    })
  }
</script>

<ul class="shares">
  {#each shares as s (s.id)}
    <li>
      <Link size={icon.sm} />
      <ShareRow share={s} {dir} onchange={load}>
        <a href={shareLink(s.token)} target="_blank" rel="noreferrer">{shareLink(s.token)}</a>
      </ShareRow>
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

