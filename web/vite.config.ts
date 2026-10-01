import { defineConfig, type Plugin } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { cpSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { brotliCompressSync, gzipSync, constants } from 'node:zlib'

const api = 'http://127.0.0.1:5280'

const precompress = (): Plugin => ({
  name: 'precompress',
  apply: 'build',
  writeBundle({ dir = 'dist' }) {
    const assets = join(dir, 'assets')
    for (const f of readdirSync(assets)) {
      const p = join(assets, f)
      const b = readFileSync(p)
      const variants: [string, Buffer][] = [
        ['.br', brotliCompressSync(b, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: b.length } })],
        ['.gz', gzipSync(b, { level: 9 })],
      ]
      for (const [ext, z] of variants) if (z.length < b.length) writeFileSync(p + ext, z)
    }
  },
})

const pdfjs = JSON.parse(readFileSync('node_modules/pdfjs-dist/package.json', 'utf8')).version as string

const pdfData = (): Plugin => ({
  name: 'pdf-data',
  apply: 'build',
  writeBundle({ dir = 'dist' }) {
    for (const d of ['cmaps', 'standard_fonts']) cpSync(join('node_modules/pdfjs-dist', d), join(dir, 'pdfjs', pdfjs, d), { recursive: true })
  },
})

export default defineConfig(({ mode }) => ({
  plugins: [svelte(), precompress(), ...(mode === 'index' ? [pdfData()] : [])],
  define: { PDFJS_DATA: JSON.stringify(`/pdfjs/${pdfjs}/`) },
  build: {
    outDir: 'dist',
    emptyOutDir: mode === 'index',
    target: 'es2022',
    rolldownOptions: {
      input: mode === 'index' ? 'index.html' : `${mode}.html`,
      output: { codeSplitting: { groups: [{ name: mode === 'index' ? 'app' : mode, tags: ['$initial'] }] } },
    },
  },
  server: { proxy: Object.fromEntries(['/api', '/raw', '/thumb', '/upload', '/s/'].map((p) => [p, api])) },
}))
