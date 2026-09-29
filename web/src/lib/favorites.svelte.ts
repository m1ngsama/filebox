import { SvelteSet } from 'svelte/reactivity'
import { api, type Favorite } from './api'
import { toast } from './toast.svelte'
import { t } from './i18n'
import { base } from './format'

const stars = new SvelteSet<string>()
const key = (vol: string, path: string) => `${vol}\0${path}`
let pending: Promise<Favorite[]> | undefined

export function loadStars(fresh = false) {
  if (fresh || !pending) {
    const p = api.favorites().then((r) => {
      stars.clear()
      for (const f of r.entries) stars.add(key(f.vol, f.path))
      return r.entries
    })
    p.catch(() => pending === p && (pending = undefined))
    pending = p
  }
  return pending
}

export const starred = (vol: string, path: string) => stars.has(key(vol, path))

export async function star(vol: string, paths: string[], on: boolean) {
  await api.star(vol, paths, on)
  for (const p of paths) {
    if (on) stars.add(key(vol, p))
    else stars.delete(key(vol, p))
  }
  const what = t.what(paths.map(base))
  toast(on ? t.starred(what) : t.unstarred(what))
}
