import test from '@playwright/test';

import {
  primaryUser,
  ensureWelcomePage,
  switchToOrganization,
  ensureExampleHttpbinProviderWorkspace,
  ensureAccountExists,
  waitForHttpbinBindingReady,
  deleteAccount,
  waitForAccountDeleted,
  logStep,
} from './helpers/portal';
import { newOrgName, testAccountName } from './helpers/constants';

test.describe('Portal Account Deletion', () => {
  // A retry may recreate a deleted account without its original shard selector.
  test.describe.configure({ retries: 0 });
  test.setTimeout(600000);

  test('Account deletion flow', async ({ page }) => {
    logStep('test:account-deletion:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    const url = await ensureAccountExists(page);

    // Exercise account deletion with the operator's APIBinding finalizer present.
    waitForHttpbinBindingReady(newOrgName, testAccountName);

    await deleteAccount(page, url);
    waitForAccountDeleted(newOrgName, testAccountName, 120);
    logStep(`deleteAccount:done account=${testAccountName}`);

    logStep('test:account-deletion:done');
  });
});
