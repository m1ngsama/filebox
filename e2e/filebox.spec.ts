import { test, expect, type Page } from '@playwright/test'
import { createHash } from 'node:crypto'
import { closeSync, createReadStream, existsSync, openSync, readFileSync, statSync, writeFileSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DIR } from './playwright.config'
import { t } from '../web/src/lib/i18n'

const VOL = join(DIR, 'vol')
const fileInput = (p: Page) => p.locator('input[type=file]:not([webkitdirectory])')
const row = (p: Page, name: string) => p.locator('.row', { hasText: name })

async function login(page: Page) {
  await page.goto('/')
  await page.getByPlaceholder(t.password).fill('pw-pw-pw-pw')
  await page.getByRole('button', { name: t.login }).click()
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

// docs is a directory, so its "..." menu carries a 分享 action that opens the
// details sidebar (both the 'share' and 'details' action ids route there).
async function shareDocs(page: Page, mode: 'read' | 'upload' | 'drop', password = '') {
  await row(page, 'docs').locator('button.more').click()
  await page.getByRole('menuitem', { name: t.share, exact: true }).click()
  await page.getByRole('radio', { name: t.modes[mode], exact: true }).check()
  if (password) await page.getByLabel(t.passwordOptional).fill(password)
  // .shares renders a placeholder <li> (no <a>) when empty, so count links, not <li>s.
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
  await page.getByPlaceholder(t.password).fill('nope-nope')
  await page.getByRole('button', { name: t.login }).click()
  await expect(page.getByText(t.wrongPassword)).toBeVisible()
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
  // Keyed by Upload-Offset, not PATCH count, so this stays meaningful regardless of chunkSize.
  let aborted = false
  let stalled = false
  await page.route('**/upload/*', (route) => {
    if (route.request().method() !== 'PATCH') return route.continue()
    const offset = Number(route.request().headers()['upload-offset'])
    if (!aborted && offset > 0) {
      aborted = true
      return route.abort('connectionreset') // dropped connection: the client retries
    }
    // Never resolve so the upload is still mid-flight (offset > 0, not done) when the page
    // reloads below; a real stalled connection likewise never completes.
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
