<script lang="ts">
  import { onMount, tick } from 'svelte'
  import Eye from '@lucide/svelte/icons/eye'
  import EyeOff from '@lucide/svelte/icons/eye-off'
  import Fingerprint from '@lucide/svelte/icons/fingerprint'
  import { api, HttpError } from '../lib/api'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import { passkeyLogin, passkeyError, webauthn } from '../lib/passkey'

  let { onok }: { onok: () => void } = $props()
  let name = $state(load('user') ?? '')
  let password = $state('')
  let error = $state('')
  let passkeys = $state(false)
  let show = $state(false)
  let busy = $state(false)
  let single = $state(true)
  let user = $state<HTMLInputElement>()
  let pass = $state<HTMLInputElement>()
  let autofill = false
  let live = true
  let since = 0
  let abort = () => {}

  onMount(() => {
    pass?.focus()
    api.loginInfo().then(
      (r) => {
        single = r.single
        if (!single && !name && !password && document.activeElement === pass) tick().then(() => user?.focus())
      },
      () => (single = false),
    )
  })

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    try {
      await api.login(single ? '' : name.trim(), password)
      if (!single) save('user', name.trim())
      onok()
    } catch (err) {
      error = err instanceof HttpError && err.status === 429 ? err.message : t.wrongLogin
      await tick()
      pass?.select()
    }
    busy = false
  }

  async function passkey(conditional = false) {
    if (conditional) since = Date.now()
    try {
      await passkeyLogin(conditional)
      onok()
    } catch (err) {
      if (conditional) {
        since = 0
        return
      }
      error = passkeyError(err)
      startAutofill()
    }
  }

  function startAutofill() {
    if (autofill && live) passkey(true)
  }

  function refreshAutofill() {
    if (Date.now() - since > 4 * 60_000) startAutofill()
  }

  $effect(() => {
    api.passkeysEnabled().then(
      async ({ enabled }) => {
        if (!enabled || !live) return
        const w = await webauthn()
        abort = () => w.WebAuthnAbortService.cancelCeremony()
        passkeys = w.browserSupportsWebAuthn()
        autofill = passkeys && (await w.browserSupportsWebAuthnAutofill())
        startAutofill()
      },
      () => {},
    )
    return () => {
      live = false
      abort()
    }
  })
</script>

<form class="login" onsubmit={submit} aria-labelledby="login-title">
  <header class="login-brand">
    <img src="/icon.svg" alt="" width="48" height="48" />
    <h1 id="login-title">{t.brand}</h1>
    <p class="hint">{t.loginHint}</p>
  </header>
  <label for="login-name" class="sr-only">{t.username}</label>
  <input
    id="login-name"
    class:sr-only={single}
    bind:this={user}
    bind:value={name}
    placeholder={t.username}
    autocomplete={single ? 'username' : 'username webauthn'}
    onfocus={refreshAutofill}
    autocapitalize="none"
    spellcheck="false"
    tabindex={single ? -1 : undefined}
    aria-hidden={single || undefined}
    required={!single}
  />
  <div class="password">
    <label for="login-password" class="sr-only">{t.password}</label>
    <input
      id="login-password"
      bind:this={pass}
      type={show ? 'text' : 'password'}
      bind:value={password}
      placeholder={t.password}
      autocomplete={single ? 'current-password webauthn' : 'current-password'}
      onfocus={refreshAutofill}
      aria-invalid={!!error}
      aria-describedby={error ? 'login-error' : undefined}
      required
    />
    <button type="button" class="icon-btn" aria-label={t.showPassword} aria-pressed={show} onclick={() => (show = !show)}>
      {#if show}<EyeOff size={18} />{:else}<Eye size={18} />{/if}
    </button>
  </div>
  {#if error}<p class="error" id="login-error" role="alert">{error}</p>{/if}
  <button type="submit" class="primary" disabled={busy}>{t.login}</button>
  {#if passkeys}
    <div class="login-or"><span>{t.or}</span></div>
    <button type="button" onclick={() => passkey()}><Fingerprint size={18} />{t.passkeyLogin}</button>
  {/if}
</form>
