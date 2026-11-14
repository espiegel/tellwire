import { defineConfig, devices } from '@playwright/test'

// End-to-end: real Chromium -> built frontend (vite preview) -> real Go gateway -> mock MUD.
// Never touches the live DuneMUD server.
const MOCK = 12323
const GATEWAY = 18080
const WEB = 4173
// E2E_BASE=http://host:port runs the same suite against an already running stack
// (e.g. docker compose, or Caddy in front of the gateway) instead of starting its own.
const external = process.env.E2E_BASE

export default defineConfig({
  testDir: 'e2e',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  // CHROMIUM_PATH lets CI/sandboxes reuse a pre-installed browser instead of `playwright install`.
  use: {
    baseURL: external ?? `http://localhost:${WEB}`,
    trace: 'off',
    launchOptions: process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH, args: ['--no-sandbox'] } : {},
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 5'] }, grep: /@mobile/ },
  ],
  webServer: external ? undefined : [
    {
      command: `go build -o bin/ ./cmd/... && MOCKMUD_LISTEN=127.0.0.1:${MOCK} ./bin/mockmud`,
      cwd: '../gateway',
      port: MOCK,
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: './bin/gateway',
      cwd: '../gateway',
      port: GATEWAY,
      env: {
        GATEWAY_LISTEN: `127.0.0.1:${GATEWAY}`,
        GATEWAY_UPSTREAMS: `default=127.0.0.1:${MOCK}:plain`,
        GATEWAY_ALLOWED_ORIGINS: `http://localhost:${WEB}`,
        GATEWAY_LOG_FORMAT: 'text',
      },
      timeout: 120_000,
    },
    {
      command: `npm run build && npx vite preview --port ${WEB} --strictPort`,
      port: WEB,
      env: { GATEWAY_URL: `http://127.0.0.1:${GATEWAY}` },
      stderr: 'ignore', // vite's dev proxy logs ECONNRESET whenever a browser closes a socket; harmless
      timeout: 120_000,
    },
  ],
})
