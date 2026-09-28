<script lang="ts">
  import { browserSupportsWebAuthn, browserSupportsWebAuthnAutofill, WebAuthnAbortService } from '@simplewebauthn/browser'
  import { api, HttpError } from '../lib/api'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'
  import { passkeyLogin, passkeyError } from '../lib/passkey'

  let { onok }: { onok: () => void } = $props()
  let name = $state(load('user') ?? '')
  let password = $state('')
  let error = $state('')
  let passkeys = $state(false)
  let autofill = false
  let live = true
  let since = 0

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
        passkeys = enabled && browserSupportsWebAuthn()
        autofill = passkeys && (await browserSupportsWebAuthnAutofill())
        startAutofill()
      },
      () => {},
    )
    return () => {
      live = false
      WebAuthnAbortService.cancelCeremony()
    }
  })
</script>

<form class="login" onsubmit={submit}>
  <h1>{t.brand}</h1>
  <input bind:value={name} placeholder={t.username} aria-label={t.username} autocomplete="username webauthn" onfocus={refreshAutofill} autocapitalize="none" spellcheck="false" required />
  <input type="password" bind:value={password} placeholder={t.password} aria-label={t.password} autocomplete="current-password" required />
  <button type="submit">{t.login}</button>
  {#if passkeys}<button type="button" onclick={() => passkey()}>{t.passkeyLogin}</button>{/if}
  {#if error}<p class="error">{error}</p>{/if}
</form>
