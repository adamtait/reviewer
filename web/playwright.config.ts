// SPDX-License-Identifier: MIT

import { defineConfig, devices } from '@playwright/test';

// The journeys run against the built page, served by `vite preview`, with the
// API answered inside the browser by e2e/fake-api.ts. No service is started.
// A Chromium to use in place of the one Playwright installs, for a machine
// that already has a browser and no way to download another.
const chromium = process.env.WORKSPACE_E2E_CHROMIUM;

// A running service to take the page from, in place of `vite preview`. The
// API is still answered inside the browser; what this adds is the service's
// own page, assets and headers.
const service = process.env.WORKSPACE_E2E_BASE_URL;

export default defineConfig({
  testDir: 'e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: service ?? 'http://localhost:4173',
    trace: 'off',
    ...(chromium ? { launchOptions: { executablePath: chromium } } : {}),
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    // 390 points wide: the narrow layout the design names.
    {
      name: 'phone',
      use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true },
    },
  ],
  ...(service
    ? {}
    : {
        webServer: {
          command: 'npm run preview',
          url: 'http://localhost:4173',
          reuseExistingServer: !process.env.CI,
          timeout: 60_000,
        },
      }),
});
