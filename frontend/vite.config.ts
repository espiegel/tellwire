/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// In development the gateway runs separately; proxy /ws so the page keeps
// using same-origin WebSockets exactly as it does behind Caddy in production.
// Keep in sync with frontend/Caddyfile. 'unsafe-inline' for styles is required
// because xterm.js injects <style> elements for theming/measurement.
export const CSP =
  "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
  "font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"

const gateway = process.env.GATEWAY_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { port: 5173, proxy: { '/ws': { target: gateway, ws: true } } },
  preview: { port: 4173, proxy: { '/ws': { target: gateway, ws: true } }, headers: { 'Content-Security-Policy': CSP } },
  build: { target: 'es2022', sourcemap: false },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
