import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'

window.addEventListener('vite:preloadError', () => {
  try {
    if (Date.now() - Number(sessionStorage.getItem('reloadedAt')) < 60_000) return
    sessionStorage.setItem('reloadedAt', String(Date.now()))
  } catch {
    return
  }
  location.reload()
})

mount(App, { target: document.getElementById('app')! })
