// Touch gestures for lists, loaded only on touch screens: swipe a row sideways to act on it,
// pull the list down from the top to reload it, and, after a long press has picked an item, slide the finger over
// others to pick the whole stretch (the list scrolls itself when the finger nears an edge).
// The first two give a short buzz where the phone allows.

const lock = 12
const arm = 64
const most = 104

export type Gestures = {
  scroller: HTMLElement
  onswipe: (i: number, dir: 1 | -1) => void
  onpull?: () => Promise<unknown> | void
  // Is a long press holding the finger down on a picked item right now?
  ranging?: () => boolean
  // The finger is over the item at this index, or -1 once it has lifted.
  onrange?: (i: number) => void
}

const buzz = () => navigator.vibrate?.(8)

export function attach({ scroller, onswipe, onpull, ranging, onrange }: Gestures) {
  let x0 = 0
  let y0 = 0
  let mode: '' | 'swipe' | 'pull' | 'range' | 'scroll' = ''
  let row: HTMLElement | null = null
  let armed = false
  let pulled = 0
  let fx = 0
  let fy = 0
  let frame = 0
  const pull = document.createElement('div')
  pull.className = 'pull'
  pull.setAttribute('aria-hidden', 'true')
  scroller.prepend(pull)

  const resist = (d: number) => Math.sign(d) * Math.min(most, Math.abs(d) < arm ? Math.abs(d) : arm + (Math.abs(d) - arm) * 0.35)

  function start(e: TouchEvent) {
    if (e.touches.length !== 1) return (mode = 'scroll')
    x0 = e.touches[0].clientX
    y0 = e.touches[0].clientY
    mode = ''
    armed = false
    row = (e.target as Element).closest<HTMLElement>('.row[data-i]')
  }

  function over() {
    const i = document.elementFromPoint(fx, fy)?.closest<HTMLElement>('[data-i]')?.dataset.i
    if (i !== undefined) onrange?.(Number(i))
  }

  function follow() {
    const r = scroller.getBoundingClientRect()
    const edge = 56
    const v = fy < r.top + edge ? -(r.top + edge - fy) : fy > r.bottom - edge ? fy - (r.bottom - edge) : 0
    if (v) {
      scroller.scrollTop += Math.sign(v) * Math.min(18, 3 + Math.abs(v) / 4)
      over()
    }
    frame = requestAnimationFrame(follow)
  }

  function move(e: TouchEvent) {
    if (mode === 'scroll' || e.touches.length !== 1) return
    const dx = e.touches[0].clientX - x0
    const dy = e.touches[0].clientY - y0
    if (!mode && ranging?.() && (Math.abs(dx) > 4 || Math.abs(dy) > 4)) {
      mode = 'range'
      frame = requestAnimationFrame(follow)
    }
    if (mode === 'range') {
      fx = e.touches[0].clientX
      fy = e.touches[0].clientY
      e.preventDefault()
      return over()
    }
    if (!mode) {
      if (row && Math.abs(dx) > lock && Math.abs(dx) > Math.abs(dy) * 1.5) mode = 'swipe'
      else if (onpull && scroller.scrollTop <= 0 && dy > lock && dy > Math.abs(dx) * 1.5) mode = 'pull'
      else if (Math.abs(dx) > lock || Math.abs(dy) > lock) return void (mode = 'scroll')
      else return
    }
    e.preventDefault()
    if (mode === 'swipe' && row) {
      const d = resist(dx)
      row.classList.add('swiping')
      row.classList.toggle('left', d < 0)
      row.style.setProperty('--sx', `${d}px`)
      setArmed(Math.abs(d) >= arm, row)
    } else if (mode === 'pull') {
      pulled = Math.max(0, resist(dy * 0.6))
      pull.style.setProperty('--py', `${pulled}px`)
      pull.classList.add('shown')
      setArmed(pulled >= arm, pull)
    }
  }

  function setArmed(on: boolean, el: HTMLElement) {
    if (on && !armed) buzz()
    armed = on
    el.classList.toggle('armed', on)
  }

  // A touch the system took away, for a call or its own gesture, ends without doing anything.
  async function end(e: TouchEvent) {
    const was = mode
    mode = ''
    cancelAnimationFrame(frame)
    if (was === 'range') onrange?.(-1)
    if (e.type === 'touchcancel') armed = false
    if (was === 'swipe' && row) {
      const r = row
      const dir = (parseFloat(r.style.getPropertyValue('--sx')) || 0) < 0 ? -1 : 1
      r.classList.remove('swiping', 'armed', 'left')
      r.style.removeProperty('--sx')
      if (armed) {
        e.preventDefault()
        onswipe(Number(r.dataset.i), dir)
      }
    } else if (was === 'pull') {
      if (armed && onpull) {
        pull.classList.add('busy')
        pull.style.setProperty('--py', `${arm}px`)
        try {
          await onpull()
        } finally {
          pull.classList.remove('busy')
        }
      }
      pull.classList.remove('shown', 'armed')
      pull.style.removeProperty('--py')
    }
    armed = false
    row = null
  }

  scroller.addEventListener('touchstart', start, { passive: true })
  scroller.addEventListener('touchmove', move, { passive: false })
  scroller.addEventListener('touchend', end)
  scroller.addEventListener('touchcancel', end)
  return () => {
    scroller.removeEventListener('touchstart', start)
    scroller.removeEventListener('touchmove', move)
    scroller.removeEventListener('touchend', end)
    scroller.removeEventListener('touchcancel', end)
    pull.remove()
  }
}
