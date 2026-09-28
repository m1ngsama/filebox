<script lang="ts">
  import { api, HttpError } from '../lib/api'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import { passkeyLogin, passkeyError, webauthn } from '../lib/passkey'

  let { onok }: { onok: () => void } = $props()
  let name = $state(load('user') ?? '')
  let password = $state('')
  let error = $state('')
  let passkeys = $state(false)
  let autofill = false
  let live = true
  let since = 0
  let abort = () => {}

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.login(name.trim(), password)
      save('user', name.trim())
      onok()
    } catch (err) {
      error = err instanceof HttpError && err.status === 429 ? err.message : t.wrongLogin
    }
  }

  async function passkey(conditional = false) {
    if (conditional) since = Date.now()
    try {
      await passkeyLogin(conditional)
      onok()
    } catch (err) {
      const msg = passkeyError(err)
      if (conditional && msg === t.passkeyCancelled) return
      error = msg
      if (conditional) since = 0
      else startAutofill()
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

<form class="login" onsubmit={submit}>
  <h1>{t.brand}</h1>
  <label for="login-name" class="sr-only">{t.username}</label>
  <input id="login-name" bind:value={name} placeholder={t.username} autocomplete="username webauthn" onfocus={refreshAutofill} autocapitalize="none" spellcheck="false" required />
  <label for="login-password" class="sr-only">{t.password}</label>
  <input id="login-password" type="password" bind:value={password} placeholder={t.password} autocomplete="current-password" required />
  <button type="submit">{t.login}</button>
  {#if passkeys}<button type="button" onclick={() => passkey()}>{t.passkeyLogin}</button>{/if}
  {#if error}<p class="error">{error}</p>{/if}
</form>
