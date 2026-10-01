<script lang="ts">
  import { icon } from '../lib/icon'
  import Link from '@lucide/svelte/icons/link'
  import ShareRow from './ShareRow.svelte'
  import { api, shareLink, type Share } from '../lib/api'
  import { byLapse } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'
  import { copyLater } from '../lib/clipboard'

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
      shares = byLapse((await api.shares()).shares.filter((s) => s.vol === vol && s.path === path))
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
    const made = api.newShare({ vol, path, mode, password, expires_in: expires })
    const copied = copyLater(made.then((r) => shareLink(r.token)))
    act(async () => {
      const { existing } = await made
      password = ''
      const ok = await copied
      toast(existing ? (ok ? t.shareExisting : t.shareExistingShown) : ok ? t.shareCreatedCopied : t.shareCreated)
    })
  }
</script>

<ul class="shares">
  {#each shares as s (s.id)}
    <li>
      <Link size={icon.sm} />
      <ShareRow share={s} {dir} onchange={load} />
    </li>
  {:else}
    <li class="hint">{t.noShares}</li>
  {/each}
</ul>

<form class="share-form" onsubmit={create}>
  {#if dir}
    <fieldset>
      <legend>{t.permission}</legend>
      {#each modes as m (m)}
        <label class="radio">
          <input type="radio" name="mode" value={m} bind:group={mode} />
          {t.modes[m]}
        </label>
      {/each}
    </fieldset>
  {/if}
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
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="primary" disabled={busy}>{t.newShare}</button>
</form>

