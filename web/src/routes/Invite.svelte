<script lang="ts">
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import EmptyState from '../components/EmptyState.svelte'
  import { api, HttpError, type InviteInfo } from '../lib/api'
  import { t } from '../lib/i18n'

  let { token, onok }: { token: string; onok: () => void } = $props()

  let info = $state<InviteInfo | null>(null)
  let gone = $state(false)
  let failed = $state('')
  let name = $state('')
  let password = $state('')
  let again = $state('')
  let error = $state('')
  let busy = $state(false)

  async function load() {
    failed = ''
    try {
      info = await api.inviteInfo(token)
    } catch (e) {
      if (e instanceof HttpError && e.status === 404) gone = true
      else failed = (e as Error).message
    }
  }
  $effect(() => void load())

  const join = $derived(info?.kind === 'join')
  const mismatch = $derived(!!again && again !== password)
  const ok = $derived([...password].length >= 8 && password === again && (!join || !!name.trim()))
  const vols = $derived(Object.entries(info?.access ?? {}).sort())

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (!ok) return
    busy = true
    error = ''
    try {
      await api.useInvite(token, name.trim(), password)
      onok()
    } catch (err) {
      error = err instanceof HttpError && err.status === 409 ? t.nameTaken : err instanceof HttpError && err.status === 404 ? t.invalidInvite : (err as Error).message
      if (err instanceof HttpError && err.status === 404) gone = true
    }
    busy = false
  }
</script>

{#if gone}
  <div class="login">
    <EmptyState icon={CloudOff} as="h1" title={t.invalidInvite} hint={t.invalidInviteHint} />
  </div>
{:else if failed}
  <div class="login">
    <EmptyState icon={CloudOff} as="h1" title={t.loadFailedTitle} hint={failed}>
      <button class="primary" onclick={load}>{t.retry}</button>
    </EmptyState>
  </div>
{:else if info}
  <form class="login" onsubmit={submit} aria-labelledby="invite-title">
    <header class="login-brand">
      <img src="/icon.svg" alt="" width="48" height="48" />
      <h1 id="invite-title">{join ? t.inviteTitle : t.inviteResetTitle(info.user)}</h1>
      {#if join}<p class="hint">{t.inviteHint(info.role === 'admin' ? t.roleAdmin : t.roleMember)}</p>{/if}
    </header>
    {#if join && info.role !== 'admin' && vols.length}
      <p class="hint invite-vols">{t.inviteVolumes} {vols.map(([v, l]) => `${v} (${t.accessLevels[l]})`).join(', ')}</p>
    {/if}
    {#if join}
      <label for="invite-name" class="sr-only">{t.chooseUsername}</label>
      <input id="invite-name" bind:value={name} placeholder={t.chooseUsername} autocomplete="username" autocapitalize="none" spellcheck="false" maxlength="32" required />
    {/if}
    <label for="invite-password" class="sr-only">{t.password}</label>
    <input id="invite-password" type="password" bind:value={password} placeholder={t.password} autocomplete="new-password" minlength="8" maxlength="72" required />
    <small class="hint">{t.passwordRule}</small>
    <label for="invite-again" class="sr-only">{t.confirmPassword}</label>
    <input id="invite-again" type="password" bind:value={again} placeholder={t.confirmPassword} autocomplete="new-password" required aria-invalid={mismatch} />
    {#if mismatch}<p class="error" role="alert">{t.passwordMismatch}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button class="primary" class:busy disabled={busy || !ok}>{join ? t.joinAction : t.setPasswordAction}</button>
  </form>
{:else}
  <div class="login" role="status" aria-label={t.loading}></div>
{/if}
