import { defineConfig } from '@playwright/test';

const slowMo = Number(process.env.SLOW_MO || '0');

// The sharded tasks run several `playwright test` processes at once; a shared
// output directory makes them truncate each other's trace archives.
const outputDir = ['test-results', process.env.ORG_NAME, process.env.TEST_ACCOUNT_NAME].filter(Boolean).join('/');

export default defineConfig({
  retries: 2,
  outputDir,
  use: {
    ignoreHTTPSErrors: true,
    launchOptions: {
      slowMo: Number.isFinite(slowMo) ? slowMo : 0,
    },
    video: process.env.VIDEO === 'true' ? 'on' : 'off',
    trace: 'retain-on-failure',
  },
});
