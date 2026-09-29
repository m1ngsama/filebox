import type { Loc } from './api'

export type Carried = { vol: string; dir: string; names: string[] }
export type Target = { accepts: (c: Carried) => boolean; drop: (c: Carried, copy: boolean) => void; spring?: () => void }

const MIME = 'application/x-filebox-items'
let carried: Carried | null = null

const carrying = (e: DragEvent) => !!carried && !!e.dataTransfer?.types.includes(MIME)

export const inside = (c: Carried, to: Loc) =>
  c.vol === to.vol && (to.path === c.dir || c.names.some((n) => { const p = c.dir ? `${c.dir}/${n}` : n; return to.path === p || to.path.startsWith(`${p}/`) }))

export function carry(e: DragEvent, c: Carried) {
  const dt = e.dataTransfer
  if (!dt) return
  carried = c
  dt.effectAllowed = 'copyMove'
  dt.setData(MIME, '')
  dt.setData('text/plain', c.names.join('\n'))
  const ghost = document.createElement('div')
  ghost.className = 'drag-ghost'
  ghost.textContent = c.names[0]
  if (c.names.length > 1) {
    const badge = document.createElement('span')
    badge.textContent = String(c.names.length)
    ghost.append(badge)
  }
  document.body.append(ghost)
  dt.setDragImage(ghost, -12, -12)
  setTimeout(() => ghost.remove())
}

export const drop = () => (carried = null)

export function target(node: HTMLElement, t: Target) {
  let timer = 0
  const ok = (e: DragEvent) => carrying(e) && t.accepts(carried!)
  const off = () => {
    node.classList.remove('drop-over')
    clearTimeout(timer)
  }
  const enter = (e: DragEvent) => {
    if (!ok(e) || node.classList.contains('drop-over')) return
    node.classList.add('drop-over')
    if (t.spring) timer = setTimeout(t.spring, 800)
  }
  const over = (e: DragEvent) => {
    if (!ok(e)) return
    e.preventDefault()
    e.dataTransfer!.dropEffect = e.altKey ? 'copy' : 'move'
  }
  const leave = (e: DragEvent) => !node.contains(e.relatedTarget as Node) && off()
  const land = (e: DragEvent) => {
    if (!ok(e)) return
    e.preventDefault()
    e.stopPropagation()
    off()
    const c = carried!
    carried = null
    t.drop(c, e.altKey)
  }
  const on = [['dragenter', enter], ['dragover', over], ['dragleave', leave], ['drop', land]] as const
  for (const [k, f] of on) node.addEventListener(k, f as EventListener)
  return {
    update: (n: Target) => (t = n),
    destroy() {
      off()
      for (const [k, f] of on) node.removeEventListener(k, f as EventListener)
    },
  }
}
