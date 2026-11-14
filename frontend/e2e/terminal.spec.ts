import { expect, test, type Page } from '@playwright/test'

const screen = (page: Page) => page.locator('.xterm-rows')
const input = (page: Page) => page.getByTestId('command-input')
const status = (page: Page) => page.getByTestId('status')

async function login(page: Page, name = 'Paul', password = 'sup3r-secret-pw') {
  await page.goto('/')
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await expect(screen(page)).toContainText('By what name are you known?')
  await input(page).fill(name)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Password:')
  return password
}

test('connects through the gateway to the mock MUD and shows the banner', async ({ page }) => {
  // The preview server sends the production CSP; any violation shows up as a console error.
  const violations: string[] = []
  page.on('console', (m) => /content security policy/i.test(m.text()) && violations.push(m.text()))
  page.on('pageerror', (e) => violations.push(String(e)))
  await page.goto('/')
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await expect(screen(page)).toContainText('Tellwire Mock Server')
  await expect(screen(page)).toContainText('By what name are you known?')
  await input(page).fill('look')
  await input(page).press('Enter')
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.keyboard.press('Escape')
  expect(violations).toEqual([])
})

test('password is masked, never echoed, never stored, and not recallable from history', async ({ page }) => {
  const pw = await login(page)
  await expect(input(page)).toHaveAttribute('type', 'password')
  await expect(input(page)).toHaveAttribute('data-masked', 'true')
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
  await expect(input(page)).toHaveAttribute('type', 'text')
  // Not in terminal, not in storage, not in history.
  expect(await screen(page).innerText()).not.toContain(pw)
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain(pw)
  await input(page).press('ArrowUp')
  expect(await input(page).inputValue()).not.toBe(pw)
  expect(await input(page).inputValue()).toBe('Paul') // the (non-secret) name is history; the password never entered it
  await input(page).press('ArrowUp')
  expect(await input(page).inputValue()).toBe('Paul') // nothing older exists
})

test('room description, ANSI colours, 256-colour and movement', async ({ page }) => {
  const pw = await login(page)
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Arrakeen Shuttle Landing Port')
  await expect(screen(page)).toContainText('golden sand')
  // ANSI colour is rendered as styled cells, not literal escape text.
  expect(await screen(page).innerText()).not.toContain('[33m')
  await expect(page.locator('.xterm-fg-3', { hasText: 'golden sand' }).first()).toBeVisible() // ANSI yellow
  await input(page).fill('colors')
  await input(page).press('Enter')
  await expect(page.locator('.xterm-bg-16').first()).toBeAttached() // 256-colour background
  await input(page).fill('north')
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Arrakeen Market')
  await input(page).fill('unicode')
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('café naïve')
  await expect(screen(page)).toContainText('╔═══╗')
})

test('command history with arrow keys and arbitrary commands', async ({ page }) => {
  const pw = await login(page)
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
  for (const c of ['look', "say it's 100% fine; ok?", 'who']) {
    await input(page).fill(c)
    await input(page).press('Enter')
  }
  await expect(screen(page)).toContainText("You say: it's 100% fine; ok?")
  await expect(screen(page)).toContainText('Players online:')
  await input(page).press('ArrowUp')
  await expect(input(page)).toHaveValue('who')
  await input(page).press('ArrowUp')
  await expect(input(page)).toHaveValue("say it's 100% fine; ok?")
  await input(page).press('ArrowDown')
  await expect(input(page)).toHaveValue('who')
})

test('GMCP inspector shows live structured data from the gateway', async ({ page }) => {
  const pw = await login(page)
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
  await page.getByRole('button', { name: 'GMCP inspector' }).click()
  const insp = page.getByTestId('gmcp-inspector')
  await expect(insp).toContainText('Room.Info')
  await expect(insp).toContainText('Arrakeen Shuttle Landing Port')
  await expect(insp).toContainText('Char.Vitals')
  await input(page).fill('hit')
  await input(page).press('Enter')
  await expect(insp).toContainText('"hp": 355') // delta merged into full vitals
  await expect(insp).toContainText('"maxhp": 412')
})

test('quit, disconnect status, and reconnect to a fresh session', async ({ page }) => {
  const pw = await login(page)
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
  await input(page).fill('quit')
  await input(page).press('Enter')
  await expect(status(page)).toHaveAttribute('data-state', 'disconnected')
  await expect(screen(page)).toContainText('disconnected')
  await expect(input(page)).toBeDisabled()
  await page.getByRole('button', { name: 'Reconnect' }).first().click()
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await expect(screen(page)).toContainText('By what name are you known?') // brand new login prompt
})

test('manual disconnect then reconnect', async ({ page }) => {
  await page.goto('/')
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await page.getByRole('button', { name: 'Disconnect' }).click()
  await expect(status(page)).toHaveAttribute('data-state', 'disconnected')
  await page.getByRole('button', { name: 'Reconnect' }).click()
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
})

test('terminal size is reported to the MUD (NAWS) and follows resizes', async ({ page }) => {
  const pw = await login(page)
  await input(page).fill(pw)
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
  await input(page).fill('term')
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('TTYPE: TELLWIRE, XTERM-256COLOR, MTTS 13')
  const before = (await screen(page).innerText()).match(/size: (\d+)x(\d+)/)!
  await page.setViewportSize({ width: 700, height: 500 })
  await page.waitForTimeout(500)
  await input(page).fill('term')
  await input(page).press('Enter')
  await expect(async () => {
    const all = [...(await screen(page).innerText()).matchAll(/size: (\d+)x(\d+)/g)]
    const last = all[all.length - 1]!
    expect(Number(last[1])).toBeLessThan(Number(before[1]))
  }).toPass()
})

test('font size preference persists across reloads', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByLabel(/Font size/).fill('22')
  await page.keyboard.press('Escape')
  await page.reload()
  await page.getByRole('button', { name: 'Settings' }).click()
  await expect(page.getByTestId('font-size-value')).toHaveText('22')
})

test('mobile layout fits the viewport without horizontal scroll @mobile', async ({ page }) => {
  await page.goto('/')
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await expect(screen(page)).toContainText('By what name are you known?')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  await expect(input(page)).toBeVisible()
  const box = (await input(page).boundingBox())!
  expect(box.y + box.height).toBeLessThanOrEqual(page.viewportSize()!.height)
})

test('a deployment can rebrand the client at runtime via /brand.json (and the default is Tellwire)', async ({ page }) => {
  const violations: string[] = []
  page.on('console', (m) => /content security policy/i.test(m.text()) && violations.push(m.text()))
  await page.goto('/')
  await expect(page.getByTestId('brand-name')).toHaveText('Tellwire')
  await expect(page).toHaveTitle('Tellwire — MUD in your browser')

  await page.route('**/brand.json', (route) =>
    route.fulfill({ contentType: 'application/json', body: JSON.stringify({ name: 'DuneMUD', accentStart: 4, tagline: 'unofficial client', title: 'DuneMUD — Play' }) }))
  await page.reload()
  await expect(page.getByTestId('brand-name')).toHaveText('DuneMUD')
  await expect(page).toHaveTitle('DuneMUD — Play')
  await expect(page.getByText('unofficial client')).toBeVisible()
  await expect(status(page)).toHaveAttribute('data-state', 'connected') // branding must not break the session
  expect(violations).toEqual([])
})
