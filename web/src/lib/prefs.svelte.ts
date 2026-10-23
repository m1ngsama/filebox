import { api } from './api'
import { save } from './storage'

export type Prefs = { accent?: string; density?: string }

// How the signed-in person likes filebox to look. The server keeps it with the account; this device keeps a copy
// so the first paint already wears it.
export const prefs = $state<Prefs>({})

export const accents = ['blue', 'teal', 'green', 'orange', 'pink', 'purple', 'graphite'] as const

function apply(p: Prefs) {
  const d = document.documentElement.dataset
  if (p.accent && p.accent !== 'blue') d.accent = p.accent
  else delete d.accent
  if (p.density === 'compact') d.density = 'compact'
  else delete d.density
  save('prefs', JSON.stringify(p))
}

export function usePrefs(p: Prefs | undefined) {
  prefs.accent = p?.accent
  prefs.density = p?.density
  apply(prefs)
}

export async function setPrefs(change: Prefs) {
  const before = { ...prefs }
  Object.assign(prefs, change)
  apply(prefs)
  try {
    await api.setPrefs({ ...prefs })
  } catch (e) {
    Object.assign(prefs, before)
    apply(prefs)
    throw e
  }
}
