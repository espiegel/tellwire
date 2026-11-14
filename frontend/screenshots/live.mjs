// Interactive README screenshots from a REAL MUD. You log in; the script captures.
//   npm run screenshots:live                      # real DuneMUD (dunemud.net:6788, TLS)
//   UPSTREAM=127.0.0.1:2323:plain npm run screenshots:live   # any other upstream (e.g. the mock)
//   BRAND_FILE=path/to/brand.json                              # override the branding shown (see docs/deployment.md)
// Nothing is typed for you and no credentials are read, stored or logged by this script.
// Output: ../docs/img/{hero,gmcp-inspector,settings,mobile}.png  (override with OUT_DIR)
import { spawn, spawnSync } from 'node:child_process'
import { createInterface } from 'node:readline'
import { chromium } from '@playwright/test'
import path from 'node:path'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(here, '../..')
const OUT = process.env.OUT_DIR ?? path.join(root, 'docs/img')
const UPSTREAM = process.env.UPSTREAM ?? 'dunemud.net:6788:tls'
// Real DuneMUD gets the DuneMUD brand preset; other upstreams keep the default Tellwire brand unless BRAND_FILE is set.
const BRAND_FILE = process.env.BRAND_FILE ?? (process.env.UPSTREAM ? undefined : path.join(root, 'deploy/brand.dunemud.json'))
const GW = 18090, WEB = 4174
const procs = []
const rl = createInterface({ input: process.stdin, output: process.stdout })
const ask = (q) => new Promise((r) => rl.question(`\n${q}\n  [Enter] to capture › `, () => r()))
const cleanup = () => procs.forEach((p) => p.kill())
process.on('exit', cleanup); process.on('SIGINT', () => { cleanup(); process.exit(130) })

const sh = (cmd, args, cwd) => { const r = spawnSync(cmd, args, { cwd, stdio: 'inherit' }); if (r.status) process.exit(r.status) }
sh('go', ['build', '-o', 'bin/', './cmd/gateway'], path.join(root, 'gateway'))
sh('npm', ['run', '-s', 'build'], path.join(root, 'frontend'))
procs.push(spawn('./bin/gateway', [], { cwd: path.join(root, 'gateway'), stdio: 'ignore', env: { ...process.env,
  GATEWAY_LISTEN: `127.0.0.1:${GW}`, GATEWAY_UPSTREAMS: `default=${UPSTREAM}`,
  GATEWAY_ALLOWED_ORIGINS: `http://localhost:${WEB}`, GATEWAY_LOG_FORMAT: 'text' } }))
procs.push(spawn('npx', ['vite', 'preview', '--port', String(WEB), '--strictPort'], { cwd: path.join(root, 'frontend'), stdio: 'ignore',
  env: { ...process.env, GATEWAY_URL: `http://127.0.0.1:${GW}` } }))
await new Promise((r) => setTimeout(r, 4000))

const browser = await chromium.launch({ headless: process.env.HEADLESS === '1' })
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 })
const page = await ctx.newPage()
if (BRAND_FILE) {
  const body = readFileSync(BRAND_FILE, 'utf8')
  await page.route('**/brand.json', (route) => route.fulfill({ contentType: 'application/json', body }))
}
await page.goto(`http://localhost:${WEB}/`)
const shot = async (name) => { await page.mouse.move(0, 0); await page.screenshot({ path: path.join(OUT, name) }); console.log(`  saved ${name}`) }

console.log('\nLog in using the browser window. Before sharing a shot, make sure no private chat,')
console.log('tells, or other players\' names are on screen (scroll/clear first). Only you can judge that.')
await ask('1/4 HERO: get to a good-looking spot (a room with colour, maybe `look`).')
await shot('hero.png')
await page.getByRole('button', { name: 'GMCP inspector' }).click()
await ask('2/4 INSPECTOR: the GMCP panel is open. Move/act so Room.Info and Char.Vitals show.')
await shot('gmcp-inspector.png')
await page.getByRole('button', { name: 'GMCP inspector' }).click() // close
await page.getByRole('button', { name: 'Settings' }).click()
await ask('3/4 SETTINGS: the settings panel is open.')
await shot('settings.png')
await page.keyboard.press('Escape')
const cdp = await ctx.newCDPSession(page)
await cdp.send('Emulation.setDeviceMetricsOverride', { width: 393, height: 851, deviceScaleFactor: 2.75, mobile: true })
await ask('4/4 MOBILE: the page is now a phone-sized viewport. Adjust if you like.')
await page.screenshot({ path: path.join(OUT, 'mobile.png') }); console.log('  saved mobile.png')
await browser.close(); rl.close(); cleanup()
