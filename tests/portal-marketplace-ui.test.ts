import test from '@playwright/test';

import {
  primaryUser,
  ensureWelcomePage,
  switchToOrganization,
  ensureExampleHttpbinProviderWorkspace,
  ensureAccountExists,
  openOrganizationMarketplace,
  openAccountMarketplace,
  openAccountMarketplaceProvider,
  clickMarketplaceAction,
  expectMarketplaceActionVisible,
  waitForMarketplaceAction,
  waitForHttpBinsNavigation,
  logStep,
} from './helpers/portal';

test.describe('Portal Marketplace UI', () => {
  test.setTimeout(600000);

  test('Marketplace is available by default in org and account routes', async ({ page }) => {
    logStep('test:marketplace-ui-availability:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    await openOrganizationMarketplace(page);
    const accountUrl = await ensureAccountExists(page);
    await openAccountMarketplace(page);
    await openAccountMarketplaceProvider(page);
    await expectMarketplaceActionVisible(page, 'Disable');
    await waitForHttpBinsNavigation(page, true, accountUrl);
    logStep('test:marketplace-ui-availability:done');
  });

  test('Marketplace provider can be disabled and enabled through UI', async ({ page }) => {
    logStep('test:marketplace-ui-lifecycle:start');
    await ensureWelcomePage(page, primaryUser);
    await switchToOrganization(page, primaryUser, true);
    ensureExampleHttpbinProviderWorkspace();

    const accountUrl = await ensureAccountExists(page);
    await openAccountMarketplaceProvider(page);
    await expectMarketplaceActionVisible(page, 'Disable');

    await clickMarketplaceAction(page, 'Disable');
    await waitForHttpBinsNavigation(page, false, accountUrl);
    await waitForMarketplaceAction(page, 'Enable');

    await clickMarketplaceAction(page, 'Enable');
    await waitForMarketplaceAction(page, 'Disable');
    await waitForHttpBinsNavigation(page, true, accountUrl);
    logStep('test:marketplace-ui-lifecycle:done');
  });
});
