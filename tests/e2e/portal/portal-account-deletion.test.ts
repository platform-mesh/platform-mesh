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
import { prepareHttpbinDeletionBinding } from './helpers/deletion';

test.describe('Portal Account Deletion', () => {
  // Retrying can recreate the account on a different shard.
  test.describe.configure({ retries: 0 });
  test.setTimeout(600000);

  test('Account deletion flow', async ({ page }) => {
    logStep('test:account-deletion:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    const url = await ensureAccountExists(page);

    // Exercise account deletion with the operator's APIBinding finalizer present.
    prepareHttpbinDeletionBinding(newOrgName, testAccountName);

    await deleteAccount(page, url);
    waitForAccountDeleted(newOrgName, testAccountName, 120);
    logStep(`deleteAccount:done account=${testAccountName}`);

    logStep('test:account-deletion:done');
  });
});
