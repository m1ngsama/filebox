// A thumb on the right edge of a long list. It shows where you are, and dragging it moves the list with it, naming the spot in a
// bubble beside it (a month, a letter, a size), as in Telegram. The list is long when it is more than two screens tall.

const THUMB = 56

export type Scrub = { el: HTMLElement; label: () => string }

export function scrub(node: HTMLElement, o: Scrub) {
  const thumb = node.children[0] as HTMLElement
  const bubble = node.children[1] as HTMLElement
  let el = o.el
  let label = o.label
  let dragging = false
  let fade = 0

  const sync = () => {
    const room = el.scrollHeight - el.clientHeight
    node.hidden = room < el.clientHeight * 2
    if (node.hidden) return
    const p = Math.min(1, Math.max(0, el.scrollTop / room))
    node.style.setProperty('--y', `${p * (el.clientHeight - THUMB)}px`)
    if (dragging) bubble.textContent = label()
  }
  const wake = () => {
    node.classList.add('active')
    clearTimeout(fade)
    fade = window.setTimeout(() => node.classList.remove('active'), 900)
    sync()
  }
  const move = (e: PointerEvent) => {
    const r = el.getBoundingClientRect()
    const p = Math.min(1, Math.max(0, (e.clientY - r.top - THUMB / 2) / (el.clientHeight - THUMB)))
    el.scrollTop = p * (el.scrollHeight - el.clientHeight)
  }
  const down = (e: PointerEvent) => {
    dragging = true
    node.classList.add('drag')
    thumb.setPointerCapture(e.pointerId)
    sync()
  }
  const up = () => {
    dragging = false
    node.classList.remove('drag')
    wake()
  }

  const watch = new ResizeObserver(sync)
  const attach = () => {
    el.addEventListener('scroll', wake, { passive: true })
    el.addEventListener('pointerenter', sync)
    watch.observe(el)
    sync()
  }
  attach()
  thumb.addEventListener('pointerdown', down)
  thumb.addEventListener('pointermove', (e) => dragging && move(e))
  thumb.addEventListener('pointerup', up)
  thumb.addEventListener('pointercancel', up)

  return {
    update(n: Scrub) {
      label = n.label
      if (n.el !== el) {
        el.removeEventListener('scroll', wake)
        el.removeEventListener('pointerenter', sync)
        watch.disconnect()
        el = n.el
        attach()
      }
      sync()
    },
    destroy() {
      el.removeEventListener('scroll', wake)
      el.removeEventListener('pointerenter', sync)
      watch.disconnect()
      clearTimeout(fade)
    },
  }
}
