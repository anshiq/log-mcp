import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  globalSetup: './e2e/globalSetup.ts',
  use: {
    baseURL: `http://127.0.0.1:${process.env.E2E_PORT ?? '17350'}`
  }
});
