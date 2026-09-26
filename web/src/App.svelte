<script lang="ts">
  import { api, session, HttpError, type Me } from './lib/api'
  import { route, navigate } from './lib/router.svelte'
  import { t } from './lib/i18n'
  import Login from './routes/Login.svelte'
  import Browser from './routes/Browser.svelte'
  import Shares from './routes/Shares.svelte'
  import Settings from './routes/Settings.svelte'
  import Trash from './routes/Trash.svelte'
  import SharePage from './routes/SharePage.svelte'
  import UploadPanel from './components/UploadPanel.svelte'
  import Nav from './components/Nav.svelte'
  import NavToggle from './components/NavToggle.svelte'

  let me = $state<Me | null>(null)
  let needLogin = $state(false)
  let error = $state('')
  const parts = $derived(route.path.split('/').filter(Boolean).map(decodeURIComponent))
  const isShare = $derived(parts[0] === 's' && !!parts[1])
  const titles: Record<string, string> = { shares: t.myShares, trash: t.trash, settings: t.settings }

  session.lost = () => {
    me = null
    needLogin = true
  }

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
  <UploadPanel />
{:else if error}
  <div class="load-error">
    <p class="error">{error}</p>
    <button onclick={load}>{t.retry}</button>
  </div>
{:else if needLogin}
  <Login onok={load} />
{:else if me}
  <div class="shell">
    <Nav vols={me.vols} {parts} onlogout={logout} />
    <main class="main">
      {#if me.vols.length && parts[0] === 'files' && parts[1]}
        <Browser vol={parts[1]} path={parts.slice(2).join('/')} vols={me.vols} />
      {:else}
        <header class="bar">
          <NavToggle />
          <h1>{titles[parts[0]] ?? t.brand}</h1>
        </header>
        <div class="page">
          {#if !me.vols.length}
            <p class="error">{t.noVolumes}</p>
          {:else if parts[0] === 'shares'}
            <Shares />
          {:else if parts[0] === 'settings'}
            <Settings vols={me.vols} />
          {:else if parts[0] === 'trash'}
            <Trash vol={parts[1] ?? me.vols[0]} vols={me.vols} />
          {/if}
        </div>
      {/if}
    </main>
  </div>
  <UploadPanel />
{/if}
