import './app.css'

window.addEventListener('vite:preloadError', () => {
  try {
    if (Date.now() - Number(sessionStorage.getItem('reloadedAt')) < 60_000) return
    sessionStorage.setItem('reloadedAt', String(Date.now()))
  } catch {
    return
  }
  location.reload()
})
