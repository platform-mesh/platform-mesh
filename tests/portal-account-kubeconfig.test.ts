import test from '@playwright/test';

import {
  primaryUser,
  ensureWelcomePage,
  switchToOrganization,
  ensureExampleHttpbinProviderWorkspace,
  ensureAccountExists,
  downloadAccountKubeconfig,
  verifyDownloadedKubeconfig,
  logStep,
} from './helpers/portal';

test.describe('Portal Account Kubeconfig', () => {
  test.setTimeout(600000);

  test('Account kubeconfig download flow', async ({ page }) => {
    logStep('test:account-kubeconfig:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    await ensureAccountExists(page);
    const kubeconfigPath = await downloadAccountKubeconfig(page);
    await verifyDownloadedKubeconfig(kubeconfigPath);
    logStep('test:account-kubeconfig:done');
  });
});
