import { api, type SavedSearch } from './api'

// The searches a person saved, shown in the sidebar.
export const saved = $state<{ list: SavedSearch[] }>({ list: [] })

export const loadSearches = () => api.searches().then((r) => (saved.list = r.searches), () => {})

export async function saveSearch(name: string, params: string) {
  const s = await api.saveSearch(name, params)
  saved.list = [...saved.list, s]
}

export async function dropSearch(id: number) {
  await api.dropSearch(id)
  saved.list = saved.list.filter((s) => s.id !== id)
}
