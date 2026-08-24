import { defineConfig, devices } from '@playwright/test';

/**
 * Browser end-to-end configuration.
 *
 * Both target engines are exercised against the same packed file, opened
 * directly through a file:// URL. Nothing is ever served over HTTP: there is
 * no webServer entry here, and the specs assert that no http(s) request is
 * made at all.
 */
export default defineConfig({
  testDir: '.',
  testMatch: /.*\.spec\.mjs/,
  globalSetup: './global-setup.mjs',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI ? [['list'], ['github']] : [['list']],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    // Never load anything over the network, even accidentally.
    offline: false, // set per-test where the assertion is about offline rendering
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
