import { Page } from '@playwright/test';

import { logStep } from './log';
import type { TestUser } from './constants';

async function expectUnauthorizedAccountAccess(page: Page, accountUrl: string, user?: TestUser): Promise<void> {
  logStep(`expectUnauthorizedAccountAccess:start url=${accountUrl}`);
  await page.goto(accountUrl, { waitUntil: 'domcontentloaded' });

  const emailField = page.getByRole('textbox', { name: 'Email' });
  if (user) {
    await Promise.race([
      page.waitForURL(/\/keycloak\//, { timeout: 10000 }),
      emailField.waitFor({ state: 'visible', timeout: 10000 }),
    ]).catch(() => {});
  }

  if (user && await emailField.isVisible().catch(() => false)) {
    await emailField.fill(user.email);
    await page.getByRole('textbox', { name: 'Password' }).fill(user.password);
    await page.getByRole('button', { name: 'Sign In' }).click();

    const invalidCredentials = page.getByText(/invalid username or password/i).first();
    if (await invalidCredentials.isVisible().catch(() => false)) {
      await emailField.fill(user.email);
      await page.getByRole('textbox', { name: 'Password' }).fill(user.keycloakPassword);
      await page.getByRole('button', { name: 'Sign In' }).click();
    }
  }

  if (/\/error\/403(?:$|[/?#])/.test(page.url())) {
    logStep(`expectUnauthorizedAccountAccess:denied-via=url-403 final-url=${page.url()}`);
    logStep(`expectUnauthorizedAccountAccess:done url=${accountUrl}`);
    return;
  }

  if (page.url().includes('/keycloak/')) {
    logStep(`expectUnauthorizedAccountAccess:denied-via=auth-challenge final-url=${page.url()}`);
    logStep(`expectUnauthorizedAccountAccess:done url=${accountUrl}`);
    return;
  }

  const forbiddenMessage = page.getByText(/not authorized|unauthorized|forbidden|access denied/i).first();

  const denied = await Promise.race([
    page.waitForURL(/\/error\/403(?:$|[/?#])/, { timeout: 30000 }).then(() => 'url-403'),
    forbiddenMessage.waitFor({ state: 'visible', timeout: 30000 }).then(() => 'message'),
  ]).catch(() => null);

  if (!denied) {
    const pageText = await page.locator('body').innerText().catch(() => '');
    throw new Error(`Expected unauthorized access denial for ${accountUrl}, final URL=${page.url()}, body=${pageText.slice(0, 500)}`);
  }

  logStep(`expectUnauthorizedAccountAccess:denied-via=${denied} final-url=${page.url()}`);
  logStep(`expectUnauthorizedAccountAccess:done url=${accountUrl}`);
}

export { expectUnauthorizedAccountAccess };
