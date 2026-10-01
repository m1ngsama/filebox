<script lang="ts">
  import Self from './FolderTree.svelte'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import Folder from '@lucide/svelte/icons/folder'
  import { api, filesURL } from '../lib/api'
  import { link, navigate } from '../lib/router.svelte'
  import { target, inside, sink } from '../lib/dnd'
  import { tree } from '../lib/tree.svelte'
  import { shell } from '../lib/shell.svelte'
  import { t } from '../lib/i18n'

  let { vol, path, depth, here }: { vol: string; path: string; depth: number; here: string } = $props()

  const MAX = 300
  let kids = $state.raw<{ name: string; items?: number }[] | null>(null)
  const id = $derived(`${vol}/${path}`)

  $effect(() => {
    if (!tree.open.has(id)) return
    here
    let live = true
    api.ls(vol, path).then(
      (es) => {
        if (!live) return
        kids = es
          .filter((e) => e.dir && !e.name.startsWith('.'))
          .map((e) => ({ name: e.name, items: e.items }))
          .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }))
        if (!kids.length) tree.bare.add(id)
        else tree.bare.delete(id)
      },
      () => live && (kids = []),
    )
    return () => void (live = false)
  })

  const child = (n: string) => (path ? `${path}/${n}` : n)
  const drop = (to: string) => ({
    accepts: (c: Parameters<typeof inside>[0]) => !!sink.into && !inside(c, { vol, path: to }),
    drop: (c: Parameters<typeof inside>[0], copy: boolean) => sink.into?.({ vol, path: to }, c, copy),
    spring: () => {
      tree.open.add(`${vol}/${to}`)
      navigate(filesURL(vol, to))
    },
    label: to.slice(to.lastIndexOf('/') + 1),
  })

  function key(e: KeyboardEvent, p: string, has: boolean) {
    const k = `${vol}/${p}`
    if (e.key === 'ArrowRight' && has && !tree.open.has(k)) tree.open.add(k)
    else if (e.key === 'ArrowLeft' && tree.open.has(k)) tree.open.delete(k)
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || (e.key === 'ArrowLeft' && !tree.open.has(k))) {
      const all = [...(e.currentTarget as HTMLElement).closest('[role=tree]')!.querySelectorAll<HTMLElement>('[role=treeitem] > .tree-row > a')]
      const i = all.indexOf(e.currentTarget as HTMLElement)
      const to = e.key === 'ArrowDown' ? all[i + 1] : e.key === 'ArrowUp' ? all[i - 1] : (e.currentTarget as HTMLElement).closest('[role=group]')?.closest('[role=treeitem]')?.querySelector<HTMLElement>(':scope > .tree-row > a')
      to?.focus()
    } else return
    e.preventDefault()
  }
</script>

{#if kids}
  <ul role="group" class="tree-group">
    {#each kids.slice(0, MAX) as { name: n, items } (n)}
      {@const p = child(n)}
      {@const k = `${vol}/${p}`}
      {@const open = tree.open.has(k)}
      <li role="treeitem" aria-expanded={items === 0 || tree.bare.has(k) ? undefined : open} aria-selected={here === p} aria-level={depth + 2}>
        <div class="tree-row" style:--depth={depth + 1} use:target={drop(p)}>
          {#if items === 0 || tree.bare.has(k)}<span class="twisty" aria-hidden="true"></span>{:else}<button
              type="button"
              class="twisty"
              class:open
              tabindex="-1"
              aria-hidden="true"
              onclick={() => (open ? tree.open.delete(k) : tree.open.add(k))}><ChevronRight size={14} /></button
            >{/if}
          <a
            href={filesURL(vol, p)}
            aria-current={here === p ? 'page' : undefined}
            onclick={(e) => {
              tree.open.add(k)
              shell.nav = false
              link(e)
            }}
            onkeydown={(e) => key(e, p, items !== 0 && !tree.bare.has(k))}
          >
            <Folder size={16} aria-hidden="true" />
            <span>{n}</span>
          </a>
        </div>
        {#if open}<Self {vol} path={p} depth={depth + 1} {here} />{/if}
      </li>
    {/each}
    {#if kids.length > MAX}<li class="tree-more" style:--depth={depth + 1}>{t.andMore(kids.length - MAX)}</li>{/if}
  </ul>
{/if}
