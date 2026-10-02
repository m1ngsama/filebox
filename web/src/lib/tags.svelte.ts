import { api, type Tag } from './api'

export const tags = $state<{ list: Tag[] }>({ list: [] })
export const tagColors = ['', 'red', 'orange', 'yellow', 'green', 'teal', 'blue', 'purple', 'pink', 'gray'] as const

let pending: Promise<Tag[]> | null = null
export function loadTags(fresh = false) {
  if (fresh || !pending)
    pending = api.tags().then(
      (r) => (tags.list = r.tags),
      () => ((pending = null), tags.list),
    )
  return pending
}

export const tagOf = (id: number) => tags.list.find((x) => x.id === id)
