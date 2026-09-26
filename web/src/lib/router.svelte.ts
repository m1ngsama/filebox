export const route = $state({ path: location.pathname, search: location.search })

function sync() {
  route.path = location.pathname
  route.search = location.search
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
