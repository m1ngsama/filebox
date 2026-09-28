import { defineConfig, type Plugin } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
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

export default defineConfig({
  plugins: [svelte(), precompress()],
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  server: { proxy: Object.fromEntries(['/api', '/raw', '/thumb', '/upload', '/s/'].map((p) => [p, api])) },
})
