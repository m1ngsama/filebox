<script lang="ts">
  import { api, HttpError } from '../lib/api'
  import { t } from '../lib/i18n'

  let { onok }: { onok: () => void } = $props()
  let password = $state('')
  let error = $state('')

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.login(password)
      onok()
    } catch (err) {
      error = err instanceof HttpError && err.status === 429 ? t.tooMany : t.wrongPassword
    }
  }
</script>

<form class="login" onsubmit={submit}>
  <h1>{t.brand}</h1>
  <input type="password" bind:value={password} placeholder={t.password} autocomplete="current-password" required />
  <button type="submit">{t.login}</button>
  {#if error}<p class="error">{error}</p>{/if}
</form>
