import { tick } from 'svelte'

export const route = $state({ path: location.pathname, search: location.search })

const still = matchMedia('(prefers-reduced-motion: reduce)')

const narrow = matchMedia('(max-width: 767px)')

function sync() {
  const from = route.path
  const to = location.pathname
  route.path = to
  route.search = location.search
  if (from === to || still.matches) return
  const push = to.startsWith(from)
  const frames = !narrow.matches || !(push || from.startsWith(to)) ? [{ opacity: 0.4 }, { opacity: 1 }] : [{ translate: push ? '24% 0' : '-24% 0', opacity: 0.4 }, { translate: '0 0', opacity: 1 }]
  tick().then(() => document.querySelector('.main')?.animate(frames, { duration: narrow.matches ? 280 : 140, easing: 'cubic-bezier(0.2, 0, 0, 1)' }))
}

export function navigate(url: string, replace = false) {
  history[replace ? 'replaceState' : 'pushState'](null, '', url)
  sync()
}

export function link(e: MouseEvent) {
  const a = e.currentTarget as HTMLAnchorElement
  if (e.button || e.metaKey || e.ctrlKey || e.shiftKey || a.target) return
  e.preventDefault()
  navigate(a.pathname + a.search)
}

addEventListener('popstate', sync)
