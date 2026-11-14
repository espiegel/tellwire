import { expect, test, type Page } from '@playwright/test'

const OUT = '../docs/img'
const screen = (page: Page) => page.locator('.xterm-rows')
const input = (page: Page) => page.getByTestId('command-input')
const status = (page: Page) => page.getByTestId('status')

async function enterGame(page: Page) {
  await page.goto('/')
  await expect(status(page)).toHaveAttribute('data-state', 'connected')
  await expect(screen(page)).toContainText('By what name are you known?')
  await input(page).fill('Paul')
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Password:')
  await input(page).fill('demo-password')
  await input(page).press('Enter')
  await expect(screen(page)).toContainText('Welcome, Paul.')
}

async function run(page: Page, cmd: string, expectText?: string) {
  await input(page).fill(cmd)
  await input(page).press('Enter')
  if (expectText) await expect(screen(page)).toContainText(expectText)
}

test('hero @desktop', async ({ page }) => {
  await enterGame(page)
  await run(page, 'colors')
  await page.mouse.move(0, 0)
  await page.screenshot({ path: `${OUT}/hero.png` })
})

test('gmcp inspector @desktop', async ({ page }) => {
  await enterGame(page)
  await page.getByRole('button', { name: 'GMCP inspector' }).click()
  const insp = page.getByTestId('gmcp-inspector')
  await expect(insp).toContainText('Room.Info')
  await run(page, 'hit')
  await expect(insp).toContainText('"hp": 355')
  await page.mouse.move(0, 0)
  await page.screenshot({ path: `${OUT}/gmcp-inspector.png` })
})

test('settings @desktop', async ({ page }) => {
  await enterGame(page)
  await page.getByRole('button', { name: 'Settings' }).click()
  await expect(page.getByTestId('font-size-value')).toBeVisible()
  await page.mouse.move(0, 0)
  await page.screenshot({ path: `${OUT}/settings.png` })
})

test('mobile @mobile', async ({ page }) => {
  await enterGame(page)
  await run(page, 'colors')
  await page.screenshot({ path: `${OUT}/mobile.png` })
})
