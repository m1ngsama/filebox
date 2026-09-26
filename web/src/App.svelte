<script lang="ts">
  import { api, HttpError, type Me } from './lib/api'
  import { route, navigate, link } from './lib/router.svelte'
  import { t } from './lib/i18n'
  import Login from './routes/Login.svelte'
  import Browser from './routes/Browser.svelte'
  import Shares from './routes/Shares.svelte'
  import Tokens from './routes/Tokens.svelte'
  import Trash from './routes/Trash.svelte'
  import SharePage from './routes/SharePage.svelte'
  import UploadPanel from './components/UploadPanel.svelte'

  let me = $state<Me | null>(null)
  let needLogin = $state(false)
  let error = $state('')
  const parts = $derived(route.path.split('/').filter(Boolean).map(decodeURIComponent))
  const isShare = $derived(parts[0] === 's' && !!parts[1])

  async function load() {
    try {
      me = await api.me()
      needLogin = false
      error = ''
    } catch (e) {
      if (e instanceof HttpError && e.status === 401) {
        me = null
        needLogin = true
        error = ''
      } else {
        error = t.loadFailed
      }
    }
  }

  async function logout() {
    await api.logout()
    me = null
    needLogin = true
  }

  $effect(() => {
    if (!isShare) load()
  })

  $effect(() => {
    if (me && me.vols.length && (parts.length === 0 || (parts[0] === 'files' && parts.length === 1)))
      navigate(`/files/${encodeURIComponent(me.vols[0])}/`, true)
  })
</script>

{#if isShare}
  <SharePage token={parts[1]} />
{:else if error}
  <div class="load-error">
    <p class="error">{error}</p>
    <button onclick={load}>{t.retry}</button>
  </div>
{:else if needLogin}
  <Login onok={load} />
{:else if me}
  <header class="top">
    <a class="brand" href="/" onclick={link}>{t.brand}</a>
    {#each me.vols as v}
      <a href={`/files/${encodeURIComponent(v)}/`} onclick={link} class:active={parts[0] === 'files' && parts[1] === v}>{v}</a>
    {/each}
    <span class="grow"></span>
    {#if me.vols.length}
      <a href="/shares" onclick={link}>{t.shares}</a>
      <a href="/tokens" onclick={link}>{t.tokens}</a>
      <a href={`/trash/${encodeURIComponent(parts[0] === 'files' && parts[1] ? parts[1] : me.vols[0])}`} onclick={link}>{t.trash}</a>
    {/if}
    <button onclick={logout}>{t.logout}</button>
  </header>
  <main>
    {#if !me.vols.length}
      <p class="error">{t.noVolumes}</p>
    {:else if parts[0] === 'files' && parts[1]}
      <Browser vol={parts[1]} path={parts.slice(2).join('/')} />
    {:else if parts[0] === 'shares'}
      <Shares />
    {:else if parts[0] === 'tokens'}
      <Tokens />
    {:else if parts[0] === 'trash'}
      <Trash vol={parts[1] ?? me.vols[0]} vols={me.vols} />
    {/if}
  </main>
  <UploadPanel />
{/if}
