export function linger(node: HTMLElement) {
  let root = node
  while (root.parentElement && root.parentElement !== document.body) root = root.parentElement
  if (root.parentElement !== document.body) return
  return () => {
    if (root.isConnected) return
    node.removeAttribute('role')
    node.classList.add('leaving')
    root.inert = true
    root.ariaHidden = 'true'
    for (const f of root.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea')) f.readOnly = true
    document.body.append(root)
    const done = () => root.remove()
    if (!parseFloat(getComputedStyle(node).animationDuration)) return done()
    node.addEventListener('animationend', (e) => e.target === node && done())
    setTimeout(done, 1000)
  }
}

export function fade(node: HTMLElement) {
  node.inert = true
  return { duration: parseFloat(getComputedStyle(node).getPropertyValue('--dur-1')) || 0, css: (t: number) => `opacity: ${t}; translate: 0 calc(${1 - t} * var(--space-1))` }
}

// A row of pills with one highlight that glides to whichever is `.on`, keeps it in view,
// and lets the arrow keys step between pills. `_` is the value the row depends on, so the action re-runs when it changes.
export function glide(node: HTMLElement, _?: unknown) {
  let raf = 0
  const place = (animate: boolean) => {
    const on = node.querySelector<HTMLElement>('.on')
    if (!on) return
    node.classList.toggle('glide', animate)
    node.style.setProperty('--gx', `${on.offsetLeft}px`)
    node.style.setProperty('--gw', `${on.offsetWidth}px`)
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches
    const l = on.offsetLeft - node.scrollLeft
    if (l < 0 || l + on.offsetWidth > node.clientWidth) node.scrollTo({ left: on.offsetLeft - (node.clientWidth - on.offsetWidth) / 2, behavior: animate && !calm ? 'smooth' : 'auto' })
  }
  const keys = (e: KeyboardEvent) => {
    const all = [...node.querySelectorAll<HTMLElement>('button')]
    const i = all.indexOf(document.activeElement as HTMLElement)
    const to = e.key === 'ArrowRight' ? i + 1 : e.key === 'ArrowLeft' ? i - 1 : e.key === 'Home' ? 0 : e.key === 'End' ? all.length - 1 : -1
    if (to < 0 || i < 0) return
    e.preventDefault()
    all[(to + all.length) % all.length].focus()
  }
  const ro = new ResizeObserver(() => (cancelAnimationFrame(raf), (raf = requestAnimationFrame(() => place(false)))))
  ro.observe(node)
  node.addEventListener('keydown', keys)
  place(false)
  return {
    update: () => place(true),
    destroy() {
      ro.disconnect()
      cancelAnimationFrame(raf)
      node.removeEventListener('keydown', keys)
    },
  }
}
