export type ToastAction = { label: string; run: () => unknown; keys?: string }
export type Toast = { id: number; text: string; kind: 'success' | 'info' | 'error'; actions: ToastAction[] }

export const toasts = $state<Toast[]>([])
const timers = new Map<number, { left: number; start: number; id: number }>()
let seq = 0
let paused = false
const holds = { hover: false, focus: false }
let leaving: (id: number) => void = () => {}
let kept: ToastAction | undefined

export const onLeave = (fn: typeof leaving) => (leaving = fn)

export function toast(text: string, o: { kind?: Toast['kind']; actions?: (ToastAction | undefined)[]; ms?: number } = {}) {
  const id = ++seq
  const actions = (o.actions ?? []).filter((a) => !!a)
  kept = actions.find((a) => a.keys) ?? kept
  toasts.push({ id, text, kind: o.kind ?? 'success', actions })
  if (toasts.length > 3) dismiss((toasts.find((x) => !x.actions.length) ?? toasts[0]).id)
  timers.set(id, { left: o.ms ?? (actions.length || o.kind === 'error' ? 8000 : 4000), start: 0, id: 0 })
  if (!paused) arm(id)
  return id
}

export function act(id: number, a: ToastAction) {
  dismiss(id)
  if (kept?.run === a.run) kept = undefined
  a.run()
}

export function runLatest(label: string) {
  for (let i = toasts.length - 1; i >= 0; i--) {
    const a = toasts[i].actions.find((x) => x.label === label)
    if (a) return act(toasts[i].id, a), true
  }
  if (kept?.label !== label) return false
  const a = kept
  kept = undefined
  a.run()
  return true
}

export function retract(id: number) {
  const x = toasts.find((x) => x.id === id)
  if (x?.actions.some((a) => a.run === kept?.run)) kept = undefined
  dismiss(id)
}

export function retext(id: number, text: string) {
  const x = toasts.find((x) => x.id === id)
  if (x) x.text = text
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
