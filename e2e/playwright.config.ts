import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const DIR = join(tmpdir(), 'filebox-e2e')

export default defineConfig({
  testDir: '.',
  timeout: 180_000,
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:5298' },
  webServer: {
    command: 'sh start.sh',
    url: 'http://127.0.0.1:5298/',
    env: { E2E_DIR: DIR },
    reuseExistingServer: false,
  },
})
