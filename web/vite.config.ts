// SPDX-License-Identifier: MIT

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The service that answers /api and /auth while developing. `npm run dev`
// serves the page and passes those two prefixes on, so the cookie stays on one
// origin, as it is when the service serves the built page itself.
const service = process.env.WORKSPACE_SERVICE ?? 'http://localhost:8080';

// The policy the service sends with the page, word for word. `vite preview`
// sends it too, so the journeys in e2e/ run under it and fail if the built
// page needs anything it forbids. internal/workspace/web/web_test.go fails if
// this text and the service's ever differ.
const contentSecurityPolicy =
  "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    // No inline scripts and no inline module preload: the page is served under
    // a content security policy that allows scripts from its own origin only.
    modulePreload: { polyfill: false },
    sourcemap: false,
  },
  server: {
    proxy: { '/api': service, '/auth': service },
  },
  preview: {
    headers: { 'Content-Security-Policy': contentSecurityPolicy, 'X-Content-Type-Options': 'nosniff' },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    restoreMocks: true,
  },
});
