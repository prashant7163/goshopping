import { defineConfig, devices } from '@playwright/test'

/**
 * The cascade-layer arrangement (ADR-031) is checked here rather than in a unit
 * test, because none of it is visible without a browser: MUI's rules are
 * injected by Emotion at runtime, and what matters is which declaration the
 * cascade actually picks.
 *
 * Both servers are exercised. Vite serves CSS through injected <style> elements
 * in development and a <link> in the built output, and the ordering bug this
 * suite exists to catch turned on exactly that difference — so passing against
 * one proves little about the other.
 */
export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? 'github' : 'list',

  webServer: [
    {
      command: 'npm run dev',
      port: 5173,
      reuseExistingServer: !process.env.CI,
    },
    {
      command: 'npm run build && npm run preview',
      port: 4173,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],

  projects: [
    {
      name: 'dev',
      use: { ...devices['Desktop Chrome'], baseURL: 'http://localhost:5173' },
    },
    {
      name: 'preview',
      use: { ...devices['Desktop Chrome'], baseURL: 'http://localhost:4173' },
    },
  ],
})
