import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  timeout: 180_000,
  fullyParallel: true,
  retries: process.env.CI ? 1 : 0,
  use: { locale: 'zh-CN' },
  projects: [
    { name: 'chromium', use: devices['Desktop Chrome'] },
    ...(process.env.CI ? [] : [{ name: 'webkit', use: devices['Desktop Safari'], grep: /hostile book|comics open|EPUB opens/ }]),
  ],
})
