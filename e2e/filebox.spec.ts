import { test as base, expect, type Locator, type Page } from '@playwright/test'
import { execFileSync, spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { closeSync, cpSync, createReadStream, existsSync, mkdirSync, mkdtempSync, openSync, readFileSync, rmSync, statSync, writeFileSync, writeSync } from 'node:fs'
import { createServer, type AddressInfo } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { t } from '../web/src/lib/i18n'

const BIN = join(import.meta.dirname, '../bin/filebox')

function freePort() {
  return new Promise<number>((resolve) => {
    const s = createServer().listen(0, '127.0.0.1', () => {
      const { port } = s.address() as AddressInfo
      s.close(() => resolve(port))
    })
  })
}

const test = base.extend<{ server: { url: string; vol: string } }, { template: string }>({
  template: [
    async ({}, use) => {
      const dir = mkdtempSync(join(tmpdir(), 'filebox-e2e-'))
      execFileSync(BIN, ['passwd', '-data', dir], { input: 'pw-pw-pw-pw\n', stdio: ['pipe', 'ignore', 'ignore'] })
      await use(dir)
      rmSync(dir, { recursive: true, force: true })
    },
    { scope: 'worker' },
  ],
  server: async ({ template }, use) => {
    const dir = mkdtempSync(join(tmpdir(), 'filebox-e2e-'))
    const vol = join(dir, 'vol')
    mkdirSync(join(vol, 'docs'), { recursive: true })
    writeFileSync(join(vol, 'docs/readme.txt'), 'hello\n')
    cpSync(template, join(dir, 'data'), { recursive: true })
    const port = await freePort()
    const proc = spawn(BIN, ['serve', '-data', join(dir, 'data'), '-listen', `127.0.0.1:${port}`, '-origin', `http://localhost:${port}`, '-vol', `v=${vol}`], { stdio: 'ignore' })
    const exited = new Promise((r) => proc.once('exit', r))
    const url = `http://127.0.0.1:${port}`
    await expect.poll(() => fetch(url).then((r) => r.ok, () => false)).toBe(true)
    await use({ url, vol })
    proc.kill('SIGKILL')
    await exited
    rmSync(dir, { recursive: true, force: true })
  },
  baseURL: async ({ server }, use) => use(server.url),
})

const fileInput = (p: Page) => p.locator('input[type=file]:not([webkitdirectory])')
const row = (p: Page, name: string) => p.locator('.row', { hasText: name })
const unlabeled = (p: Page) => p.evaluate(() => [...document.querySelectorAll('input:not([type=file]):not([type=checkbox])')].filter((i) => !(i as HTMLInputElement).labels?.length).length)

async function login(page: Page) {
  await page.goto('/')
  await signIn(page)
}

async function signIn(page: Page) {
  await expect(page.getByLabel(t.username)).toBeVisible()
  expect(await unlabeled(page)).toBe(0)
  await page.getByLabel(t.username).fill('admin')
  await page.getByLabel(t.password, { exact: true }).fill('pw-pw-pw-pw')
  await page.getByRole('button', { name: t.login, exact: true }).click()
  await expect(page).toHaveURL(/\/files\/v\/$/)
}

function sha(p: string) {
  return new Promise<string>((resolve) => {
    const h = createHash('sha256')
    createReadStream(p).on('data', (d) => h.update(d)).on('end', () => resolve(h.digest('hex')))
  })
}

function bigFile(mb: number) {
  const p = join(tmpdir(), `filebox-e2e-${mb}.bin`)
  if (existsSync(p) && statSync(p).size === mb << 20) return p
  const fd = openSync(p, 'w')
  const buf = Buffer.alloc(1 << 20)
  for (let i = 0; i < mb; i++) {
    buf.fill(i % 251)
    buf.writeUInt32LE(i, 0)
    writeSync(fd, buf)
  }
  closeSync(fd)
  return p
}

async function shareDocs(page: Page, mode: 'read' | 'upload' | 'drop', password = '') {
  await row(page, 'docs').locator('button.more').click()
  const loaded = page.waitForResponse((r) => r.request().method() === 'GET' && new URL(r.url()).pathname === '/api/shares')
  await page.getByRole('menuitem', { name: t.share, exact: true }).click()
  await loaded
  await page.getByRole('radio', { name: t.modes[mode], exact: true }).check()
  if (password) await page.getByLabel(t.passwordOptional).fill(password)
  const links = page.locator('.details .shares li a')
  await page.getByRole('button', { name: t.newShare, exact: true }).click()
  await expect(links).toHaveCount(1)
  const url = await links.getAttribute('href')
  await page.locator('.details-close').click()
  return url!
}

test('wrong password is rejected', async ({ page }) => {
  await page.goto('/')
  await page.getByPlaceholder(t.username).fill('admin')
  await page.getByPlaceholder(t.password).fill('nope-nope')
  await page.getByRole('button', { name: t.showPassword }).click()
  await expect(page.getByLabel(t.password, { exact: true })).toHaveAttribute('type', 'text')
  await page.getByRole('button', { name: t.login, exact: true }).click()
  await expect(page.locator('form').getByRole('alert')).toHaveText(t.wrongLogin)
  await expect(page.getByLabel(t.password, { exact: true })).toBeFocused()
})

test('the login page stays quiet about passkeys until they are used', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByLabel(t.username)).toBeFocused()
  await page.waitForTimeout(500)
  await expect(page.locator('.login .error')).toHaveCount(0)
})

test('browse and preview', async ({ page }) => {
  await login(page)
  await expect(row(page, 'docs')).toBeVisible()
  expect(await unlabeled(page)).toBe(0)
  await row(page, 'docs').locator('button.name').click()
  await expect(page).toHaveURL(/\/files\/v\/docs\/$/)
  await row(page, 'readme.txt').locator('button.name').click()
  await expect(page.locator('.viewer pre')).toHaveText('hello\n')
  await page.keyboard.press('Escape')
  await page.locator('.crumbs a').first().click()
  await expect(page).toHaveURL(/\/files\/v\/$/)
})

test('a slow folder shows skeleton rows and a broken image offers a download', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'docs/broken.jpg'), 'not an image')
  await login(page)
  let release = () => {}
  const held = new Promise<void>((r) => (release = r))
  await page.route('**/api/ls?*', async (r) => {
    if (r.request().url().includes('docs')) await held
    await r.continue()
  })
  await row(page, 'docs').locator('button.name').click()
  await expect(page.locator('.skeleton')).toBeVisible()
  release()
  await expect(page.locator('.skeleton')).toHaveCount(0)
  await row(page, 'broken.jpg').locator('button.name').click()
  const viewer = page.getByRole('dialog', { name: 'broken.jpg' })
  await expect(viewer.getByRole('alert')).toContainText(t.previewFailed)
  await expect(viewer.getByRole('alert').getByRole('link', { name: t.download })).toHaveAttribute('href', /broken\.jpg\?dl$/)
  await expect(viewer.getByRole('button', { name: t.close })).toBeFocused()
})

test('without thumbnails small images fall back to the original and others get a typed icon', async ({ page, server }) => {
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64')
  writeFileSync(join(server.vol, 'docs/pic.png'), png)
  writeFileSync(join(server.vol, 'docs/big.png'), Buffer.concat([png, Buffer.alloc(3 << 20)]))
  writeFileSync(join(server.vol, 'docs/code.go'), 'package main')
  await login(page)
  await row(page, 'docs').locator('button.name').click()
  await expect(row(page, 'pic.png').locator('img')).toHaveAttribute('src', '/raw/v/docs/pic.png')
  await expect.poll(() => row(page, 'pic.png').locator('img').evaluate((i: HTMLImageElement) => i.naturalWidth)).toBe(1)
  await expect(row(page, 'big.png').locator('img')).toHaveCount(0)
  await expect(row(page, 'big.png').locator('svg.ficon.image')).toHaveCount(1)
  await expect(row(page, 'code.go').locator('svg.ficon.code')).toHaveCount(1)
})

test('the file list loads one script and settings loads its own chunk', async ({ page }) => {
  const scripts: string[] = []
  page.on('response', (r) => {
    if (r.url().endsWith('.js')) scripts.push(r.url())
  })
  await login(page)
  await expect(row(page, 'docs')).toBeVisible()
  expect(scripts).toHaveLength(1)
  const js = await page.request.get(scripts[0], { headers: { 'Accept-Encoding': 'br' } })
  expect(js.headers()['content-encoding']).toBe('br')
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: t.settings })).toBeVisible()
  await expect.poll(() => scripts.length).toBeGreaterThan(1)
})

test('the theme choice applies before the app loads', async ({ page }) => {
  const blocked: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error') blocked.push(m.text())
  })
  await page.emulateMedia({ colorScheme: 'light' })
  await login(page)
  await page.goto('/settings')
  await page.getByRole('button', { name: t.themes.dark }).click()
  const bg = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  await expect.poll(bg).toBe('rgb(21, 24, 29)')
  await page.route('**/assets/*.js', (r) => r.abort())
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  expect(await bg()).toBe('rgb(21, 24, 29)')
  await page.unroute('**/assets/*.js')
  await page.goto('/settings')
  await page.getByRole('button', { name: t.themes.system }).click()
  await expect(page.locator('html')).not.toHaveAttribute('data-theme')
  expect(await bg()).toBe('rgb(255, 255, 255)')
  expect(blocked.filter((m) => m.includes('Content Security Policy'))).toEqual([])
})

test('names sort naturally with folders first and the filter narrows them', async ({ page, server }) => {
  mkdirSync(join(server.vol, 'mix/zdir'), { recursive: true })
  for (const n of ['a10.txt', 'a2.txt', 'B1.txt']) writeFileSync(join(server.vol, 'mix', n), n)
  await login(page)
  await row(page, 'mix').locator('button.name').click()
  const names = page.locator('.row button.name')
  await expect(names).toHaveText(['zdir', 'a2.txt', 'a10.txt', 'B1.txt'])
  await page.locator('button.sort.name').click()
  await expect(names).toHaveText(['zdir', 'B1.txt', 'a10.txt', 'a2.txt'])
  await page.getByLabel(t.filter).fill('A1')
  await expect(names).toHaveText(['a10.txt'])
  await page.getByLabel(t.filter).fill('')
  await expect(names).toHaveCount(4)
})

test('empty folders and empty filters say what to do next', async ({ page, server }) => {
  mkdirSync(join(server.vol, 'hollow'))
  await login(page)
  await page.getByLabel(t.filter).fill('zzz')
  await expect(page.getByText(t.noMatch)).toBeVisible()
  await page.getByRole('button', { name: t.clearFilter }).click()
  await expect(page.getByLabel(t.filter)).toHaveValue('')
  await expect(row(page, 'docs')).toBeVisible()
  await row(page, 'hollow').locator('button.name').click()
  await expect(page.getByText(t.folderEmpty)).toBeVisible()
  const chooser = page.waitForEvent('filechooser')
  await page.locator('.empty-state').getByRole('button', { name: t.upload }).click()
  await chooser
  await page.goto('/shares')
  await expect(page.getByText(t.noShares)).toBeVisible()
})

test('theme colours do not need light-dark() support', async ({ page }) => {
  await login(page)
  const css = await page.evaluate(() => Promise.all([...document.styleSheets].map((s) => fetch(s.href!).then((r) => r.text()))))
  expect(css.join('')).not.toContain('light-dark(')
  const light = 'rgb(255, 255, 255)'
  const dark = 'rgb(21, 24, 29)'
  for (const [scheme, theme, want] of [
    ['light', null, light],
    ['dark', null, dark],
    ['light', 'dark', dark],
    ['dark', 'light', light],
  ] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    const bg = await page.evaluate((v) => {
      if (v) document.documentElement.dataset.theme = v
      else delete document.documentElement.dataset.theme
      return getComputedStyle(document.body).backgroundColor
    }, theme)
    expect(bg, `${scheme} system, ${theme ?? 'no'} override`).toBe(want)
  }
})

test('a missing chunk reloads the page once, then offers a retry', async ({ page }) => {
  await login(page)
  let aborted = 0
  await page.route(/\/assets\/Settings-.*\.js$/, (r) => (aborted++ ? r.continue() : r.abort()))
  const loads: string[] = []
  page.on('load', () => loads.push(page.url()))
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: t.appearance })).toBeVisible()
  expect(aborted).toBe(2)
  expect(loads).toHaveLength(2)
  await page.unroute(/Settings/)
  await page.route(/\/assets\/Settings-.*\.js$/, (r) => r.abort())
  await page.goto('/recent')
  await page.getByRole('link', { name: t.settings }).click()
  await expect(page.getByText(t.loadFailed)).toBeVisible()
  await expect(page.getByRole('button', { name: t.retry })).toBeVisible()
})

test('filled buttons keep AA contrast in both themes, hovered or not', async ({ page }) => {
  const contrast = (l: Locator) =>
    l.evaluate((el) => {
      const lum = (c: string) => {
        const ctx = document.createElement('canvas').getContext('2d')!
        ctx.fillStyle = c
        ctx.fillRect(0, 0, 1, 1)
        const [r, g, b] = [...ctx.getImageData(0, 0, 1, 1).data].map((v) => ((v /= 255) <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
      }
      const s = getComputedStyle(el)
      const [hi, lo] = [lum(s.color), lum(s.backgroundColor)].sort((a, b) => b - a)
      return (hi + 0.05) / (lo + 0.05)
    })
  await login(page)
  await row(page, 'docs').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  const danger = page.locator('.dialog').getByRole('button', { name: t.remove, exact: true })
  const primary = page.getByRole('button', { name: t.new })
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.mouse.move(0, 0)
    expect(await contrast(primary), `${scheme} primary`).toBeGreaterThanOrEqual(4.5)
    expect(await contrast(danger), `${scheme} danger`).toBeGreaterThanOrEqual(4.5)
    await danger.hover()
    expect(await contrast(danger), `${scheme} danger hovered`).toBeGreaterThanOrEqual(4.5)
  }
})

test('rows select and open from the keyboard', async ({ page, server }) => {
  mkdirSync(join(server.vol, 'many'), { recursive: true })
  for (let i = 0; i < 60; i++) writeFileSync(join(server.vol, 'many', `k-${String(i).padStart(2, '0')}.txt`), `k${i}`)
  await login(page)
  await row(page, 'many').locator('button.name').click()
  const first = row(page, 'k-00.txt')
  await first.focus()
  await page.keyboard.press('Space')
  await expect(first).toHaveAttribute('aria-selected', 'true')
  await expect(first.locator('input[type=checkbox]')).toBeChecked()
  await page.keyboard.press('ArrowDown')
  await expect(row(page, 'k-01.txt')).toBeFocused()
  await expect(row(page, 'k-01.txt')).toHaveAttribute('aria-selected', 'false')
  await page.keyboard.press('End')
  const last = row(page, 'k-59.txt')
  await expect(last).toBeFocused()
  await expect(last).toBeInViewport()
  await page.keyboard.press('Space')
  await expect(page.getByRole('grid', { name: t.fileList }).getByRole('row', { selected: true })).toHaveCount(1)
  await page.keyboard.press('Home')
  await expect(first).toBeFocused()
  await expect(first).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Enter')
  await expect(page.locator('.viewer pre')).toHaveText('k0')
})

test('resumable upload survives a dropped connection and a page reload', async ({ page, server }) => {
  const src = bigFile(200)
  const total = statSync(src).size
  await login(page)
  let aborted = false
  let stalled = false
  let stall = () => {}
  const reached = new Promise<void>((r) => (stall = r))
  await page.route('**/upload/*', (route) => {
    if (route.request().method() !== 'PATCH') return route.continue()
    const offset = Number(route.request().headers()['upload-offset'])
    if (!aborted && offset > 0) {
      aborted = true
      return route.abort('connectionreset')
    }
    if (!stalled && offset >= total * 0.3) {
      stalled = true
      stall()
      return new Promise<void>(() => {})
    }
    return route.continue()
  })
  const offsets: number[] = []
  page.on('request', (r) => {
    if (r.method() === 'PATCH') offsets.push(Number(r.headers()['upload-offset']))
  })
  await fileInput(page).setInputFiles(src)
  await reached
  await expect(page.locator('.uploads li.uploading')).toHaveCount(1)
  await page.unrouteAll()
  await page.reload()
  offsets.length = 0
  await fileInput(page).setInputFiles(src)
  await expect(page.locator('.uploads li.done')).toHaveCount(1, { timeout: 120_000 })
  expect(offsets[0]).toBeGreaterThan(0)
  expect(await sha(join(server.vol, 'filebox-e2e-200.bin'))).toBe(await sha(src))
})

test('closing details opened from my shares clears the query', async ({ page }) => {
  await login(page)
  await shareDocs(page, 'read')
  await page.getByRole('link', { name: t.myShares, exact: true }).click()
  await page.locator('.rows li a', { hasText: 'v:/docs' }).click()
  await expect(page).toHaveURL(/\/files\/v\/\?details=docs$/)
  await expect(page.locator('.details h2')).toHaveText('docs')
  await page.locator('.details-close').click()
  await expect(page).toHaveURL(/\/files\/v\/$/)
  await page.goBack()
  await page.goForward()
  await expect(page).toHaveURL(/\/files\/v\/$/)
  await page.reload()
  await expect(row(page, 'docs')).toBeVisible()
  await expect(page.locator('.details')).toHaveCount(0)
})

test('an upload that loses the session returns to login and resumes after it', async ({ page, context, server }) => {
  const src = bigFile(160)
  await login(page)
  let expired = false
  await page.route('**/upload/*', async (route) => {
    if (expired || route.request().method() !== 'PATCH') return route.continue()
    const response = await route.fetch()
    await context.clearCookies()
    expired = true
    await route.fulfill({ response })
  })
  await fileInput(page).setInputFiles(src)
  await expect(page.getByRole('button', { name: t.login, exact: true })).toBeVisible({ timeout: 60_000 })
  await page.unrouteAll()
  await signIn(page)
  await expect(page.locator('.uploads li.error')).toContainText(t.errors[401])
  const offsets: number[] = []
  page.on('request', (r) => {
    if (r.method() === 'PATCH') offsets.push(Number(r.headers()['upload-offset']))
  })
  await fileInput(page).setInputFiles(src)
  await expect(page.locator('.uploads li.done')).toHaveCount(1, { timeout: 120_000 })
  expect(offsets[0]).toBeGreaterThan(0)
  expect(await sha(join(server.vol, 'filebox-e2e-160.bin'))).toBe(await sha(src))
})

test('password share opens anonymously', async ({ page, browser }) => {
  await login(page)
  const url = await shareDocs(page, 'read', 'secret-pass')
  const anon = await browser.newPage()
  await anon.goto(url)
  await expect(anon.getByLabel(t.password, { exact: true })).toBeVisible()
  expect(await unlabeled(anon)).toBe(0)
  await expect(anon.locator('body')).not.toContainText('docs')
  await anon.getByLabel(t.password, { exact: true }).fill('secret-pass')
  await anon.getByRole('button', { name: t.unlock, exact: true }).click()
  await row(anon, 'readme.txt').locator('button.name').click()
  await expect(anon.locator('.viewer pre')).toHaveText('hello\n')
  expect((await anon.request.get(`${url}/raw/readme.txt`)).status()).toBe(200)
  await anon.close()
})

test('repeated wrong share passwords say when to retry', async ({ page, browser }) => {
  await login(page)
  const url = await shareDocs(page, 'read', 'other-pass')
  const anon = await browser.newPage()
  await anon.goto(url)
  for (let fails = 0; fails < 7; ) {
    if ((await anon.request.post(`${url}/unlock`, { data: { password: 'wrong' } })).status() === 401) fails++
    else await anon.waitForTimeout(250)
  }
  await anon.getByPlaceholder(t.password).fill('wrong')
  await anon.getByRole('button', { name: t.unlock, exact: true }).click()
  await expect(anon.getByText(new RegExp(`^${t.tooMany(9).replace('9', '\\d+')}$`))).toBeVisible()
  await anon.close()
})

test('malformed share token never reaches the API', async ({ page }) => {
  await login(page)
  const paths: string[] = []
  page.on('request', (r) => paths.push(new URL(r.url()).pathname))
  await page.goto('/s/..%2Fapi%2Flogout%3F')
  await expect(page.getByText(t.shareGone)).toBeVisible()
  expect(paths.filter((p) => p.startsWith('/api/'))).toEqual([])
})

test('drop share accepts uploads and hides contents', async ({ page, browser, server }) => {
  await login(page)
  const url = await shareDocs(page, 'drop')
  const anon = await browser.newPage()
  await anon.goto(url)
  await expect(anon.getByText(t.dropHint)).toBeVisible()
  await expect(anon.getByText('readme.txt')).toHaveCount(0)
  await fileInput(anon).setInputFiles({ name: 'hello.txt', mimeType: 'text/plain', buffer: Buffer.from('dropped') })
  await expect(anon.locator('.uploads li.done')).toHaveCount(1)
  expect(readFileSync(join(server.vol, 'docs/hello.txt'), 'utf8')).toBe('dropped')
  await anon.close()
})

test('delete and restore from trash', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'tmp.txt'), 'x')
  await login(page)
  await row(page, 'tmp.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(row(page, 'tmp.txt')).toHaveCount(0)
  await page.getByRole('link', { name: t.trash, exact: true }).click()
  await page.getByRole('button', { name: t.restore, exact: true }).click()
  await expect.poll(() => existsSync(join(server.vol, 'tmp.txt'))).toBe(true)
})

test('undo from the keyboard keeps focus and lets later toasts expire', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'kb.txt'), 'k')
  await login(page)
  await row(page, 'kb.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  const undo = page.locator('.toast').getByRole('button', { name: t.undo, exact: true })
  await undo.focus()
  await page.keyboard.press('Enter')
  await expect(row(page, 'kb.txt')).toHaveCount(1)
  expect(await page.evaluate(() => document.activeElement !== document.body)).toBe(true)
  await expect(page.locator('.toast', { hasText: t.undone })).toHaveCount(1)
  await expect(page.locator('.toast', { hasText: t.undone })).toHaveCount(0, { timeout: 7000 })
  await row(page, 'kb.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(row(page, 'kb.txt')).toHaveCount(0)
  await page.keyboard.press('ControlOrMeta+z')
  await expect(row(page, 'kb.txt')).toHaveCount(1)
})

test('ctrl+z on a focused undo button keeps focus and lets later toasts expire', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'kz.txt'), 'k')
  await login(page)
  await row(page, 'kz.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.toast').getByRole('button', { name: t.undo, exact: true }).focus()
  await page.keyboard.press('ControlOrMeta+z')
  await expect(row(page, 'kz.txt')).toHaveCount(1)
  expect(await page.evaluate(() => document.activeElement !== document.body)).toBe(true)
  await expect(page.locator('.toast', { hasText: t.undone })).toHaveCount(1)
  await expect(page.locator('.toast', { hasText: t.undone })).toHaveCount(0, { timeout: 7000 })
})

test('an undo toast outlives newer plain toasts', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'keep.txt'), 'k')
  await login(page)
  await row(page, 'keep.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(page.locator('.toast').getByRole('button', { name: t.undo })).toHaveCount(1)
  for (let i = 0; i < 3; i++) {
    await fileInput(page).setInputFiles({ name: `n${i}.txt`, mimeType: 'text/plain', buffer: Buffer.from('n') })
    await expect(page.locator('.toast', { hasText: t.uploaded(1) })).not.toHaveCount(0)
  }
  await expect(page.locator('.toast').getByRole('button', { name: t.undo })).toHaveCount(1)
})

test('a partial undo restores what it can and names what it could not', async ({ page, server }) => {
  for (const n of ['p1.txt', 'p2.txt']) writeFileSync(join(server.vol, n), n)
  await login(page)
  for (const n of ['p1.txt', 'p2.txt']) await row(page, n).locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(row(page, 'p1.txt')).toHaveCount(0)
  writeFileSync(join(server.vol, 'p2.txt'), 'new')
  await page.locator('.toast').getByRole('button', { name: t.undo }).click()
  await expect(page.getByRole('alert').filter({ hasText: t.undoFailed('“p2.txt”') })).toHaveText(t.failedItem(t.undoFailed('“p2.txt”'), t.errors[409]))
  expect(readFileSync(join(server.vol, 'p1.txt'), 'utf8')).toBe('p1.txt')
  expect(readFileSync(join(server.vol, 'p2.txt'), 'utf8')).toBe('new')
})

test('delete, rename and move each offer an undo', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'undo.txt'), 'u')
  await login(page)
  const toast = page.locator('.toast')
  const undo = () => toast.getByRole('button', { name: t.undo, exact: true }).click()
  await row(page, 'undo.txt').locator('button.more').click()
  await page.getByRole('menuitem', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: t.trashed('“undo.txt”') })).toHaveText(t.trashed('“undo.txt”'))
  await expect(toast.getByRole('button', { name: t.undo })).toHaveAccessibleDescription(t.trashed('“undo.txt”'))
  await expect(row(page, 'undo.txt')).toHaveCount(0)
  await undo()
  await expect(row(page, 'undo.txt')).toHaveCount(1)
  await expect(toast.filter({ hasText: t.undone })).toHaveCount(1)

  await row(page, 'undo.txt').locator('button.more').click()
  await page.getByRole('menuitem', { name: t.rename, exact: true }).click()
  await page.getByLabel(t.newName).fill('renamed.txt')
  await page.locator('.dialog').getByRole('button', { name: t.rename, exact: true }).click()
  await expect(row(page, 'renamed.txt')).toHaveCount(1)
  await toast.filter({ hasText: t.renamed('renamed.txt') }).getByRole('button', { name: t.undo }).click()
  await expect(row(page, 'undo.txt')).toHaveCount(1)
  await expect(row(page, 'renamed.txt')).toHaveCount(0)

  await row(page, 'undo.txt').locator('button.more').click()
  await page.getByRole('menuitem', { name: t.moveOrCopy, exact: true }).click()
  await page.locator('.picker-list').getByRole('button', { name: 'docs' }).click()
  await page.locator('.dialog').getByRole('button', { name: t.move, exact: true }).click()
  await expect(row(page, 'undo.txt')).toHaveCount(0)
  expect(existsSync(join(server.vol, 'docs/undo.txt'))).toBe(true)
  await toast.filter({ hasText: t.movedTo('“undo.txt”', 'v:/docs') }).getByRole('button', { name: t.undo }).click()
  await expect(row(page, 'undo.txt')).toHaveCount(1)
  expect(existsSync(join(server.vol, 'undo.txt'))).toBe(true)
})

test('trash restores and permanently deletes a selection', async ({ page, server }) => {
  for (const n of ['t1.txt', 't2.txt', 't3.txt']) writeFileSync(join(server.vol, n), n)
  await login(page)
  for (const n of ['t1.txt', 't2.txt', 't3.txt']) await row(page, n).locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await page.getByRole('link', { name: t.trash, exact: true }).click()
  const rows = page.getByRole('list', { name: t.trash }).getByRole('listitem')
  await expect(rows).toHaveCount(3)
  await expect(rows.first()).toContainText(t.deletedAt(''))
  await expect(page.getByText(t.trashNote)).toBeVisible()
  await rows.filter({ hasText: 't1.txt' }).getByRole('checkbox').check()
  await rows.filter({ hasText: 't2.txt' }).getByRole('checkbox').check()
  await expect(page.getByText(t.selected(2))).toBeVisible()
  await page.locator('.trash-head').getByRole('button', { name: t.restore }).click()
  await expect(rows).toHaveCount(1)
  await expect.poll(() => existsSync(join(server.vol, 't1.txt')) && existsSync(join(server.vol, 't2.txt'))).toBe(true)
  await rows.getByRole('checkbox').check()
  await page.locator('.trash-head').getByRole('button', { name: t.deleteForever }).click()
  await page.getByRole('dialog').getByRole('button', { name: t.deleteForever }).click()
  await expect(page.getByText(t.trashEmpty)).toBeVisible()
  expect(existsSync(join(server.vol, 't3.txt'))).toBe(false)
})

test('partial delete names the items that failed', async ({ page, server }) => {
  writeFileSync(join(server.vol, 'gone-a.txt'), 'a')
  writeFileSync(join(server.vol, 'gone-b.txt'), 'b')
  await login(page)
  await row(page, 'gone-a.txt').locator('input[type=checkbox]').check()
  await row(page, 'gone-b.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  rmSync(join(server.vol, 'gone-b.txt'))
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(page.locator('.dialog .error')).toHaveText(t.removeFailed(['gone-b.txt']))
  expect(existsSync(join(server.vol, 'gone-a.txt'))).toBe(false)
})

test('passkey registration and login', async ({ page, context, server }) => {
  const cdp = await context.newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })
  const presence = (enabled: boolean) => cdp.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled })
  const name = 'e2e / key'
  const row = page.locator('.rows li', { hasText: name })
  await page.goto(server.url.replace('127.0.0.1', 'localhost'))
  await page.getByPlaceholder(t.username).fill('admin')
  await page.getByPlaceholder(t.password).fill('pw-pw-pw-pw')
  await page.getByRole('button', { name: t.login, exact: true }).click()
  await page.getByRole('link', { name: t.settings }).click()
  await page.getByRole('button', { name: t.addPasskey }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel(t.name).fill(name)
  await dialog.getByRole('button', { name: t.add }).click()
  await expect(dialog).toBeHidden()
  await expect(row).toContainText(t.neverUsed)
  await presence(false)
  await page.getByRole('button', { name: t.logout }).click()
  const button = page.getByRole('button', { name: t.passkeyLogin })
  await expect(button).toBeVisible()
  await presence(true)
  await button.click()
  await expect(row).not.toContainText(t.neverUsed)
})

test('settings sections have headings, a sub-nav and quiet destructive buttons', async ({ page }) => {
  await login(page)
  await page.goto('/settings')
  for (const h of [t.appearance, t.sessions, t.appPasswords]) await expect(page.getByRole('region', { name: h })).toBeVisible()
  await expect(page.getByRole('region', { name: t.appPasswords }).getByText(t.noTokens)).toBeVisible()
  const out = page.getByRole('list', { name: t.sessions }).getByRole('button', { name: t.signOut, exact: true })
  const muted = (b: Element) => getComputedStyle(b).color === getComputedStyle(b.closest('li')!.querySelector('.hint')!).color
  expect(await out.evaluate(muted)).toBe(true)
  await page.getByRole('navigation', { name: t.settings }).getByRole('link', { name: t.appPasswords }).click()
  await expect(page.getByRole('heading', { name: t.appPasswords })).toBeInViewport()
})

test('app passwords that fail to load show the error, not an empty list', async ({ page }) => {
  await login(page)
  await page.route('**/api/tokens', (r) => r.fulfill({ status: 500, body: '{"error":"boom"}' }))
  await page.goto('/settings')
  const card = page.getByRole('region', { name: t.appPasswords })
  await expect(card.locator('.error')).toHaveText(t.serverError)
  await expect(card.getByText(t.noTokens)).toHaveCount(0)
})

test('signing out another device sends it back to login', async ({ page, browser, server }) => {
  const other = await browser.newPage({ baseURL: server.url, userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0' })
  await login(other)
  await login(page)
  await page.getByRole('link', { name: t.settings }).click()
  const rows = page.getByRole('list', { name: t.sessions }).getByRole('listitem')
  await expect(rows).toHaveCount(2)
  await expect(rows.filter({ hasText: t.thisDevice })).toHaveCount(1)
  const firefox = rows.filter({ hasText: 'Firefox · Linux' })
  await expect(firefox).not.toContainText(t.thisDevice)
  await firefox.getByRole('button', { name: t.signOut, exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: t.signOut, exact: true }).click()
  await expect(rows).toHaveCount(1)
  await expect(page.getByRole('button', { name: t.signOutOthers })).toHaveCount(0)
  await other.getByRole('link', { name: t.trash, exact: true }).click()
  await expect(other.getByLabel(t.username)).toBeVisible()
  await other.close()
})

test('an uploaded file shows first in recent and leads back to its folder', async ({ page }) => {
  await login(page)
  await row(page, 'docs').locator('button.name').click()
  await fileInput(page).setInputFiles({ name: 'fresh.txt', mimeType: 'text/plain', buffer: Buffer.from('fresh') })
  await expect(page.locator('.uploads li.done')).toHaveCount(1)
  await page.getByRole('link', { name: t.recent, exact: true }).click()
  const rows = page.locator('.row')
  await expect(rows).toHaveCount(2)
  await expect(rows.first()).toContainText('fresh.txt')
  await expect(rows.first()).toContainText('v:/docs')
  await rows.first().locator('button.more').click()
  await page.getByRole('menuitem', { name: t.openFolder }).click()
  await expect(page).toHaveURL(/\/files\/v\/docs\/\?details=fresh\.txt$/)
})

test.describe('with an English browser', () => {
  test.use({ locale: 'en-US' })

  test('dates stay in the UI language', async ({ page }) => {
    await login(page)
    await page.goto('/settings')
    await expect(page.getByRole('list', { name: t.sessions }).getByText(new RegExp(`^${t.lastUsed('(现在|\\d+秒钟前)')}$`))).toBeVisible()
  })
})

test('keyboard shortcuts act on the list and stay quiet while typing', async ({ page, server }) => {
  for (const n of ['a.txt', 'b.txt', 'c.txt']) writeFileSync(join(server.vol, n), n)
  await login(page)
  await expect(row(page, 'c.txt')).toBeVisible()
  await page.keyboard.press('?')
  const help = page.getByRole('dialog', { name: t.shortcuts })
  await expect(help).toContainText(t.keys.rename)
  await page.keyboard.press('Escape')
  await expect(help).toHaveCount(0)
  await page.keyboard.press('/')
  await expect(page.locator('#filter')).toBeFocused()
  await page.keyboard.type('n?u')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.locator('#filter')).toHaveValue('n?u')
  await page.keyboard.press('Escape')
  await expect(page.locator('#filter')).toHaveValue('')
  await row(page, 'a.txt').focus()
  await page.keyboard.press('n')
  await expect(page.getByRole('dialog', { name: t.newFolder })).toBeVisible()
  await page.keyboard.press('Escape')
  await row(page, 'a.txt').focus()
  await page.keyboard.press('F2')
  await expect(page.getByRole('dialog', { name: t.rename }).getByLabel(t.newName)).toHaveValue('a.txt')
  await page.keyboard.press('Escape')
  const chooser = page.waitForEvent('filechooser')
  await row(page, 'a.txt').focus()
  await page.keyboard.press('u')
  await chooser
  await page.keyboard.press('ControlOrMeta+a')
  await expect(page.getByRole('grid', { name: t.fileList }).getByRole('row', { selected: true })).toHaveCount(4)
  await page.keyboard.press('Delete')
  await expect(page.getByRole('dialog', { name: t.confirmDeleteTitle })).toContainText(t.what(['', '', '', '']))
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
  await row(page, 'a.txt').locator('.num.size').click()
  await row(page, 'c.txt').locator('.num.size').click({ modifiers: ['Shift'] })
  await expect(page.getByRole('row', { selected: true })).toHaveCount(3)
  expect(await page.evaluate(() => getSelection()?.toString())).toBe('')
  const count = await page.locator('.list-head .count').boundingBox()
  expect(count!.height).toBeLessThan(30)
  await page.keyboard.press('Escape')
  await row(page, 'a.txt').locator('button.more').click()
  await page.getByRole('menuitem', { name: t.details }).click()
  await page.locator('.details h2').click()
  await page.keyboard.press('ControlOrMeta+a')
  await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
})

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })

  async function finger(page: Page) {
    const cdp = await page.context().newCDPSession(page)
    const send = (type: string, x = 0, y = 0) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }] })
    const at = async (l: Locator) => {
      const b = (await l.boundingBox())!
      return [b.x + b.width * 0.6, b.y + b.height / 2]
    }
    return {
      async hold(l: Locator, move = 0) {
        const [x, y] = await at(l)
        await send('touchStart', x, y)
        if (move) await send('touchMove', x, y + move)
        await page.waitForTimeout(900)
        await send('touchEnd')
      },
      async swipe(dx: number, dy: number, [x, y] = [195, 420]) {
        await send('touchStart', x, y)
        for (let i = 1; i <= 6; i++) await send('touchMove', x + (dx * i) / 6, y + (dy * i) / 6)
        await send('touchEnd')
      },
    }
  }

  test('long-press starts selection, taps toggle and a bottom toolbar acts on it', async ({ page, server }) => {
    mkdirSync(join(server.vol, 'many'))
    for (let i = 0; i < 30; i++) writeFileSync(join(server.vol, 'many', `m-${String(i).padStart(2, '0')}.txt`), `m${i}`)
    await login(page)
    await row(page, 'many').locator('button.name').tap()
    await expect(page.locator('.bar .title')).toHaveText('many')
    await expect(page.getByRole('link', { name: t.upTo('v') })).toBeVisible()
    await expect(page.locator('.crumbs')).toBeHidden()
    await expect(row(page, 'm-01.txt')).toContainText('2 B · ')
    await expect(row(page, 'm-01.txt').locator('input[type=checkbox]')).toBeHidden()
    expect((await row(page, 'm-01.txt').boundingBox())!.height).toBe(56)

    const f = await finger(page)
    const bar = page.getByRole('toolbar', { name: t.selected(1) })
    await f.hold(row(page, 'm-03.txt'), 40)
    await expect(row(page, 'm-03.txt')).toHaveAttribute('aria-selected', 'false')
    await expect(bar).toHaveCount(0)

    await f.hold(row(page, 'm-01.txt'))
    await expect(row(page, 'm-01.txt')).toHaveAttribute('aria-selected', 'true')
    await expect(bar).toBeVisible()
    await expect(page.getByRole('menu')).toHaveCount(0)
    await expect(page.locator('.files > .bar')).toBeHidden()
    await expect(page.locator('.list-head')).toContainText(t.selected(1))
    await expect(row(page, 'm-01.txt').locator('input[type=checkbox]')).toBeVisible()
    expect(await page.evaluate(() => getSelection()?.toString())).toBe('')

    await row(page, 'm-02.txt').tap()
    await row(page, 'm-01.txt').locator('button.name').tap()
    await expect(row(page, 'm-01.txt')).toHaveAttribute('aria-selected', 'false')
    await expect(row(page, 'm-02.txt')).toHaveAttribute('aria-selected', 'true')
    for (const b of await bar.getByRole('button').all()) expect((await b.boundingBox())!.height).toBeLessThan(70)

    await page.getByRole('button', { name: t.selectAll, exact: true }).tap()
    await expect(page.locator('.list-head')).toContainText(t.selected(30))
    await page.getByRole('button', { name: t.clearSelection }).tap()
    await expect(page.getByRole('toolbar')).toHaveCount(0)
    await expect(page.locator('.files > .bar')).toBeVisible()

    await f.hold(row(page, 'm-00.txt'))
    await bar.getByRole('button', { name: t.remove }).tap()
    await page.getByRole('dialog').getByRole('button', { name: t.remove }).tap()
    const toast = page.locator('.toast', { hasText: t.trashed(t.what(['m-00.txt'])) })
    await expect(toast).toBeVisible()
    const fab = page.getByRole('button', { name: t.new, exact: true })
    await expect(fab).toBeVisible()
    expect((await toast.boundingBox())!.y + (await toast.boundingBox())!.height).toBeLessThanOrEqual((await fab.boundingBox())!.y)

    await row(page, 'm-05.txt').tap()
    await expect(page.locator('.viewer pre')).toHaveText('m5')
    await page.getByRole('dialog', { name: 'm-05.txt' }).getByRole('button', { name: t.close }).tap()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await row(page, 'm-06.txt').locator('button.more').tap()
    await page.getByRole('menuitem', { name: t.selectItem }).tap()
    await expect(row(page, 'm-06.txt')).toHaveAttribute('aria-selected', 'true')
    await page.getByRole('button', { name: t.clearSelection }).tap()
    await page.keyboard.press('?')
    await expect(page.getByRole('dialog', { name: t.shortcuts })).toBeVisible()
  })

  test('the new button opens a sheet and stays clear of uploads and toasts', async ({ page }) => {
    await login(page)
    const fab = page.getByRole('button', { name: t.new, exact: true })
    await fab.tap()
    await expect(page.getByRole('dialog', { name: t.new }).getByRole('button', { name: t.newFolder })).toBeVisible()
    await page.getByRole('dialog', { name: t.new }).getByRole('button', { name: t.newFolder }).tap()
    await page.getByLabel(t.folderName).fill('fresh')
    await page.keyboard.press('Enter')
    await expect(row(page, 'fresh')).toBeVisible()
    await page.route('**/upload/**', () => {})
    await fileInput(page).setInputFiles({ name: 'slow.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(10) })
    const panel = page.locator('.uploads')
    await expect(panel).toBeVisible()
    await expect.poll(async () => (await fab.boundingBox())!.y + 56 <= (await panel.boundingBox())!.y).toBe(true)
  })

  test('a long text preview scrolls by touch', async ({ page, server }) => {
    writeFileSync(join(server.vol, 'docs', 'long.txt'), Array.from({ length: 400 }, (_, i) => `line ${i}`).join('\n'))
    await login(page)
    await row(page, 'docs').locator('button.name').tap()
    await row(page, 'long.txt').tap()
    const body = page.getByRole('dialog', { name: 'long.txt' }).locator('.body')
    await expect(body.locator('pre')).toContainText('line 399')
    const f = await finger(page)
    await f.swipe(0, -400, [195, 700])
    await expect.poll(() => body.evaluate((b) => b.scrollTop)).toBeGreaterThan(0)
    await expect(page.getByRole('dialog', { name: 'long.txt' })).toBeVisible()
  })

  test('swipes step through images and a downward swipe closes the preview', async ({ page, server }) => {
    const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64')
    for (const n of ['p1.png', 'p2.png', 'p3.png']) writeFileSync(join(server.vol, 'docs', n), png)
    await login(page)
    await row(page, 'docs').locator('button.name').tap()
    await row(page, 'p1.png').tap()
    const f = await finger(page)
    await expect(page.getByRole('dialog', { name: 'p1.png' })).toContainText('1 / 3')
    await expect(page.getByRole('button', { name: t.next })).toBeHidden()
    await f.swipe(-200, 10)
    await expect(page.getByRole('dialog', { name: 'p2.png' })).toContainText('2 / 3')
    await f.swipe(200, -10)
    await expect(page.getByRole('dialog', { name: 'p1.png' })).toBeVisible()
    await f.swipe(20, 40)
    await expect(page.getByRole('dialog', { name: 'p1.png' })).toBeVisible()
    await f.swipe(10, 220)
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
})
