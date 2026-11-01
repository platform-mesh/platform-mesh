import { defineConfig } from '@playwright/test';

const slowMo = Number(process.env.SLOW_MO || '0');

export default defineConfig({
  retries: 2,
  use: {
    ignoreHTTPSErrors: true,
    launchOptions: {
      slowMo: Number.isFinite(slowMo) ? slowMo : 0,
    },
    video: process.env.VIDEO === 'true' ? 'on' : 'off',
    trace: 'retain-on-failure',
  },
});
