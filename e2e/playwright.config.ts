import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  timeout: 180_000,
  fullyParallel: true,
  use: { locale: 'zh-CN' },
  projects: [
    { name: 'chromium', use: devices['Desktop Chrome'] },
    { name: 'webkit', use: devices['Desktop Safari'], grep: /hostile book|comics open|EPUB opens/ },
  ],
})
