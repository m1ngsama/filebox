export type Toast = { id: number; text: string; kind: 'success' | 'info' | 'error'; action?: { label: string; run: () => unknown; keys?: string } }

export const toasts = $state<Toast[]>([])
const timers = new Map<number, { left: number; start: number; id: number }>()
let seq = 0
let paused = false
const holds = { hover: false, focus: false }
let leaving: (id: number) => void = () => {}

export const onLeave = (fn: typeof leaving) => (leaving = fn)

export function toast(text: string, o: { kind?: Toast['kind']; action?: Toast['action']; ms?: number } = {}) {
  const id = ++seq
  toasts.push({ id, text, kind: o.kind ?? 'success', action: o.action })
  if (toasts.length > 3) dismiss((toasts.find((x) => !x.action) ?? toasts[0]).id)
  timers.set(id, { left: o.ms ?? (o.action || o.kind === 'error' ? 8000 : 4000), start: 0, id: 0 })
  if (!paused) arm(id)
  return id
}

export function runLatest(label: string) {
  for (let i = toasts.length - 1; i >= 0; i--) {
    const a = toasts[i].action
    if (a?.label !== label) continue
    dismiss(toasts[i].id)
    a.run()
    return true
  }
  return false
}

export const fail = (e: unknown) => toast((e as Error).message, { kind: 'error' })

function arm(id: number) {
  const tm = timers.get(id)
  if (!tm) return
  tm.start = Date.now()
  tm.id = setTimeout(() => dismiss(id), tm.left)
}

export function dismiss(id: number) {
  clearTimeout(timers.get(id)?.id)
  timers.delete(id)
  const i = toasts.findIndex((x) => x.id === id)
  if (i < 0) return
  leaving(id)
  toasts.splice(i, 1)
}

export function hold(why: keyof typeof holds, on: boolean) {
  holds[why] = on
  pause(holds.hover || holds.focus)
}

function pause(on: boolean) {
  if (on === paused) return
  paused = on
  for (const [id, tm] of timers) {
    if (!on) arm(id)
    else {
      clearTimeout(tm.id)
      tm.left = Math.max(1000, tm.left - (Date.now() - tm.start))
    }
  }
}
