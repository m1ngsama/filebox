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
  await page.getByLabel(t.password).fill('pw-pw-pw-pw')
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
  await page.getByRole('button', { name: t.login, exact: true }).click()
  await expect(page.getByText(t.wrongLogin)).toBeVisible()
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
  await expect(anon.getByLabel(t.password)).toBeVisible()
  expect(await unlabeled(anon)).toBe(0)
  await expect(anon.locator('body')).not.toContainText('docs')
  await anon.getByLabel(t.password).fill('secret-pass')
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
