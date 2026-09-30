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
