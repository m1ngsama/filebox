import { MediaQuery } from 'svelte/reactivity'

export const shell = $state({ nav: false })
export const narrow = new MediaQuery('max-width: 767px')
