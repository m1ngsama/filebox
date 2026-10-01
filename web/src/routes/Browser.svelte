<script lang="ts" module>
  import type { Entry as Row } from '../lib/api'
  import { t as tr } from '../lib/i18n'

  type Op = { key: string; name: string; entry: Row | null; settled: number }
  let ops: Op[] = []
  let clock = 0
  const live = { refresh: () => {} }

  type Job = { paths: string[]; ok: Promise<boolean> }
  let jobs: Job[] = []
  const overlaps = (a: string, b: string) => a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`)

  function queue<T>(paths: string[], f: (signal: AbortSignal) => Promise<T>) {
    const deps = jobs.filter((j) => j.paths.some((p) => paths.some((q) => overlaps(p, q))))
    const run = Promise.all(deps.map((d) => d.ok)).then((oks) => {
      if (oks.includes(false)) throw new Error(tr.dependsFailed)
      return f(AbortSignal.timeout(30_000))
    })
    const job = { paths, ok: run.then(() => true, () => false) }
    jobs.push(job)
    job.ok.then(() => (jobs = jobs.filter((j) => j !== job)))
    return run
  }
</script>

<script lang="ts">
  import { icon } from '../lib/icon'
  import { onMount, tick, untrack } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { DropdownMenu } from 'bits-ui'
  import Plus from '@lucide/svelte/icons/plus'
  import Upload from '@lucide/svelte/icons/upload'
  import FolderUp from '@lucide/svelte/icons/folder-up'
  import FolderPlus from '@lucide/svelte/icons/folder-plus'
  import FolderOpen from '@lucide/svelte/icons/folder-open'
  import Download from '@lucide/svelte/icons/download'
  import Pencil from '@lucide/svelte/icons/pencil'
  import FolderInput from '@lucide/svelte/icons/folder-input'
  import Link from '@lucide/svelte/icons/link'
  import Star from '@lucide/svelte/icons/star'
  import StarOff from '@lucide/svelte/icons/star-off'
  import Info from '@lucide/svelte/icons/info'
  import Trash from '@lucide/svelte/icons/trash'
  import X from '@lucide/svelte/icons/x'
  import LayoutGrid from '@lucide/svelte/icons/layout-grid'
  import List from '@lucide/svelte/icons/list'
  import ChevronRight from '@lucide/svelte/icons/chevron-right'
  import SearchX from '@lucide/svelte/icons/search-x'
  import SquareCheck from '@lucide/svelte/icons/square-check'
  import FolderX from '@lucide/svelte/icons/folder-x'
  import FolderLock from '@lucide/svelte/icons/folder-lock'
  import HardDrive from '@lucide/svelte/icons/hard-drive'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import { narrow } from '../lib/shell.svelte'
  import Search from '@lucide/svelte/icons/search'
  import ChevronDown from '@lucide/svelte/icons/chevron-down'
  import ChevronLeft from '@lucide/svelte/icons/chevron-left'
  import Ellipsis from '@lucide/svelte/icons/ellipsis'
  import EllipsisVertical from '@lucide/svelte/icons/ellipsis-vertical'
  import ArrowUp from '@lucide/svelte/icons/arrow-up'
  import ArrowDown from '@lucide/svelte/icons/arrow-down'
  import Check from '@lucide/svelte/icons/check'
  import { api, HttpError, errorText, filesURL, fileURL, rawURL, thumbURL, zipURL, saveURL, selectURL, shareLink, type Entry, type Loc, type Move, type RecentFile, type ContentHit, type Progress } from '../lib/api'
  import { copyLater } from '../lib/clipboard'
  import { toast, fail, runLatest, retract, retext, forgetUndo, type ToastAction } from '../lib/toast.svelte'
  import { navigate, link, route } from '../lib/router.svelte'
  import { enqueue, type Replaced } from '../lib/uploads.svelte'
  import { loadStars, starred, star } from '../lib/favorites.svelte'
  import { folderAction, downloadAction, actOn, saveZip } from '../lib/located'
  import { kind, thumbable, rawThumb, arrange, parent, base, child, flip, sorts, place, prefersGrid, mostlyMedia, dated, days, sidecars, subtitleRename, stem, type Sort } from '../lib/format'
  import { t } from '../lib/i18n'
  import { load, save, viewOf, keepView, type View } from '../lib/storage'
  import NavToggle from '../components/NavToggle.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import EntryList, { type Action } from '../components/EntryList.svelte'
  import ContentHits from '../components/ContentHits.svelte'
  import type { Choice } from '../components/ConflictDialog.svelte'
  import { target, inside, sink, type Carried, type Target } from '../lib/dnd'

  let { vol, path, vols }: { vol: string; path: string; vols: string[] } = $props()

  type Dialog = { kind: 'mkdir' } | { kind: 'rename'; e: Entry } | { kind: 'renameMany'; names: string[] } | { kind: 'move'; names: string[] }

  let collapsed = $state(false)
  let entries = $state.raw<Entry[]>([])
  let error = $state<{ status: number; message: string } | null>(null)
  let up = $state<string | null>(null)
  let at = $state('')
  let streaming = $state(false)
  let announce = $state('')
  let loading: AbortController | undefined
  let filter = $state('')
  let scope = $state<'here' | 'all'>('here')
  let kindF = $state('')
  let whenF = $state('')
  let sizeF = $state('')
  const filtering = $derived(scope === 'all' && !!(kindF || whenF || sizeF))

  function since(w: string) {
    const d = new Date()
    if (w === 'day') d.setHours(0, 0, 0, 0)
    else if (w === 'year') d.setMonth(0, 1), d.setHours(0, 0, 0, 0)
    else d.setDate(d.getDate() - (w === 'week' ? 7 : 30))
    return String(d.getTime())
  }
  let hits = $state.raw<RecentFile[] | null>(null)
  let content = $state.raw<ContentHit[]>([])
  let indexing = $state.raw<Progress | null>(null)
  let hitSort = $state<Sort | null>(null)
  let hitDesc = $state(false)
  const ranked = $derived(hits && hitSort ? arrange(hits, '', hitSort, hitDesc) : hits)
  let finding = $state(false)
  let partial = $state(false)
  let reveal = $state.raw<{ name: string; center?: boolean }>()
  let query = $state('')
  let sort = $state<Sort>('name')
  let desc = $state(false)
  let chosen = $state<View>()
  let subs = $state(load('subs') === '1')
  let carry = $state(true)
  type Asking = { to: Loc; c: Carried; copy: boolean; extra: string[] }
  let asking = $state.raw<Asking | null>(null)
  let dragging = $state(false)
  let depth = 0
  let preview = $state.raw<Entry | null>(null)
  let details = $state.raw<Entry | null>(null)
  let dialog = $state<Dialog | null>(null)
  let searching = $state(false)
  let help = $state(false)
  let sheet = $state<'new' | 'more' | null>(null)
  let filterEl = $state<HTMLInputElement>()
  let opener = $state<HTMLButtonElement>()
  let barH = $state(0)
  let conflict = $state<{ name: string; rest: number; resolve: (r: [Choice, boolean] | null) => void } | null>(null)
  const selected = new SvelteSet<string>()
  let calm = 0
  $effect(() => {
    if (dialog || asking) carry = true
  })
  let files = $state<HTMLInputElement>()
  let folder = $state<HTMLInputElement>()

  const here = $derived(`${vol}/${path}`)
  const join = (n: string) => child(path, n)
  const crumbs = $derived(path ? path.split('/') : [])
  const side = $derived(at === here ? sidecars(entries) : new Map<string, string[]>())
  const hidden = $derived(new Set(subs ? [] : [...side.values()].flat()))
  const visible = $derived(hidden.size ? entries.filter((e) => !hidden.has(e.name)) : entries)
  const shown = $derived(at !== here ? [] : streaming ? visible : arrange(visible, query, sort, desc))
  let frozen = $state<boolean>()
  const auto = $derived(frozen ?? (at === here && (!streaming || entries.length >= 200) ? prefersGrid(visible) : undefined))
  const grid = $derived(chosen ? chosen === 'grid' : !!auto)
  const byDay = $derived.by(() => {
    const day = grid && mostlyMedia(shown) ? dated() : days()
    return (e: Entry) => day(e.mtime)
  })
  $effect(() => {
    if (frozen === undefined && auto !== undefined) frozen = auto
  })
  const subsOf = (names: string[]) => [...new Set(names.flatMap((n) => side.get(n) ?? []))].filter((n) => !names.includes(n))

  function flipView() {
    chosen = grid ? 'list' : 'grid'
    keepView(here, chosen)
  }
  const one = $derived(selected.size === 1 ? entries.find((e) => selected.has(e.name)) : undefined)
  const thumb = (e: Entry) => (e.dir || thumbable(e.name) ? thumbURL(vol, join(e.name)) : null)
  const raw = (e: Entry) => (rawThumb(e) ? rawURL(vol, join(e.name)) : null)

  let listing: Entry[] = []
  let listingKey = ''

  function overlay(list: Entry[], started: number, key: string) {
    listing = list
    listingKey = key
    ops = ops.filter((o) => o.key !== key || !o.settled || o.settled >= started)
    const mine = ops.filter((o) => o.key === key)
    if (!mine.length) return list
    const m = new Map(list.map((e) => [e.name, e]))
    for (const o of mine) {
      if (o.entry) m.set(o.name, o.entry)
      else m.delete(o.name)
    }
    return [...m.values()]
  }

  const reapply = () => (entries = overlay(listing, 0, listingKey))

  function begin(changes: [string, Entry | null][]) {
    const mine = changes.map(([name, entry]) => ({ key: here, name, entry, settled: 0 }))
    ops.push(...mine)
    reapply()
    return mine
  }

  function settle(mine: Op[]) {
    const c = ++clock
    for (const o of mine) o.settled = c
    reapply()
  }

  function neighbour(gone: Set<string>) {
    const idx = shown.flatMap((e, i) => (gone.has(e.name) ? [i] : []))
    if (!idx.length) return
    const ok = (e: Entry) => !gone.has(e.name)
    return (shown.slice(idx.at(-1)! + 1).find(ok) ?? shown.slice(0, idx[0]).reverse().find(ok))?.name
  }

  function rollback(mine: Op[], err: unknown) {
    fail(err)
    const visible = mine[0]?.key === here && at === here
    const next = visible ? neighbour(new Set(mine.filter((o) => o.entry).map((o) => o.name))) : undefined
    ops = ops.filter((o) => !mine.includes(o))
    reapply()
    if (!visible) return
    const back = mine.filter((o) => !o.entry && entries.some((e) => e.name === o.name)).map((o) => o.name)
    if (back.length) {
      selected.clear()
      for (const n of back) selected.add(n)
    }
    const to = back[0] ?? next
    if (to) reveal = { name: to }
    refresh()
  }

  const isFile = (v: string, p: string) =>
    api.ls(v, parent(p)).then(
      (es) => es.some((e) => e.name === base(p) && !e.dir),
      () => false,
    )

  async function refresh() {
    const want = here
    const started = ++clock
    loading?.abort()
    const { signal } = (loading = new AbortController())
    let last = -Infinity
    let said = -Infinity
    const partial = (es: Entry[]) => {
      if (want !== here || (at === want && !streaming) || performance.now() - last < 150) return
      last = performance.now()
      if (last - said > 2500) {
        said = last
        announce = t.loadingItems(es.length)
      }
      entries = overlay(es.slice(), started, want)
      streaming = true
      error = null
      at = want
    }
    const [list, err] = await api.ls(vol, path, signal, partial).then(
      (es) => [es, null] as const,
      (e: Error) => [[], e instanceof HttpError ? { status: e.status, message: e.message } : { status: 0, message: t.loadFailed }] as const,
    )
    if (want !== here || signal?.aborted) return false
    if (err && err.status >= 500 && path && (await isFile(vol, path))) {
      if (want === here) navigate(`${selectURL(vol, path)}&preview`, true)
      return false
    }
    if (want !== here) return false
    entries = overlay([...list], started, want)
    listed = started
    error = err
    streaming = false
    announce = ''
    at = want
    return true
  }

  $effect(() => {
    vol
    path
    filter = ''
    searching = false
    error = null
    details = null
    chosen = viewOf(`${vol}/${path}`)
    frozen = undefined
    reveal = undefined
    listing = []
    listingKey = ''
    const focus = new URLSearchParams(untrack(() => route.search)).get('details')
    refresh().then((ok) => {
      if (ok && focus) details = entries.find((e) => e.name === focus) ?? null
    })
    return () => loading?.abort()
  })

  async function nearest(p: string) {
    const ups: string[] = []
    while (p) ups.push((p = parent(p)))
    const ok = await Promise.all(ups.map((u) => !u || api.exists(vol, u)))
    return ups[ok.indexOf(true)] ?? ''
  }

  $effect(() => {
    up = null
    if (error?.status !== 404 && error?.status !== 403) return
    let live = true
    nearest(path).then((p) => live && (up = p))
    return () => void (live = false)
  })

  function closeDetails() {
    const back = document.activeElement === document.body || !!document.activeElement?.closest('.details')
    details = null
    if (back) tick().then(() => document.querySelector<HTMLElement>('.main [role=grid] [tabindex="0"]')?.focus())
    if (new URLSearchParams(route.search).has('details')) navigate(route.path, true)
  }

  $effect(() => {
    const q = filter.trim()
    const filters: Record<string, string> = {}
    if (kindF) filters.kind = kindF
    if (whenF) filters.after = since(whenF)
    if (sizeF) filters.min = sizeF
    if (scope !== 'all' || (!(filtering && !q) && [...q].length < 2 && !/^([\u30fc\uff70]|(?=[\p{L}\p{Nl}])[\p{sc=Han}\p{sc=Hiragana}\p{sc=Katakana}\p{sc=Hangul}])$/u.test(q))) {
      hits = null
      finding = false
      return
    }
    finding = true
    const stop = new AbortController()
    const id = setTimeout(
      () =>
        api.search(q, stop.signal, filters).then(
          (r) => {
            if (stop.signal.aborted) return
            hits = r.entries
            content = r.content
            indexing = r.indexing
            partial = r.scanning
            hitSort = null
            finding = false
          },
          (e: Error) => {
            if (stop.signal.aborted) return
            finding = false
            fail(e)
          },
        ),
      200,
    )
    return () => {
      clearTimeout(id)
      stop.abort()
    }
  })

  let asked = 0
  let listed = 0
  $effect(() => {
    const params = new URLSearchParams(route.search)
    const names = params.getAll('select')
    if (!names.length) return void (asked = 0)
    if (at !== here || streaming) return
    entries
    untrack(async () => {
      asked ||= ++clock
      const hit = names.filter((n) => entries.some((e) => e.name === n))
      if (hit.length < names.length && listed < asked) return void refresh()
      asked = 0
      filter = query = ''
      searching = false
      await tick()
      if (hit.length) {
        selected.clear()
        for (const n of hit) selected.add(n)
        reveal = { name: hit[0], center: true }
        if (params.has('preview')) open(entries.find((e) => e.name === hit[0])!)
      }
      navigate(route.path, true)
    })
  })

  const hitLoc = (e: Entry) => e as RecentFile

  const locate = (e: Entry) => actOn('folder', hitLoc(e))

  $effect(() => {
    const f = filter
    if (!f) return void (query = '')
    const id = setTimeout(() => (query = f), 120)
    return () => clearTimeout(id)
  })

  $effect(() => {
    query
    vol
    path
    selected.clear()
  })

  $effect(() => {
    const list = entries
    if (streaming) return
    untrack(() => {
      if (!selected.size) return
      const have = new Set(list.map((e) => e.name))
      for (const n of [...selected]) if (!have.has(n)) selected.delete(n)
    })
  })

  $effect(() => {
    const s = document.documentElement.style
    if (selected.size && barH) s.setProperty('--bar-h', `${barH}px`)
    else s.removeProperty('--bar-h')
    return () => s.removeProperty('--bar-h')
  })

  function open(e: Entry) {
    if (e.dir) navigate(filesURL(vol, join(e.name)))
    else preview = e
  }

  function download(names: string[]) {
    const hit = names.length === 1 ? entries.find((e) => e.name === names[0]) : undefined
    if (hit && !hit.dir) saveURL(rawURL(vol, join(hit.name), true))
    else {
      const name = t.zipName(hit ? hit.name : (crumbs.at(-1) ?? vol), names.length)
      saveZip(zipURL(vol, names.map(join), name), name)
    }
  }

  const ask = (name: string, rest: number) => new Promise<[Choice, boolean] | null>((resolve) => (conflict = { name, rest, resolve }))

  async function upload(list: FileList | File[] | null | undefined, asFolder = false) {
    if (!list?.length || error) return
    forgetUndo()
    const v = vol
    const dir = path
    let items = [...list].map((file) => ({ file, rel: asFolder ? file.webkitRelativePath : '' }))
    const top = (x: { file: File; rel: string }) => (x.rel ? x.rel.slice(0, x.rel.indexOf('/')) : x.file.name)
    const taken = new Set((await api.ls(v, dir).catch(() => entries)).map((e) => e.name))
    const clash = [...new Set(items.map(top))].filter((n) => taken.has(n))
    const choices = new Map<string, Choice>()
    let every: Choice | null = null
    for (const [i, n] of clash.entries()) {
      if (!every) {
        const r = await ask(n, clash.length - i - 1)
        conflict = null
        if (!r) return
        if (r[1]) every = r[0]
        choices.set(n, r[0])
      } else choices.set(n, every)
    }
    const renamed = new Map<string, string>()
    for (const [n, c] of choices) {
      if (c !== 'keep' || !asFolder) continue
      let k = 1
      while (taken.has(`${n} (${k})`)) k++
      renamed.set(n, `${n} (${k})`)
      taken.add(`${n} (${k})`)
    }
    items = items
      .filter((x) => choices.get(top(x)) !== 'skip')
      .map((x) => (renamed.has(top(x)) ? { ...x, rel: renamed.get(top(x)) + x.rel.slice(x.rel.indexOf('/')) } : x))
    const meta = { vol: v, dir: dir || '/' }
    const over = items.filter((x) => choices.get(top(x)) === 'replace')
    if (over.length) enqueue(over, '/upload/', { ...meta, overwrite: '1' }, refresh, (rs) => undo(() => unreplace(rs)))
    const rest = items.filter((x) => choices.get(top(x)) !== 'replace')
    if (rest.length) enqueue(rest, '/upload/', meta, refresh)
  }

  const creators = [
    { label: t.upload, icon: Upload, run: () => files?.click() },
    { label: t.uploadFolder, icon: FolderUp, run: () => folder?.click() },
    { label: t.newFolder, icon: FolderPlus, run: () => (dialog = { kind: 'mkdir' }) },
  ]

  function sortBy(k: Sort) {
    desc = flip(sort, desc, k)
    sort = k
  }

  async function search() {
    searching = true
    await tick()
    filterEl?.focus()
  }

  async function endSearch() {
    searching = false
    filter = kindF = whenF = sizeF = ''
    scope = 'here'
    await tick()
    if (narrow.current) opener?.focus()
  }

  const act = {
    select: { id: 'select', label: t.selectItem, icon: SquareCheck },
    open: { id: 'open', label: t.open, icon: FolderOpen },
    download: { id: 'download', label: t.download, icon: Download },
    rename: { id: 'rename', label: t.rename, icon: Pencil },
    move: { id: 'move', label: t.moveOrCopy, icon: FolderInput },
    share: { id: 'share', label: t.copyLink, icon: Link },
    star: { id: 'star', label: t.star, icon: Star },
    unstar: { id: 'unstar', label: t.unstar, icon: StarOff },
    details: { id: 'details', label: t.details, icon: Info },
    remove: { id: 'remove', label: t.remove, icon: Trash, danger: true },
    mkdir: { id: 'mkdir', label: t.newFolder, icon: FolderPlus },
    upload: { id: 'upload', label: t.upload, icon: Upload },
  } satisfies Record<string, Action>

  const actions = (e: Entry | null): Action[] =>
    !e ? (error ? [] : [act.mkdir, act.upload])
    : [...(narrow.current ? [act.select] : []), act.open, act.download, act.rename, act.move, act.share, starred(vol, join(e.name)) ? act.unstar : act.star, act.details, act.remove]

  function onaction(id: string, e: Entry | null) {
    if (id === 'mkdir') dialog = { kind: 'mkdir' }
    else if (id === 'upload') files?.click()
    else if (!e) return
    else if (id === 'select') selected.add(e.name)
    else if (id === 'open') open(e)
    else if (id === 'download') download([e.name])
    else if (id === 'rename') dialog = { kind: 'rename', e }
    else if (id === 'move') dialog = { kind: 'move', names: [e.name] }
    else if (id === 'remove') remove([e.name])
    else if (id === 'star' || id === 'unstar') toggleStar([e.name], id === 'star')
    else if (id === 'share') quickShare(e)
    else details = e
  }

  function quickShare(e: Entry) {
    const made = api.newShare({ vol, path: join(e.name), mode: 'read', password: '', expires_in: 7 * 86400 })
    const copied = copyLater(made.then((r) => shareLink(r.token)))
    made.then(
      async () => toast((await copied) ? t.quickShared : t.quickSharedShown, { actions: [{ label: t.shareOptions, run: () => (details = e) }] }),
      fail,
    )
  }

  $effect(() => void loadStars())

  $effect(() => {
    const mine = refresh
    live.refresh = mine
    return () => {
      if (live.refresh === mine) live.refresh = () => {}
    }
  })

  const allStarred = $derived(selected.size > 0 && [...selected].every((n) => starred(vol, join(n))))

  function toggleStar(names: string[], on: boolean) {
    forgetUndo()
    star(vol, names.map(join), on).then(() => selected.clear(), fail)
  }

  type Failed = { name: string; error: Error }

  const undo = (run: () => Promise<Failed[]>): ToastAction => ({
    label: t.undo,
    keys: 'Control+Z Meta+Z',
    run: () =>
      run()
        .then((bad) => {
          if (!bad.length) toast(t.undone)
          else if (bad.length === 1) fail(new Error(t.failedItem(t.undoFailed(t.what([bad[0].name])), bad[0].error.message)))
          else fail(new Error(t.undoFailed(t.list(bad.map((b) => t.what([b.name]))))))
        }, fail)
        .finally(() => live.refresh()),
  })

  async function reverse(moves: Move[]) {
    if (moves.some((m) => m.from.vol !== m.to.vol)) toast(t.undoing, { kind: 'info' })
    const bad: Failed[] = []
    for (const m of [...moves].reverse()) await api.move(m.to, m.from).catch((error) => bad.push({ name: base(m.from.path), error }))
    return bad
  }

  async function restore(v: string, items: { path: string; id: string }[]) {
    if (!items.length) return []
    const { failed } = await api.restoreMany(v, items.map((x) => x.id))
    const path = new Map(items.map((x) => [x.id, x.path]))
    return failed.map((f) => ({ name: base(path.get(f.id) ?? f.id), error: new Error(errorText(f.status) ?? f.error) }))
  }

  async function unreplace(rs: Replaced[]) {
    const bad: Failed[] = []
    for (const r of [...rs].reverse()) await api.restoreVersion(r.vol, r.id).catch((error: Error) => bad.push({ name: base(r.path), error }))
    return bad
  }

  function pick(name: string) {
    calm = performance.now() + 300
    selected.clear()
    if (!narrow.current) selected.add(name)
    reveal = { name, center: true }
  }

  function vacant(n: string) {
    if (entries.some((e) => e.name === n)) throw new Error(t.errors[409])
  }

  function mkdir(n: string) {
    vacant(n)
    forgetUndo()
    const mine = begin([[n, { name: n, dir: true, size: 0, mtime: Date.now() }]])
    pick(n)
    const p = join(n)
    queue([`${vol}/${p}`], (s) => api.mkdir(vol, p, s)).then(
      (e) => {
        mine[0].entry = e
        settle(mine)
      },
      (err) => rollback(mine, err),
    )
  }

  function rename(e: Entry, n: string, extra: string[] = []) {
    const pairs: [Entry, string][] = [[e, n]]
    if (stem(n) !== stem(e.name)) for (const x of entries) if (extra.includes(x.name)) pairs.push([x, subtitleRename(e.name, n, x.name)])
    vacant(n)
    for (const [x, to] of pairs.slice(1)) if (to !== x.name && entries.some((y) => y.name === to)) throw new Error(t.failedItem(t.what([to]), t.errors[409]))
    if (details?.name === e.name) details = { ...e, name: n }
    apply(pairs, t.renamed(n), n, () => details?.name === n && (details = e))
  }

  function renameMany(plan: [string, string][]) {
    const moving = new Set(plan.map(([from]) => from))
    const pairs: [Entry, string][] = []
    for (const [from, to] of plan) {
      const e = entries.find((x) => x.name === from)
      if (!e) continue
      pairs.push([e, to])
      if (stem(to) === stem(from)) continue
      for (const x of side.get(from) ?? []) {
        const sub = entries.find((y) => y.name === x)
        if (sub && !moving.has(x)) pairs.push([sub, subtitleRename(from, to, x)])
      }
    }
    const after = new Set([...entries.map((x) => x.name).filter((x) => !pairs.some(([e]) => e.name === x)), ...pairs.map(([, to]) => to)])
    if (after.size !== entries.length) throw new Error(t.errors[409])
    selected.clear()
    apply(pairs, t.renamedMany(plan.length), plan[0][1])
  }

  // apply moves names in place; when one new name is another's old one, everything passes through a temporary name first.
  function apply(pairs: [Entry, string][], said: string, focus: string, failed?: () => void) {
    forgetUndo()
    pairs = pairs.filter(([x, to]) => x.name !== to)
    const olds = new Set(pairs.map(([x]) => x.name))
    const chained = pairs.some(([, to]) => olds.has(to))
    const at = (n: string) => ({ vol, path: join(n) })
    const tmp = (i: number) => `.filebox-rename-${Date.now().toString(36)}-${i}`
    const ms: Move[] = chained
      ? [...pairs.map(([x], i) => ({ from: at(x.name), to: at(tmp(i)) })), ...pairs.map(([, to], i) => ({ from: at(tmp(i)), to: at(to) }))]
      : pairs.map(([x, to]) => ({ from: at(x.name), to: at(to) }))
    const mine = begin(pairs.flatMap(([x, to]): [string, Entry | null][] => [[x.name, null], [to, { ...x, name: to }]]))
    pick(focus)
    const pending = queue(ms.flatMap((m) => [m.from, m.to]).map((l) => `${l.vol}/${l.path}`), async (s) => {
      const done: Move[] = []
      for (const m of ms) {
        try {
          await api.mv(m.from, m.to, s)
        } catch (err) {
          const now = await api.ls(vol, path).then((es) => new Set(es.map((x) => x.name)), () => null)
          const landed = now ? ms.filter((x) => now.has(base(x.to.path)) && !now.has(base(x.from.path))) : done
          const stuck: string[] = []
          for (const x of [...landed].reverse()) await api.move(x.to, x.from).catch(() => stuck.unshift(base(x.to.path)))
          const why = pairs.length > 1 ? t.failedItem(t.what([base(m.from.path)]), (err as Error).message) : (err as Error).message
          throw new Error(stuck.length ? `${why} ${t.stuckAt(t.what(stuck), place(vol, path))}` : why)
        }
        done.push(m)
      }
    })
    const id = toast(said, { actions: [undo(() => pending.then(() => reverse(ms)))] })
    pending.then(
      () => {
        settle(mine)
        loadStars(true)
      },
      (err) => {
        retract(id)
        failed?.()
        rollback(mine, err)
      },
    )
  }

  async function remove(names: string[]) {
    const v = vol
    const paths = names.map(join)
    const kept = subs ? [] : subsOf(names)
    const next = neighbour(new Set(names))
    const mine = begin(names.map((n) => [n, null]))
    selected.clear()
    if (next) reveal = { name: next }
    if (details && names.includes(details.name)) closeDetails()
    const pending = queue(paths.map((p) => `${v}/${p}`), (s) => api.rm(v, paths, s))
    const id = toast(t.trashed(t.what(names)) + (kept.length ? t.keptSubtitles(kept.length) : ''), { actions: [undo(async () => restore(v, (await pending).trashed))], ms: narrow.current ? 2 ** 31 - 1 : undefined })
    try {
      const r = await pending
      loadStars(true)
      settle(mine)
      const bad = r.failed.filter((f) => f.status !== 404)
      if (!bad.length) return
      const keep = new Set(bad.map((f) => base(f.path)))
      const done = names.filter((n) => !keep.has(n))
      if (done.length) retext(id, t.trashed(t.what(done)))
      else retract(id)
      const f = bad[0]
      rollback(
        mine.filter((o) => keep.has(o.name)),
        new Error(bad.length > 1 ? t.removeFailed([...keep]) : t.failedItem(t.what([base(f.path)]), errorText(f.status) ?? f.error)),
      )
    } catch (err) {
      retract(id)
      rollback(mine, err)
    }
  }

  function moved(done: Move[], copy: boolean) {
    if (copy) forgetUndo()
    if (!copy) loadStars(true)
    selected.clear()
    closeDetails()
    refresh()
    if (!done.length) return
    const w = t.what(done.map((m) => base(m.from.path)))
    const to = done[0].to
    const show = { label: t.open, run: () => navigate(selectURL(to.vol, to.path)) }
    if (copy) toast(t.copiedTo(w, place(to.vol, parent(to.path))), { actions: [show] })
    else toast(t.movedTo(w, place(to.vol, parent(to.path))), { actions: [show, undo(() => reverse(done))] })
  }

  async function dropInto(to: { vol: string; path: string }, c: Carried, copy: boolean) {
    const extra = c.vol === vol && c.dir === path ? subsOf(c.names) : []
    if (extra.length) return void (asking = { to, c, copy, extra })
    if (copy) forgetUndo()
    const r = await api.transfer(c.vol, c.dir, c.names, to, copy)
    moved(r.done, copy)
    if (r.error) fail(r.error)
  }

  async function carried(a: Asking, withSubs: boolean) {
    asking = null
    if (a.copy) forgetUndo()
    const r = await api.transferAll(a.c.vol, a.c.dir, withSubs ? [...a.c.names, ...a.extra] : a.c.names, a.to, a.copy, undefined, () => toast(t.undoing, { kind: 'info' }))
    moved(r.done, a.copy)
    if (r.error) fail(r.error)
  }

  const into = (to: { vol: string; path: string }, spring?: () => void): Target => ({
    accepts: (c) => !inside(c, to),
    drop: (c, copy) => dropInto(to, c, copy),
    spring,
  })

  onMount(() => {
    sink.into = dropInto
    return () => {
      if (sink.into === dropInto) sink.into = undefined
    }
  })

  const dnd = {
    carry: (e: Entry) => ({ vol, dir: path, names: selected.has(e.name) ? [...selected] : [e.name] }),
    target: (e: Entry) => into({ vol, path: join(e.name) }, () => open(e)),
  }

  async function pass(id: string) {
    const e = one
    selected.clear()
    await tick()
    document.querySelector<HTMLElement>('[role=grid] [tabindex="0"]')?.focus()
    if (e) onaction(id, e)
  }

  function focused() {
    const i = (document.activeElement as HTMLElement | null)?.closest<HTMLElement>('[data-i]')?.dataset.i
    return i === undefined ? undefined : shown[+i]
  }

  function inList(el: Element) {
    const at = getSelection()?.anchorNode
    return !!el.closest('.files') || (el.matches('body, main') && (!at || !!at.parentElement?.closest('.files')))
  }

  function paste(e: ClipboardEvent) {
    if (document.querySelector(":is([role=dialog], [role=menu]):not([data-state='closed'])")) return
    if ((e.target as Element).matches?.('input, select, textarea, [contenteditable]')) return
    const files = [...(e.clipboardData?.files ?? [])]
    if (!files.length) return
    e.preventDefault()
    const d = new Date()
    const two = (n: number) => String(n).padStart(2, '0')
    const stamp = `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}.${two(d.getMinutes())}.${two(d.getSeconds())}`
    upload(files.map((f, i) => (/^image\.(png|jpe?g|gif|webp)$/i.test(f.name) ? new File([f], `${t.pastedImage(stamp)}${files.length > 1 ? ` ${i + 1}` : ''}.${f.name.split('.').pop()}`, { type: f.type, lastModified: f.lastModified }) : f)))
  }

  function keydown(e: KeyboardEvent) {
    if (document.querySelector(":is([role=dialog], [role=menu]):not([data-state='closed'])")) return
    const typing = (e.target as Element).matches?.('input:not([type=checkbox], [type=radio]), select, textarea, [contenteditable]')
    const mod = e.metaKey || e.ctrlKey
    const k = e.key.toLowerCase()
    const target = one ?? focused()
    if (e.key === 'Escape') {
      if (details) closeDetails()
      else if (searching || filter || filtering) endSearch()
      else selected.clear()
    } else if (typing || e.altKey) return
    else if (mod && !e.shiftKey && !e.repeat && k === 'z' && runLatest(t.undo)) e.preventDefault()
    else if (mod && !e.shiftKey && k === 'a' && inList(e.target as Element)) {
      e.preventDefault()
      for (const x of shown) selected.add(x.name)
    } else if (mod) return
    else if (e.key === '/' || e.key === '?' || ((e.key === 'n' || e.key === 'u') && !(e.target as Element).closest('[data-seeking]')) || (e.key === 'F2' && target)) {
      e.preventDefault()
      if (e.key === '/') search()
      else if (e.key === '?') help = true
      else if (e.key === 'n') dialog = { kind: 'mkdir' }
      else if (e.key === 'u') files?.click()
      else if (selected.size > 1) dialog = { kind: 'renameMany', names: shown.filter((x) => selected.has(x.name)).map((x) => x.name) }
      else if (target) dialog = { kind: 'rename', e: target }
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && !(e.target as Element).closest('[data-seeking]') && selected.size && !e.repeat && performance.now() > calm && inList(e.target as Element)) remove([...selected])
    else if (e.key === 'Enter' && selected.size === 1 && !(e.target as Element).closest('button, a, [role=grid]')) {
      const hit = entries.find((x) => selected.has(x.name))
      if (hit) open(hit)
    }
  }

  const hasFiles = (e: DragEvent) => !!e.dataTransfer?.types.includes('Files')
</script>

<svelte:window onkeydowncapture={keydown} onpaste={paste} onfocus={() => loadStars(true)} ondragover={(e) => e.preventDefault()} ondrop={(e) => e.preventDefault()} />

{#snippet batch()}
  <button class="ghost" onclick={() => download([...selected])}>
    <Download size={icon.sm} />{t.download}
  </button>
  <button class="ghost" onclick={() => (dialog = { kind: 'move', names: [...selected] })}><FolderInput size={icon.sm} />{t.moveOrCopy}</button>
  {#if selected.size > 1}<button class="ghost" onclick={() => (dialog = { kind: 'renameMany', names: shown.filter((x) => selected.has(x.name)).map((x) => x.name) })}><Pencil size={icon.sm} />{t.rename}</button>{/if}
  <button class="ghost" onclick={() => toggleStar([...selected], !allStarred)}>
    {#if allStarred}<StarOff size={icon.sm} />{t.unstar}{:else}<Star size={icon.sm} />{t.star}{/if}
  </button>
  <button class="ghost danger" onclick={() => remove([...selected])}><Trash size={icon.sm} />{t.remove}</button>
  <button class="icon-btn" aria-label={t.clearSelection} onclick={() => selected.clear()}><X size={icon.sm} /></button>
{/snippet}

<div class="files-wrap">
  <section
    class="files"
    class:selecting={selected.size > 0}
    aria-label={vol}
    ondragenter={(e) => {
      if (!hasFiles(e)) return
      depth++
      dragging = true
    }}
    ondragleave={() => {
      if (--depth <= 0) {
        depth = 0
        dragging = false
      }
    }}
    ondrop={(e) => {
      depth = 0
      dragging = false
      upload(e.dataTransfer?.files)
    }}
  >
    <header class="bar" class:searching class:expanded={!collapsed && !ranked && !error}>
      <NavToggle />
      {#if crumbs.length}
        {@const up = crumbs.length > 1 ? crumbs[crumbs.length - 2] : vol}
        <a class="up" href={filesURL(vol, parent(path))} onclick={link} aria-label={t.upTo(up)}><ChevronLeft size={icon.md} /><span>{up}</span></a>
      {/if}
      <h1 class="title">{crumbs.length ? crumbs[crumbs.length - 1] : vol}</h1>
      <nav class="crumbs" aria-label={t.breadcrumb}>
        <a href={filesURL(vol, '')} onclick={link} use:target={into({ vol, path: '' })}>{vol}</a>
        {#each crumbs as c, i}
          {@const to = crumbs.slice(0, i + 1).join('/')}
          <ChevronRight size={icon.sm} />
          <a href={filesURL(vol, to)} onclick={link} use:target={into({ vol, path: to })} aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</a>
        {/each}
      </nav>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class="primary new" disabled={!!error}><Plus size={icon.md} />{t.new}</DropdownMenu.Trigger>
        <DropdownMenu.Portal to="main">
          <DropdownMenu.Content class="menu" preventScroll={false} align="start" sideOffset={4}>
            {#each creators as c, i (c.label)}
              {#if i === 2}<DropdownMenu.Separator class="menu-sep" />{/if}
              <DropdownMenu.Item class="menu-item" onSelect={c.run}><c.icon size={icon.sm} />{c.label}</DropdownMenu.Item>
            {/each}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      <span class="grow"></span>
      <div class="find">
        <label for="filter" class="sr-only">{t.filter}</label>
        <input
          id="filter"
          class="filter"
          type="search"
          bind:value={filter}
          bind:this={filterEl}
          placeholder={scope === 'all' ? t.searchAll : t.filter}
          onkeydown={(e) => {
            if (e.key === 'Enter' && !finding && ranked?.length) locate(ranked[0])
          }}
        />
        <div class="scope" role="radiogroup" aria-label={t.searchScope}>
          {#each [['here', t.scopeHere], ['all', t.scopeAll]] as const as [k, label] (k)}
            <button
              type="button"
              role="radio"
              aria-checked={scope === k}
              tabindex={scope === k ? 0 : -1}
              onkeydown={async (e) => {
                if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
                e.preventDefault()
                const group = e.currentTarget.parentElement
                scope = scope === 'here' ? 'all' : 'here'
                await tick()
                group?.querySelector<HTMLElement>('[aria-checked=true]')?.focus()
              }}
              onclick={() => {
                scope = k
                filterEl?.focus()
              }}>{label}</button
            >
          {/each}
        </div>
      </div>
      <button class="icon-btn search-open" aria-label={t.openFilter} onclick={search} bind:this={opener}><Search size={icon.md} /></button>
      <button class="icon-btn search-close" aria-label={t.closeFilter} onclick={endSearch}><X size={icon.md} /></button>
      <button class="icon-btn view" aria-label={grid ? t.listView : t.gridView} title={grid ? t.listView : t.gridView} onclick={flipView}>
        {#if grid}<List size={icon.md} />{:else}<LayoutGrid size={icon.md} />{/if}
      </button>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class={side.size ? 'icon-btn overflow keep' : 'icon-btn overflow'} aria-label={t.more}><EllipsisVertical size={icon.md} /></DropdownMenu.Trigger>
        <DropdownMenu.Portal to="main">
          <DropdownMenu.Content class="menu" preventScroll={false} align="end" sideOffset={4}>
            <DropdownMenu.Item class="menu-item" onSelect={flipView}>
              {#if grid}<List size={icon.sm} />{t.listView}{:else}<LayoutGrid size={icon.sm} />{t.gridView}{/if}
            </DropdownMenu.Item>
            {#if side.size}
              <DropdownMenu.CheckboxItem class="menu-item" bind:checked={() => subs, (v) => save('subs', (subs = v) ? '1' : '0')}>
                {#snippet children({ checked })}{#if checked}<Check size={icon.sm} />{:else}<span class="menu-gap"></span>{/if}{t.showSubtitles}{/snippet}
              </DropdownMenu.CheckboxItem>
            {/if}
            <DropdownMenu.Separator class="menu-sep" />
            <DropdownMenu.Group>
              <DropdownMenu.GroupHeading class="menu-label">{t.sortBy}</DropdownMenu.GroupHeading>
              {#each sorts as [k, label] (k)}
                <DropdownMenu.Item class="menu-item" aria-current={sort === k || undefined} onSelect={() => sortBy(k)}>
                  {#if sort !== k}<span class="menu-gap"></span>{:else if desc}<ArrowDown size={icon.sm} />{:else}<ArrowUp size={icon.sm} />{/if}{label}
                </DropdownMenu.Item>
              {/each}
            </DropdownMenu.Group>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </header>

    {#if scope === 'all'}
      <div class="filters" role="group" aria-label={t.searchFilters}>
        {#each [{ label: t.filterKind, get: () => kindF, set: (v: string) => (kindF = v), options: t.kinds }, { label: t.filterWhen, get: () => whenF, set: (v: string) => (whenF = v), options: t.whens }, { label: t.filterSize, get: () => sizeF, set: (v: string) => (sizeF = v), options: t.sizes }] as f (f.label)}
          <label class="chip-select" class:on={!!f.get()}>
            <span>{f.get() ? f.options[f.get()] : f.label}</span>
            <ChevronDown size={icon.sm} aria-hidden="true" />
            <select aria-label={f.label} value={f.get()} onchange={(e) => f.set(e.currentTarget.value)}>
              <option value="">{t.any}</option>
              {#each Object.entries(f.options) as [k, v] (k)}<option value={k}>{v}</option>{/each}
            </select>
          </label>
        {/each}
        {#if filtering}<button class="ghost clear-filters" onclick={() => (kindF = whenF = sizeF = '')}>{t.clearFilters}</button>{/if}
      </div>
    {/if}
    {#if streaming && at === here}<p class="loading-count" aria-hidden="true">{t.loadingItems(entries.length)}</p>{/if}
    <p class="sr-only" role="status">{announce}</p>

    {#if ranked}
      {#if partial}<p class="hint banner">{t.indexing}</p>{/if}
      <EntryList
        entries={ranked}
        grid={false}
        bind:sort={() => hitSort as Sort, (v) => (hitSort = v)}
        bind:desc={() => hitDesc, (v) => (hitDesc = v)}
        thumb={(e) => (!e.dir && thumbable(e.name) ? thumbURL(hitLoc(e).vol, hitLoc(e).path) : null)}
        raw={(e) => (rawThumb(e) ? rawURL(hitLoc(e).vol, hitLoc(e).path) : null)}
        actions={(e) => (e ? [folderAction, downloadAction] : [])}
        onaction={(id, e) => e && actOn(id, hitLoc(e))}
        onopen={locate}
        loading={finding}
        id={(e) => `${hitLoc(e).vol}:${hitLoc(e).path}`}
        loc={hitLoc}
        head={!!ranked.length || !content.length}
      >
        {#snippet empty()}
          {#if !finding && !content.length}<EmptyState icon={SearchX} title={t.noResults} hint={t.noResultsHint} />{/if}
        {/snippet}
        {#snippet footer()}
          {#if !finding && (content.length || indexing)}<ContentHits hits={content} {indexing} onopen={(h) => actOn('folder', { ...h, dir: false })} />{/if}
        {/snippet}
      </EntryList>
    {:else}
    {#key here}
      <EntryList
        entries={shown}
        {grid}
        {selected}
        bind:sort
        bind:desc
        {thumb}
        {raw}
        meta={(e) => fileURL(vol, join(e.name), 'meta')}
        {actions}
        {onaction}
        onopen={open}
        {batch}
        {dnd}
        {reveal}
        tag={(e) => (side.has(e.name) && !subs ? (kind(e.name) === 'audio' ? t.lyrics : t.subtitles) : undefined)}
        loading={at !== here}
        busy={streaming}
        head={!error}
        title={error ? undefined : crumbs.length ? crumbs[crumbs.length - 1] : vol}
        group={sort === 'mtime' && !query && !streaming ? byDay : undefined}
        bind:collapsed
      >
        {#snippet empty()}
          {#if query}
            <EmptyState icon={SearchX} title={t.noMatch} hint={t.noMatchHint}>
              <button onclick={() => (filter = '')}>{t.clearFilter}</button>
            </EmptyState>
          {:else if error && !vols.includes(vol)}
            <EmptyState icon={HardDrive} as="h2" title={t.volMissing(vol)} hint={t.volMissingHint}>
              <div class="chips">
                {#each vols as v (v)}<a class="chip" href={filesURL(v, '')} onclick={link}><HardDrive size={icon.sm} />{v}</a>{/each}
              </div>
            </EmptyState>
          {:else if error?.status === 404 || error?.status === 403}
            {@const four = error.status === 404}
            <EmptyState icon={four ? FolderX : FolderLock} as="h2" title={four ? t.folderMissing : t.folderForbidden} hint={four ? t.folderMissingHint : t.folderForbiddenHint}>
              {#if up !== null && up !== path}<a class="button primary" href={filesURL(vol, up)} onclick={link}><FolderOpen size={icon.sm} />{t.openNamed(up ? base(up) : vol)}</a>{/if}
            </EmptyState>
          {:else if error}
            <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error.message}>
              <button
                class="primary"
                onclick={() => {
                  at = ''
                  refresh()
                }}>{t.retry}</button
              >
            </EmptyState>
          {:else}
            <EmptyState icon={FolderOpen} title={t.folderEmpty} hint={t.emptyHint}>
              <button class="primary" onclick={() => files?.click()}><Upload size={icon.sm} />{t.upload}</button>
            </EmptyState>
          {/if}
        {/snippet}
      </EntryList>
    {/key}
    {/if}

    {#if dragging}<div class="dropzone">{t.dropHere}</div>{/if}
    {#if !selected.size && !details && !error}<button class="primary fab" aria-label={t.new} onclick={() => (sheet = 'new')}><Plus size={icon.lg} /></button>{/if}
  </section>

  {#if details}
    {#await import('../components/Details.svelte') then { default: Details }}
      {#key details.name}
        <Details
          {vol}
          path={join(details.name)}
          entry={details}
          thumbs={[thumb(details), raw(details)]}
          onclose={closeDetails}
          onchange={() => refresh().then(() => (details = entries.find((e) => e.name === details?.name) ?? details))}
        />
      {/key}
    {/await}
  {/if}
</div>

<input bind:this={files} type="file" multiple hidden onchange={(e) => upload(e.currentTarget.files)} />
<input bind:this={folder} type="file" webkitdirectory hidden onchange={(e) => upload(e.currentTarget.files, true)} />

{#if conflict}
  {@const c = conflict}
  {#await import('../components/ConflictDialog.svelte') then { default: ConflictDialog }}
    {#key c}
      <ConflictDialog name={c.name} rest={c.rest} onchoose={(choice, all) => c.resolve([choice, all])} onclose={() => c.resolve(null)} />
    {/key}
  {/await}
{/if}

{#if help}
  {#await import('../components/ShortcutsDialog.svelte') then { default: ShortcutsDialog }}
    <ShortcutsDialog onclose={() => (help = false)} />
  {/await}
{/if}

{#if selected.size}
  <div class="sel-tools" role="group" aria-label={t.selected(selected.size)} bind:offsetHeight={barH}>
    <button onclick={() => download([...selected])}><Download size={icon.md} /><span>{t.download}</span></button>
    <button onclick={() => (dialog = { kind: 'move', names: [...selected] })}><FolderInput size={icon.md} /><span>{t.moveOrCopy}</span></button>
    <button disabled={!one} onclick={() => pass('share')}><Link size={icon.md} /><span>{t.copyLink}</span></button>
    <button onclick={() => (sheet = 'more')}><Ellipsis size={icon.md} /><span>{t.more}</span></button>
    <button class="danger" onclick={() => remove([...selected])}><Trash size={icon.md} /><span>{t.remove}</span></button>
  </div>
{/if}

{#if sheet}
  {#await import('../components/Sheet.svelte') then { default: Sheet }}
    {@const names = [...selected]}
    {@const mark = allStarred ? act.unstar : act.star}
    {@const items =
      sheet === 'new'
        ? creators
        : [
            { ...mark, run: () => toggleStar(names, mark === act.star) },
            ...(one ? [act.rename, act.details].map((a) => ({ ...a, run: () => pass(a.id) })) : [{ ...act.rename, run: () => (dialog = { kind: 'renameMany', names: shown.filter((x) => selected.has(x.name)).map((x) => x.name) }) }]),
          ]}
    <Sheet title={sheet === 'new' ? t.new : (one?.name ?? t.selected(selected.size))} onclose={() => (sheet = null)}>
      {#each items as c (c.label)}
        <button
          class="sheet-item"
          onclick={() => {
            sheet = null
            c.run()
          }}><c.icon size={icon.md} />{c.label}</button
        >
      {/each}
    </Sheet>
  {/await}
{/if}

{#if preview}
  {#await import('../components/Preview.svelte') then { default: Preview }}
    <Preview bind:entry={preview} entries={hidden.size ? [...shown, ...entries.filter((e) => hidden.has(e.name))] : shown} url={(e, as) => fileURL(vol, join(e.name), as)} saveTo={(e) => `/api/file?${new URLSearchParams({ vol, path: join(e.name) })}`} onclose={() => (preview = null)} />
  {/await}
{/if}

{#if asking}
  {@const a = asking}
  {#await import('../components/Modal.svelte') then { default: Modal }}
    <Modal title={t.moveCopyTitle(t.what(a.c.names))} onclose={() => (asking = null)} onsubmit={() => carried(a, carry)}>
      <p>{place(a.to.vol, a.to.path)}</p>
      <label class="check"><input type="checkbox" bind:checked={carry} />{t.withSubtitles(a.extra.length)}</label>
      {#snippet footer()}
        <button type="button" onclick={() => (asking = null)}>{t.cancel}</button>
        <button class="primary">{a.copy ? t.copy : t.move}</button>
      {/snippet}
    </Modal>
  {/await}
{/if}

{#if dialog}
  {#await Promise.all([import('../components/NameDialog.svelte'), import('../components/MoveDialog.svelte')]) then [{ default: NameDialog }, { default: MoveDialog }]}
    {#if dialog?.kind === 'mkdir'}
      <NameDialog
        title={t.newFolder}
        label={t.folderName}
        action={t.create}
        onsave={mkdir}
        onclose={() => (dialog = null)}
      />
    {:else if dialog?.kind === 'rename'}
      {@const e = dialog.e}
      {@const extra = subsOf([e.name])}
      <NameDialog
        title={t.rename}
        label={t.newName}
        action={t.rename}
        value={e.name}
        stem={!e.dir}
        onsave={(n) => rename(e, n, carry ? extra : [])}
        onclose={() => (dialog = null)}
      >
        {#if extra.length}<label class="check"><input type="checkbox" bind:checked={carry} />{t.withSubtitles(extra.length)}</label>{/if}
      </NameDialog>
    {:else if dialog?.kind === 'renameMany'}
      {#await import('../components/BatchRename.svelte') then { default: BatchRename }}
        <BatchRename names={dialog.names} taken={new Set(entries.map((x) => x.name))} onsave={renameMany} onclose={() => (dialog = null)} />
      {/await}
    {:else if dialog?.kind === 'move'}
      <MoveDialog
        {vols}
        {vol}
        dir={path}
        names={dialog.names}
        extra={subsOf(dialog.names)}
        ondone={moved}
        onclose={() => (dialog = null)}
      />
    {/if}
  {/await}
{/if}
