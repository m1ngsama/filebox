import { SvelteSet } from 'svelte/reactivity'

export const tree = { open: new SvelteSet<string>(), bare: new SvelteSet<string>() }
