import { expect, Page } from '@playwright/test';

import { exampleMarketplaceEntryName, exampleProviderDisplayName, testAccountName as defaultAccountName } from './constants';
import { logStep, portalOrgUrl } from './log';

const marketplaceFrameSelector = 'iframe[src*="/ui/marketplace/ui/"]';

async function waitForMarketplaceFrame(page: Page, srcPattern: RegExp = /\/ui\/marketplace\/ui\/#\/marketplace/): Promise<void> {
  const marketplaceFrame = page.locator(marketplaceFrameSelector).first();
  await expect(marketplaceFrame).toBeVisible({ timeout: 30000 });
  await expect(marketplaceFrame).toHaveAttribute('src', srcPattern);
}

async function openOrganizationMarketplace(page: Page, orgName?: string): Promise<void> {
  const marketplaceUrl = portalOrgUrl('/home/marketplace', orgName);
  logStep(`openOrganizationMarketplace:url=${marketplaceUrl}`);
  await page.goto(marketplaceUrl, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});
  await waitForMarketplaceFrame(page);
}

async function openAccountMarketplace(page: Page, orgName?: string, accountName?: string): Promise<void> {
  const account = accountName ?? defaultAccountName;
  const marketplaceUrl = portalOrgUrl(`/home/accounts/${account}/marketplace`, orgName);
  logStep(`openAccountMarketplace:url=${marketplaceUrl}`);
  await page.goto(marketplaceUrl, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});
  await waitForMarketplaceFrame(page);
}

async function openAccountMarketplaceProvider(page: Page, orgName?: string, accountName?: string): Promise<void> {
  const account = accountName ?? defaultAccountName;
  const providerUrl = portalOrgUrl(`/home/accounts/${account}/provider/${exampleMarketplaceEntryName}`, orgName);
  logStep(`openAccountMarketplaceProvider:url=${providerUrl}`);
  await page.goto(providerUrl, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});
  await waitForMarketplaceFrame(page, /\/ui\/marketplace\/ui\/#\/provider\//);
  await expect(page.frameLocator(marketplaceFrameSelector).getByText(exampleProviderDisplayName, { exact: true })).toBeVisible({ timeout: 30000 });
}

async function clickMarketplaceAction(page: Page, action: 'Enable' | 'Disable'): Promise<void> {
  logStep(`clickMarketplaceAction:start action=${action}`);
  const actionButton = marketplaceActionButton(page, action);
  await expect(actionButton).toBeVisible({ timeout: 30000 });
  await actionButton.click({ timeout: 10000 });
  logStep(`clickMarketplaceAction:clicked action=${action}`);

  if (action === 'Disable') {
    const modal = page.locator('[data-testid="luigi-confirmation-modal"]');
    await expect(modal).toBeVisible({ timeout: 10000 });
    const confirmButton = modal.locator('[data-testid="luigi-modal-confirm"]');
    await expect(confirmButton).toBeVisible({ timeout: 5000 });
    logStep(`clickMarketplaceAction:confirm-modal-visible`);
    await confirmButton.click({ timeout: 5000 });
    await expect(modal).toBeHidden({ timeout: 10000 });

    const disableButton = marketplaceActionButton(page, 'Disable');
    await expect(disableButton).toBeHidden({ timeout: 30000 });
    logStep(`clickMarketplaceAction:uninstall-confirmed`);
  }

  if (action === 'Enable') {
    // After clicking Enable, the marketplace-ui calls installProviderInstance
    // then goBack(). Wait for the Enable button to disappear (iframe navigated).
    await expect(actionButton).toBeHidden({ timeout: 30000 });
    logStep(`clickMarketplaceAction:install-confirmed`);
  }

  logStep(`clickMarketplaceAction:done action=${action}`);
}

function marketplaceActionButton(page: Page, action: 'Enable' | 'Disable') {
  const actionTestId = action === 'Enable'
    ? 'extension-details-dialog-install-button'
    : 'extension-details-dialog-uninstall-button';

  return page.frameLocator(marketplaceFrameSelector)
    .getByTestId(actionTestId)
    .first();
}

async function expectMarketplaceActionVisible(page: Page, action: 'Enable' | 'Disable'): Promise<void> {
  await expect(marketplaceActionButton(page, action)).toBeVisible({ timeout: 30000 });
}

async function waitForMarketplaceAction(page: Page, action: 'Enable' | 'Disable', orgName?: string, accountName?: string): Promise<void> {
  for (let attempt = 0; attempt < 10; attempt++) {
    await openAccountMarketplaceProvider(page, orgName, accountName);
    const button = marketplaceActionButton(page, action);
    if (await button.isVisible().catch(() => false)) {
      logStep(`waitForMarketplaceAction:done action=${action} attempt=${attempt}`);
      return;
    }
    logStep(`waitForMarketplaceAction:retry action=${action} attempt=${attempt}`);
    await page.waitForTimeout(3000);
  }
  await openAccountMarketplaceProvider(page, orgName, accountName);
  await expectMarketplaceActionVisible(page, action);
}

async function waitForHttpBinsNavigation(page: Page, visible: boolean, accountUrl: string): Promise<void> {
  const httpBinsNav = page.locator('[data-testid="orchestrate_platform-mesh_io_httpbins_httpbins"]').first();

  for (let attempt = 0; attempt < 6; attempt++) {
    await page.goto(accountUrl, { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});

    const isVisible = await httpBinsNav.isVisible().catch(() => false);
    if (isVisible === visible) {
      return;
    }

    await page.waitForTimeout(2000);
  }

  if (visible) {
    await expect(httpBinsNav).toBeVisible({ timeout: 5000 });
    return;
  }

  await expect(httpBinsNav).toBeHidden({ timeout: 5000 });
}

export {
  openOrganizationMarketplace,
  openAccountMarketplace,
  openAccountMarketplaceProvider,
  clickMarketplaceAction,
  expectMarketplaceActionVisible,
  waitForMarketplaceAction,
  waitForHttpBinsNavigation,
};
