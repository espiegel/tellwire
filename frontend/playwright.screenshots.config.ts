import { defineConfig, devices } from '@playwright/test'
import base from './playwright.config'

// `npm run screenshots` renders the README images against the mock MUD, using the same
// gateway + preview servers as the e2e suite. Output: ../docs/img/*.png
export default defineConfig({
  ...base,
  testDir: 'screenshots',
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 }, grep: /@desktop/ },
    { name: 'mobile', use: { ...devices['Pixel 5'] }, grep: /@mobile/ },
  ],
})
