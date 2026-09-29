<script lang="ts">
  import Download from '@lucide/svelte/icons/download'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import Star from '@lucide/svelte/icons/star'
  import StarOff from '@lucide/svelte/icons/star-off'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import { api, filesURL, rawURL, thumbURL, zipURL, saveURL, type Entry, type Favorite } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { star } from '../lib/favorites.svelte'
  import { fail } from '../lib/toast.svelte'
  import { thumbable, rawThumb, arrange, parent, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'

  let items = $state.raw<Favorite[]>([])
  let loaded = $state(false)
  let error = $state('')
  let sort = $state<Sort>('name')
  let desc = $state(false)

  const shown = $derived(arrange(items, '', sort, desc))
  const fav = (e: Entry) => e as Favorite

  $effect(() => {
    api.favorites().then(
      (r) => {
        items = r.entries
        loaded = true
      },
      (e: Error) => (error = e.message),
    )
  })

  const folder: Action = { id: 'folder', label: t.openFolder, icon: FolderOpen }
  const download: Action = { id: 'download', label: t.download, icon: Download }
  const unstar: Action = { id: 'unstar', label: t.unstar, icon: StarOff, danger: true }

  function open(e: Entry) {
    const f = fav(e)
    if (f.missing) return
    if (f.dir) navigate(filesURL(f.vol, f.path))
    else navigate(`${filesURL(f.vol, parent(f.path))}?select=${encodeURIComponent(f.name)}`)
  }

  function onaction(id: string, e: Entry | null) {
    if (!e) return
    const f = fav(e)
    if (id === 'folder') navigate(`${filesURL(f.vol, parent(f.path))}?select=${encodeURIComponent(f.name)}`)
    else if (id === 'download') saveURL(f.dir ? zipURL(f.vol, [f.path], f.name) : rawURL(f.vol, f.path, true))
    else
      star(f.vol, [f.path], false).then(() => (items = items.filter((x) => x !== f)), fail)
  }
</script>

{#snippet none()}
  <EmptyState icon={Star} title={t.favoritesEmpty} hint={t.favoritesEmptyHint} />
{/snippet}

<section class="files" aria-label={t.favorites}>
  {#if error}<p class="error banner">{error}</p>{/if}
  <EntryList
    entries={shown}
    grid={false}
    bind:sort
    bind:desc
    thumb={(e) => (!e.dir && !fav(e).missing && thumbable(e.name) ? thumbURL(fav(e).vol, fav(e).path) : null)}
    raw={(e) => (!fav(e).missing && rawThumb(e) ? rawURL(fav(e).vol, fav(e).path) : null)}
    actions={(e) => (!e ? [] : fav(e).missing ? [unstar] : [folder, download, unstar])}
    {onaction}
    onopen={open}
    loading={!loaded && !error}
    empty={none}
    id={(e) => `${fav(e).vol}:${fav(e).path}`}
    loc={fav}
    dim={(e) => (fav(e).missing ? t.missing : undefined)}
  />
</section>
