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

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.login(name.trim(), password)
      save('user', name.trim())
      onok()
    } catch (err) {
      error = err instanceof HttpError && err.status === 429 ? t.tooMany : t.wrongLogin
    }
  }

  async function passkey(autofill = false) {
    try {
      await passkeyLogin(autofill)
      onok()
    } catch (err) {
      const msg = passkeyError(err)
      if (!autofill || msg !== t.passkeyCancelled) error = msg
    }
  }

  $effect(() => {
    let live = true
    api.passkeysEnabled().then(
      async ({ enabled }) => {
        passkeys = enabled && browserSupportsWebAuthn()
        if (passkeys && (await browserSupportsWebAuthnAutofill()) && live) passkey(true)
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
  <input bind:value={name} placeholder={t.username} aria-label={t.username} autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required />
  <input type="password" bind:value={password} placeholder={t.password} aria-label={t.password} autocomplete="current-password" required />
  <button type="submit">{t.login}</button>
  {#if passkeys}<button type="button" onclick={() => passkey()}>{t.passkeyLogin}</button>{/if}
  {#if error}<p class="error">{error}</p>{/if}
</form>
