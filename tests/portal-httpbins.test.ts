import test from '@playwright/test';

import {
  primaryUser,
  testNamespaceName,
  testNamespaceHttpBinName,
  ensureWelcomePage,
  switchToOrganization,
  ensureExampleHttpbinProviderWorkspace,
  ensureAccountExists,
  ensureNamespaceExists,
  ensureHttpBinExists,
  assertHttpBinLinkWorks,
  selectNamespaceScope,
  logStep,
} from './helpers/portal';

test.describe('Portal HTTPBins', () => {
  test.setTimeout(600000);

  test('HTTPBins flow', async ({ page }) => {
    logStep('test:httpbins:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    await ensureAccountExists(page);
    await ensureNamespaceExists(page, testNamespaceName);
    await ensureHttpBinExists(page, testNamespaceName, testNamespaceHttpBinName);
    await selectNamespaceScope(page, testNamespaceName);
    await assertHttpBinLinkWorks(page, testNamespaceName, testNamespaceHttpBinName);
    logStep('test:httpbins:done');
  });
});
