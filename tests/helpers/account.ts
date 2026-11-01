import { existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { expect, Locator, Page } from '@playwright/test';

import { newOrgName, testAccountName as defaultAccountName } from './constants';
import { waitForAccountDeleted, waitForAccountExists, waitForAccountReady, normalizeDownloadedKubeconfig } from './backend';
import { clickRobust } from './httpbins';
import { logStep, portalOrgUrl } from './log';

async function openAccountsView(page: Page, orgName?: string): Promise<void> {
  const createButton = page.getByRole('button', { name: 'Create', exact: true });
  const heading = page.getByRole('heading', { name: 'Accounts', exact: true });
  const accountsUrl = portalOrgUrl('/home/accounts', orgName);

  logStep(`openAccountsView:navigate url=${accountsUrl}`);
  await page.goto(accountsUrl, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});

  await Promise.race([
    createButton.waitFor({ state: 'visible', timeout: 30000 }).then(() => true),
    heading.waitFor({ state: 'visible', timeout: 30000 }).then(() => createButton.isVisible().catch(() => false)),
  ]).catch(() => false);

  await expect(createButton).toBeVisible({ timeout: 30000 });
}

async function ensureListRowVisible(page: Page, row: Locator): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt++) {
    if (await row.isVisible().catch(() => false)) {
      return;
    }

    const loadMoreButton = page.getByRole('button', { name: 'Load more' });
    if (!await loadMoreButton.isVisible().catch(() => false)) {
      return;
    }

    await loadMoreButton.click({ force: true });
    await page.waitForTimeout(1000);
  }
}

async function ensureAccountExists(page: Page, orgName?: string, accountName?: string): Promise<string> {
  const org = orgName ?? newOrgName;
  const account = accountName ?? defaultAccountName;

  logStep(`ensureAccountExists:start account=${account}`);
  await openAccountsView(page, org);
  const accountRow = page.getByRole('row').filter({ hasText: account }).first();
  const createDialog = page.locator('ui5-dialog[open]').first();

  await ensureListRowVisible(page, accountRow);

  if (!await accountRow.isVisible().catch(() => false)) {
    logStep(`ensureAccountExists:create account=${account}`);
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await createDialog.waitFor({ state: 'visible', timeout: 10000 });

    const nameField = page.getByRole('textbox').first();
    await nameField.fill(account);

    // The account create form has a required ui5-select for "Type" with a single value "account".
    // Save stays disabled until it is filled. The first ui5-select on the page is the pagination
    // (value already "5"); the dialog's required Type select is the empty one.
    const typeSelectInfo = await page.evaluate(() => {
      function findRequiredEmptySelect(root: Document | ShadowRoot): { x: number; y: number } | null {
        const elements = root.querySelectorAll('ui5-select');
        for (const el of Array.from(elements)) {
          const value = (el as any).value ?? el.getAttribute('value') ?? '';
          if (el.hasAttribute('required') && !value) {
            const rect = (el as HTMLElement).getBoundingClientRect();
            if (rect.width > 0 && rect.height > 0) {
              return { x: Math.round(rect.left + rect.width / 2), y: Math.round(rect.top + rect.height / 2) };
            }
          }
        }
        for (const el of Array.from(root.querySelectorAll('*'))) {
          if ((el as any).shadowRoot) {
            const found = findRequiredEmptySelect((el as any).shadowRoot);
            if (found) return found;
          }
        }
        return null;
      }
      return findRequiredEmptySelect(document);
    }).catch(() => null);

    if (typeSelectInfo) {
      // Click the select to open the option popover, then use keyboard to navigate to the
      // "account" option. ui5-select emits proper selection events for keyboard navigation,
      // unlike a programmatic .click() on a ui5-option which silently no-ops.
      await page.mouse.click(typeSelectInfo.x, typeSelectInfo.y);
      await page.waitForTimeout(500);

      // The first option is empty (placeholder); the second is "account". Down + Enter selects it.
      await page.keyboard.press('ArrowDown');
      await page.waitForTimeout(100);
      await page.keyboard.press('Enter');
      await page.waitForTimeout(300);
      logStep('ensureAccountExists:type-selected via keyboard');
    } else {
      logStep('ensureAccountExists:no-required-type-select-found');
    }

    const submitButton = page.getByRole('button', { name: /^(Save|Submit)$/ }).first();
    const alreadyExistsAlert = page.getByText(`accounts.core.platform-mesh.io "${account}" already exists`);
    const transientWebhookAlert = page.getByText(/failed calling webhook|connection refused|Internal error occurred/i);

    for (let attempt = 0; attempt < 4; attempt++) {
      await expect(submitButton).toBeEnabled({ timeout: 10000 });
      await submitButton.click();

      const outcome = await Promise.race([
        alreadyExistsAlert.waitFor({ state: 'visible', timeout: 30000 }).then(() => 'exists'),
        transientWebhookAlert.waitFor({ state: 'visible', timeout: 30000 }).then(() => 'retry'),
        createDialog.waitFor({ state: 'hidden', timeout: 30000 }).then(() => 'submitted'),
        accountRow.waitFor({ state: 'visible', timeout: 30000 }).then(() => 'submitted'),
      ]);

      if (outcome === 'submitted' || outcome === 'exists') {
        break;
      }

      if (attempt === 3) {
        throw new Error('Account creation kept failing due to transient webhook errors');
      }

      const alertCloseButton = page.getByRole('button', { name: 'Close' }).first();
      if (await alertCloseButton.isVisible().catch(() => false)) {
        await alertCloseButton.click();
      }
      await page.waitForTimeout(10000);
    }
  }

  if (await createDialog.isVisible().catch(() => false)) {
    const dialogCancelButton = page.getByRole('button', { name: 'Cancel' }).first();
    const dialogCloseButton = page.getByRole('button', { name: 'Close' }).first();

    if (await dialogCancelButton.isVisible().catch(() => false)) {
      await dialogCancelButton.click();
    } else if (await dialogCloseButton.isVisible().catch(() => false)) {
      await dialogCloseButton.click();
    } else {
      await page.keyboard.press('Escape').catch(() => {});
    }

    await createDialog.waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {});
  }

  const alertCloseButton = page.getByRole('button', { name: 'Close' }).first();
  if (await alertCloseButton.isVisible().catch(() => false)) {
    await alertCloseButton.click();
  }

  waitForAccountExists(org, account);
  logStep(`ensureAccountExists:wait-ready account=${account}`);
  waitForAccountReady(org, account);
  const accountUrl = portalOrgUrl(`/home/accounts/${account}/dashboard`, org);
  logStep(`ensureAccountExists:navigate-direct account=${account} url=${accountUrl}`);
  await page.goto(accountUrl, { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('button', { name: 'Download kubeconfig' })).toBeVisible({ timeout: 30000 });
  logStep(`ensureAccountExists:done account=${account}`);
  return accountUrl;
}

async function downloadAccountKubeconfig(page: Page, orgName?: string, accountName?: string): Promise<string> {
  const org = orgName ?? newOrgName;
  const account = accountName ?? defaultAccountName;

  const downloadButton = page.getByRole('button', { name: 'Download kubeconfig' });
  let download = null;

  for (let attempt = 0; attempt < 2; attempt++) {
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 }).catch(() => null);
    await downloadButton.click();
    download = await downloadPromise;
    if (download) {
      break;
    }

    const closeButton = page.locator('ui5-message-strip-alert button, [ref="e6"]');
    if (await closeButton.isVisible().catch(() => false)) {
      await closeButton.click();
    }
    await page.waitForTimeout(5000);
  }

  expect(download).toBeDefined();

  const targetDir = path.join(tmpdir(), 'platform-mesh-e2e');
  if (!existsSync(targetDir)) {
    mkdirSync(targetDir, { recursive: true });
  }

  const kubeconfigPath = path.join(targetDir, `${org}-${account}-oidc.kubeconfig`);
  await download!.saveAs(kubeconfigPath);
  normalizeDownloadedKubeconfig(kubeconfigPath);
  return kubeconfigPath;
}

async function deleteAccount(page: Page, accountUrl: string, orgName?: string, accountName?: string): Promise<void> {
  const org = orgName ?? newOrgName;
  const account = accountName ?? defaultAccountName;

  logStep(`deleteAccount:start url=${accountUrl}`);
  waitForAccountReady(org, account);
  await page.goto(accountUrl, { waitUntil: 'domcontentloaded' });

  for (let attempt = 0; attempt < 3; attempt++) {
    const downloadButton = page.getByRole('button', { name: 'Download kubeconfig' });
    if (await downloadButton.isVisible().catch(() => false)) {
      break;
    }

    if (attempt === 2) {
      throw new Error(`Account dashboard did not become ready before deletion, final URL=${page.url()}`);
    }

    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(3000);
  }

  const deleteSelectors = [
    '[data-testid="delete-resource-button"]',
    'button:has-text("Delete")',
    '[role="button"][aria-label="Delete"]',
  ];

  let clickedDelete = false;
  for (const selector of deleteSelectors) {
    const locator = page.locator(selector).first();
    if (await locator.isVisible().catch(() => false)) {
      logStep(`deleteAccount:clicked-delete-selector selector=${selector}`);
      await clickRobust(locator);
      clickedDelete = true;
      break;
    }
  }

  if (!clickedDelete) {
    const overflowButtons = [
      page.getByRole('button', { name: 'Additional Options' }).last(),
      page.locator('[aria-label="Additional Options"]').last(),
    ];

    for (const overflowButton of overflowButtons) {
      if (await overflowButton.isVisible().catch(() => false)) {
        logStep('deleteAccount:open-additional-options');
        await clickRobust(overflowButton);
        break;
      }
    }

    const overflowDeleteOptions = [
      page.getByRole('menuitem', { name: /delete/i }).last(),
      page.getByRole('button', { name: /delete/i }).last(),
      page.locator('[aria-label="Delete"]').last(),
      page.getByText('Delete', { exact: true }).last(),
    ];

    for (const overflowDeleteOption of overflowDeleteOptions) {
      if (await overflowDeleteOption.isVisible().catch(() => false)) {
        logStep('deleteAccount:clicked-delete-from-overflow');
        await clickRobust(overflowDeleteOption);
        clickedDelete = true;
        break;
      }
    }
  }

  if (!clickedDelete) {
    const visibleButtons = await page.locator('button').evaluateAll((nodes) => nodes
      .map((node) => ({
        text: (node.textContent || '').trim(),
        ariaLabel: node.getAttribute('aria-label') || '',
        title: node.getAttribute('title') || '',
      }))
      .filter((entry) => entry.text || entry.ariaLabel || entry.title));
    throw new Error(`Could not find account delete button on the account detail page, buttons=${JSON.stringify(visibleButtons)}`);
  }

  // Wait for the delete confirmation dialog to appear using test-id
  const deleteDialog = page.locator('[data-testid="delete-resource-dialog"]');
  await expect(deleteDialog).toBeVisible({ timeout: 10000 });

  // Find and fill the confirmation input inside the dialog using test-id
  // UI5 web components have shadow DOM, so we need to target the inner input element
  const confirmInput = page.locator('[data-testid="delete-resource-input"]');
  await expect(confirmInput).toBeVisible({ timeout: 5000 });

  // Fill the input by targeting the actual input element inside the ui5-input shadow root
  const innerInput = confirmInput.locator('input');
  await innerInput.fill(account);
  logStep(`deleteAccount:typed-confirmation account=${account}`);
  await page.waitForTimeout(500);

  const confirmButton = page.locator('[data-testid="delete-resource-confirm"]');
  await expect(confirmButton).toBeVisible({ timeout: 5000 });
  await expect(confirmButton).toBeEnabled({ timeout: 5000 });
  await clickRobust(confirmButton);
  logStep('deleteAccount:clicked-confirm');
}

export { deleteAccount, downloadAccountKubeconfig, ensureAccountExists };
