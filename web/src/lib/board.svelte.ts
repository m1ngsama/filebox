import type { Carried } from './dnd'

export const board = $state<{ items: Carried | null }>({ items: null })
