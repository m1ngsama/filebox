import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

const api = 'http://127.0.0.1:5280'

export default defineConfig({
  plugins: [svelte()],
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  server: { proxy: Object.fromEntries(['/api', '/raw', '/thumb', '/upload', '/s/'].map((p) => [p, api])) },
})
