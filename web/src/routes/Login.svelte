<script lang="ts">
  import { api, HttpError } from '../lib/api'
  import { t } from '../lib/i18n'
  import { load, save } from '../lib/storage'

  let { onok }: { onok: () => void } = $props()
  let name = $state(load('user') ?? '')
  let password = $state('')
  let error = $state('')

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
</script>

<form class="login" onsubmit={submit}>
  <h1>{t.brand}</h1>
  <input bind:value={name} placeholder={t.username} aria-label={t.username} autocomplete="username" autocapitalize="none" spellcheck="false" required />
  <input type="password" bind:value={password} placeholder={t.password} aria-label={t.password} autocomplete="current-password" required />
  <button type="submit">{t.login}</button>
  {#if error}<p class="error">{error}</p>{/if}
</form>
