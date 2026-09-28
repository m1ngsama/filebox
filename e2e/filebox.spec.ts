import { test, expect, type Page } from '@playwright/test'
import { createHash } from 'node:crypto'
import { closeSync, createReadStream, existsSync, openSync, readFileSync, rmSync, statSync, writeFileSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DIR } from './playwright.config'
import { t } from '../web/src/lib/i18n'

const VOL = join(DIR, 'vol')
const fileInput = (p: Page) => p.locator('input[type=file]:not([webkitdirectory])')
const row = (p: Page, name: string) => p.locator('.row', { hasText: name })

async function login(page: Page) {
  await page.goto('/')
  await page.getByPlaceholder(t.username).fill('admin')
  await page.getByPlaceholder(t.password).fill('pw-pw-pw-pw')
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
  await page.getByRole('menuitem', { name: t.share, exact: true }).click()
  await page.getByRole('radio', { name: t.modes[mode], exact: true }).check()
  if (password) await page.getByLabel(t.passwordOptional).fill(password)
  const before = await page.locator('.details .shares li a').count()
  await page.getByRole('button', { name: t.newShare, exact: true }).click()
  const links = page.locator('.details .shares li a')
  await expect(links).toHaveCount(before + 1)
  const url = await links.first().getAttribute('href')
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
  await row(page, 'docs').locator('button.name').click()
  await expect(page).toHaveURL(/\/files\/v\/docs\/$/)
  await row(page, 'readme.txt').locator('button.name').click()
  await expect(page.locator('.viewer pre')).toHaveText('hello\n')
  await page.keyboard.press('Escape')
  await page.locator('.crumbs a').first().click()
  await expect(page).toHaveURL(/\/files\/v\/$/)
})

test('resumable upload survives a dropped connection and a page reload', async ({ page }) => {
  const src = bigFile(200)
  const total = statSync(src).size
  await login(page)
  let aborted = false
  let stalled = false
  await page.route('**/upload/*', (route) => {
    if (route.request().method() !== 'PATCH') return route.continue()
    const offset = Number(route.request().headers()['upload-offset'])
    if (!aborted && offset > 0) {
      aborted = true
      return route.abort('connectionreset')
    }
    if (!stalled && offset >= total * 0.3) {
      stalled = true
      return new Promise<void>(() => {})
    }
    return route.continue()
  })
  const offsets: number[] = []
  page.on('request', (r) => {
    if (r.method() === 'PATCH') offsets.push(Number(r.headers()['upload-offset']))
  })
  await fileInput(page).setInputFiles(src)
  await expect.poll(() => stalled).toBe(true)
  await expect(page.locator('.uploads li.uploading')).toHaveCount(1)
  await page.unrouteAll()
  await page.reload()
  offsets.length = 0
  await fileInput(page).setInputFiles(src)
  await expect(page.locator('.uploads li.done')).toHaveCount(1, { timeout: 120_000 })
  expect(offsets[0]).toBeGreaterThan(0)
  expect(await sha(join(VOL, 'filebox-e2e-200.bin'))).toBe(await sha(src))
})

test('closing details opened from my shares clears the query', async ({ page }) => {
  await login(page)
  await shareDocs(page, 'read')
  await page.getByRole('link', { name: t.myShares, exact: true }).click()
  await page.locator('.rows li a', { hasText: 'v:/docs' }).first().click()
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

test('password share opens anonymously', async ({ page, browser }) => {
  await login(page)
  const url = await shareDocs(page, 'read', 'secret-pass')
  const anon = await browser.newPage()
  await anon.goto(url)
  await expect(anon.getByPlaceholder(t.password)).toBeVisible()
  await expect(anon.locator('body')).not.toContainText('docs')
  await anon.getByPlaceholder(t.password).fill('secret-pass')
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

test('drop share accepts uploads and hides contents', async ({ page, browser }) => {
  await login(page)
  const url = await shareDocs(page, 'drop')
  const anon = await browser.newPage()
  await anon.goto(url)
  await expect(anon.getByText(t.dropHint)).toBeVisible()
  await expect(anon.getByText('readme.txt')).toHaveCount(0)
  await fileInput(anon).setInputFiles({ name: 'hello.txt', mimeType: 'text/plain', buffer: Buffer.from('dropped') })
  await expect(anon.locator('.uploads li.done')).toHaveCount(1)
  expect(readFileSync(join(VOL, 'docs/hello.txt'), 'utf8')).toBe('dropped')
  await anon.close()
})

test('delete and restore from trash', async ({ page }) => {
  writeFileSync(join(VOL, 'tmp.txt'), 'x')
  await login(page)
  await row(page, 'tmp.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(row(page, 'tmp.txt')).toHaveCount(0)
  await page.getByRole('link', { name: t.trash, exact: true }).click()
  await page.getByRole('button', { name: t.restore, exact: true }).click()
  await expect.poll(() => existsSync(join(VOL, 'tmp.txt'))).toBe(true)
})

test('partial delete names the items that failed', async ({ page }) => {
  writeFileSync(join(VOL, 'gone-a.txt'), 'a')
  writeFileSync(join(VOL, 'gone-b.txt'), 'b')
  await login(page)
  await row(page, 'gone-a.txt').locator('input[type=checkbox]').check()
  await row(page, 'gone-b.txt').locator('input[type=checkbox]').check()
  await page.locator('.list-head').getByRole('button', { name: t.remove, exact: true }).click()
  rmSync(join(VOL, 'gone-b.txt'))
  await page.locator('.dialog').getByRole('button', { name: t.remove, exact: true }).click()
  await expect(page.locator('.dialog .error')).toHaveText(t.removeFailed(['gone-b.txt']))
  expect(existsSync(join(VOL, 'gone-a.txt'))).toBe(false)
})

test('passkey registration and login', async ({ page, context }) => {
  const cdp = await context.newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })
  const presence = (enabled: boolean) => cdp.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled })
  const name = `e2e / ${Date.now()}`
  const row = page.locator('.rows li', { hasText: name })
  await page.goto('http://localhost:5298/')
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
