import test from '@playwright/test';

import {
  primaryUser,
  ensureWelcomePage,
  switchToOrganization,
  ensureExampleHttpbinProviderWorkspace,
  ensureAccountExists,
  deleteAccount,
  waitForAccountDeleted,
  logStep,
} from './helpers/portal';
import { newOrgName, testAccountName } from './helpers/constants';

test.describe('Portal Account Deletion', () => {
  test.setTimeout(600000);

  test('Account deletion flow', async ({ page }) => {
    logStep('test:account-deletion:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    const url = await ensureAccountExists(page);
    await deleteAccount(page, url);
    waitForAccountDeleted(newOrgName, testAccountName, 120);
    logStep(`deleteAccount:done account=${testAccountName}`);

    logStep('test:account-deletion:done');
  });
});
