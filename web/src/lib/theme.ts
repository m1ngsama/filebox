import { load, save } from './storage'

export type Theme = 'system' | 'light' | 'dark'

export const theme = (): Theme => {
  const v = load('theme')
  return v === 'light' || v === 'dark' ? v : 'system'
}

export function setTheme(v: Theme) {
  save('theme', v)
  if (v === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = v
}
