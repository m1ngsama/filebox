<script lang="ts">
  import { icon } from '../lib/icon'
  import Send from '@lucide/svelte/icons/send'
  import Trash from '@lucide/svelte/icons/trash'
  import MessageSquare from '@lucide/svelte/icons/message-square'
  import { api, type Comment } from '../lib/api'
  import { onComment } from '../lib/live.svelte'
  import { ago, date } from '../lib/format'
  import { t } from '../lib/i18n'
  import { fail } from '../lib/toast.svelte'

  let { vol, path }: { vol: string; path: string } = $props()
  let items = $state<Comment[] | null>(null)
  let text = $state('')
  let busy = $state(false)
  let box = $state<HTMLTextAreaElement>()
  let list = $state<HTMLElement>()
  let people = $state<string[]>([])
  let pick = $state(0)
  const mod = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'

  async function load() {
    try {
      items = (await api.comments(vol, path)).comments
    } catch (e) {
      fail(e)
      items = []
    }
  }
  $effect(() => {
    vol
    path
    load()
  })
  $effect(() => onComment((v, p) => v === vol && p === path && load()))
  $effect(() => void api.people(vol).then((r) => (people = r.people), () => {}))
  $effect(() => {
    items?.length
    queueMicrotask(() => list?.lastElementChild?.scrollIntoView({ block: 'nearest' }))
  })

  // The word being typed when it starts with @, and the members whose names start the same way.
  const typing = $derived.by(() => {
    const at = box?.selectionStart ?? text.length
    const m = /(?:^|\s)@([^\s@]*)$/.exec(text.slice(0, at))
    return m ? m[1].toLowerCase() : null
  })
  const matches = $derived(typing === null ? [] : people.filter((n) => n.toLowerCase().startsWith(typing)).slice(0, 6))

  function complete(name: string) {
    const at = box?.selectionStart ?? text.length
    const before = text.slice(0, at).replace(/@([^\s@]*)$/, `@${name} `)
    text = before + text.slice(at)
    queueMicrotask(() => box?.setSelectionRange(before.length, before.length))
    box?.focus()
  }

  async function send() {
    const body = text.trim()
    if (!body || busy) return
    busy = true
    try {
      const c = await api.addComment(vol, path, body)
      items = [...(items ?? []).filter((x) => x.id !== c.id), c]
      text = ''
    } catch (e) {
      fail(e)
    }
    busy = false
  }

  async function remove(c: Comment) {
    try {
      await api.delComment(c.id)
      items = (items ?? []).filter((x) => x.id !== c.id)
    } catch (e) {
      fail(e)
    }
  }

  function key(e: KeyboardEvent) {
    if (matches.length && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault()
      pick = (pick + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length
    } else if (matches.length && (e.key === 'Enter' || e.key === 'Tab') && !e.metaKey && !e.ctrlKey && !e.isComposing) {
      e.preventDefault()
      complete(matches[Math.min(pick, matches.length - 1)])
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.isComposing) {
      e.preventDefault()
      send()
    }
  }

  // Split a comment into plain text and @names, rendered as text, never as markup.
  const parts = (body: string) => body.split(/(@[^\s@,.;:!?\uff0c\u3002\uff1b\uff1a\uff01\uff1f\u3001]+)/)
</script>

<div class="comments">
  {#if items === null}
    <p class="hint" role="status">{t.loading}</p>
  {:else if !items.length}
    <div class="comments-empty">
      <MessageSquare size={icon.lg} />
      <p>{t.noComments}</p>
      <p class="hint">{t.noCommentsHint}</p>
    </div>
  {:else}
    <ul bind:this={list} aria-label={t.comments}>
      {#each items as c (c.id)}
        <li class:mine={c.mine}>
          <div class="comment-head">
            <b>{c.author || t.formerMember}</b>
            <span class="hint" title={date(c.at * 1000)}>{ago(c.at * 1000)}</span>
            {#if c.removable}<button class="icon-btn" aria-label={t.deleteComment} title={t.deleteComment} onclick={() => remove(c)}><Trash size={icon.sm} /></button>{/if}
          </div>
          <p class="comment-body">{#each parts(c.body) as s, i (i)}{#if i % 2}<span class="mention">{s}</span>{:else}{s}{/if}{/each}</p>
        </li>
      {/each}
    </ul>
  {/if}
  <form class="composer" onsubmit={(e) => (e.preventDefault(), send())}>
    {#if matches.length}
      <ul class="menu mentions" role="listbox" aria-label={t.commentPlaceholder}>
        {#each matches as n, i (n)}
          <li role="option" aria-selected={i === pick} class="menu-item" data-highlighted={i === pick ? '' : undefined} onpointerdown={(e) => (e.preventDefault(), complete(n))}>@{n}</li>
        {/each}
      </ul>
    {/if}
    <label class="sr-only" for="comment-text">{t.comments}</label>
    <textarea id="comment-text" bind:this={box} bind:value={text} rows="2" maxlength="2000" placeholder={t.commentPlaceholder} onkeydown={key} oninput={() => (pick = 0)}></textarea>
    <div class="composer-foot">
      <span class="hint">{t.commentKeys(mod)}</span>
      <button class="primary" disabled={busy || !text.trim()} aria-label={t.sendComment}><Send size={icon.sm} />{t.sendComment}</button>
    </div>
  </form>
</div>
