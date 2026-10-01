<script lang="ts">
  import { untrack } from 'svelte'
  import { api, session, HttpError, prefetchLs, dropPrefetch, type Me } from './lib/api'
  import { route, navigate } from './lib/router.svelte'
  import { t } from './lib/i18n'
  import { save } from './lib/storage'
  import Browser from './routes/Browser.svelte'
  import Lazy from './components/Lazy.svelte'
  import { uploads, finished } from './lib/uploads.svelte'
  import { playing } from './lib/playing.svelte'
  import Nav from './components/Nav.svelte'
  import NavToggle from './components/NavToggle.svelte'
  import Toasts from './components/Toasts.svelte'
  import EmptyState from './components/EmptyState.svelte'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import HardDrive from '@lucide/svelte/icons/hard-drive'

  let me = $state<Me | null>(null)
  let needLogin = $state(false)
  let error = $state('')
  let shared = $state(new URLSearchParams(location.search).get('share-target'))
  const parts = $derived(route.path.split('/').filter(Boolean).map(decodeURIComponent))
  const titles: Record<string, string> = { recent: t.recent, favorites: t.favorites, shares: t.myShares, trash: t.trash, settings: t.settings }
  const heading = $derived(needLogin ? t.login : parts[0] === 'files' ? parts.slice(1).at(-1) : titles[parts[0]])

  session.lost = () => {
    me = null
    needLogin = true
  }

  async function load() {
    try {
      me = await api.me()
      save('user', me.name)
      needLogin = false
      error = ''
    } catch (e) {
      dropPrefetch()
      if (e instanceof HttpError && e.status === 401) {
        me = null
        needLogin = true
        error = ''
      } else {
        error = t.loadFailed
      }
    }
  }

  function skip(e: MouseEvent) {
    e.preventDefault()
    const to = document.querySelector<HTMLElement>('.main [role=grid] [tabindex="0"]') ?? document.getElementById('main')
    to?.focus()
  }

  async function logout() {
    await api.logout()
    globalThis.caches?.delete('share-target')
    me = null
    needLogin = true
  }

  untrack(() => parts[0] === 'files' && parts[1] && prefetchLs(parts[1], parts.slice(2).join('/')))
  load()

  $effect(() => {
    if (me && me.vols.length && (parts.length === 0 || (parts[0] === 'files' && parts.length === 1)))
      navigate(`/files/${encodeURIComponent(me.vols[0])}/`, true)
  })
</script>

<svelte:head><title>{heading ? `${heading} — ${t.brand}` : t.brand}</title></svelte:head>

{#if error}
  <main class="load-error">
    <EmptyState icon={CloudOff} as="h1" title={t.loadFailedTitle} hint={error}>
      <button class="primary" onclick={load}>{t.retry}</button>
    </EmptyState>
  </main>
{:else if needLogin}
  <main><Lazy load={() => import('./routes/Login.svelte')} onok={load} /></main>
{:else if me}
  <div class="shell">
    <a class="skip" href="#main" onclick={skip}>{parts[0] === 'files' ? t.skipToList : t.skipToMain}</a>
    <Nav vols={me.vols} {parts} onlogout={logout} />
    <main class="main" id="main" tabindex="-1">
      {#if me.vols.length && parts[0] === 'files' && parts[1]}
        <Browser vol={parts[1]} path={parts.slice(2).join('/')} vols={me.vols} />
      {:else}
        <header class="bar">
          <NavToggle />
          <h1>{titles[parts[0]] ?? t.brand}</h1>
        </header>
        {#if me.vols.length && parts[0] === 'recent'}
          <Lazy load={() => import('./routes/Recent.svelte')} />
        {:else if me.vols.length && parts[0] === 'favorites'}
          <Lazy load={() => import('./routes/Favorites.svelte')} />
        {:else}
          <div class="page">
            {#if !me.vols.length}
              <EmptyState icon={HardDrive} as="h2" title={t.noVolumes} hint={t.noVolumesHint} />
            {:else if parts[0] === 'shares'}
              <Lazy load={() => import('./routes/Shares.svelte')} />
            {:else if parts[0] === 'settings'}
              <Lazy load={() => import('./routes/Settings.svelte')} vols={me.vols} origins={me.origins} />
            {:else if parts[0] === 'trash'}
              <Lazy load={() => import('./routes/Trash.svelte')} vol={parts[1] ?? me.vols[0]} vols={me.vols} />
            {/if}
          </div>
        {/if}
      {/if}
    </main>
  </div>
  {#if uploads.length || finished.last}{#await import('./components/UploadPanel.svelte') then { default: UploadPanel }}<UploadPanel />{/await}{/if}
  {#if playing.on}{#await import('./components/MiniPlayer.svelte') then { default: MiniPlayer }}<MiniPlayer />{/await}{/if}
  {#if shared !== null}
    <Lazy load={() => import('./components/ShareTarget.svelte')} vols={me.vols} status={shared} onclose={() => (shared = null)} />
  {/if}
{/if}
<Toasts />
