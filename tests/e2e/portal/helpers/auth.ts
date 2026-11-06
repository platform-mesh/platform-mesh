import { expect, Page } from '@playwright/test';

import { newOrgName, orgReadyTimeoutSeconds, portalBaseUrl, type TestUser } from './constants';
import { ensureKeycloakTestUser } from './backend';
import { logStep, portalOrgUrl } from './log';

async function loginUser(page: Page, user: TestUser): Promise<void> {
  await page.getByRole('textbox', { name: 'Email' }).fill(user.email);
  await page.getByRole('textbox', { name: 'Password' }).fill(user.password);
  await page.getByRole('button', { name: 'Sign In' }).click();
}

async function loginToInitialKeycloak(page: Page, user: TestUser): Promise<void> {
  await page.getByRole('textbox', { name: 'Email' }).fill(user.email);
  await page.getByRole('textbox', { name: 'Password' }).fill(user.keycloakPassword);
  await page.getByRole('button', { name: 'Sign In' }).click();

  const loginHeading = page.getByRole('heading', { name: 'Sign in to your account' });
  if (await loginHeading.isVisible().catch(() => false)) {
    await page.getByRole('textbox', { name: 'Email' }).fill(user.email);
    await page.getByRole('textbox', { name: 'Password' }).fill(user.password);
    await page.getByRole('button', { name: 'Sign In' }).click();
  }
}

async function registerOrLoginUser(page: Page, user: TestUser): Promise<void> {
  const registerLink = page.locator('text=Register');
  const registrationEnabled = await registerLink.isVisible({ timeout: 2000 }).catch(() => false);

  if (!registrationEnabled) {
    ensureKeycloakTestUser(user);
    await loginUser(page, user);
    await completeAccountSetup(page, user);
    return;
  }

  await registerLink.click();
  await page.fill('input[name="email"]', user.email);
  await page.fill('input[id="password"]', user.password);
  await page.fill('input[id="password-confirm"]', user.password);
  await page.fill('input[id="firstName"]', user.firstName);
  await page.fill('input[id="lastName"]', user.lastName);

  await page.click('input[value="Register"]', { timeout: 5000 });

  const emailExistsError = page.getByText('Email already exists.');
  const welcomeText = page.getByText('Welcome to the Platform Mesh Portal!');

  const result = await Promise.race([
    welcomeText.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'success'),
    emailExistsError.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'exists'),
  ]).catch(() => 'timeout');

  if (result === 'exists') {
    await page.getByRole('link', { name: 'Back to Login' }).click();
    await loginUser(page, user);
  }
}

async function completeAccountSetup(page: Page, user: TestUser): Promise<void> {
  const proceedLink = page.getByRole('link', { name: 'Click here to proceed' });
  const newPasswordField = page.getByRole('textbox', { name: 'New Password' });

  const firstElement = await Promise.race([
    proceedLink.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'proceed'),
    newPasswordField.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'password'),
  ]).catch(() => 'none');

  if (firstElement === 'proceed') {
    await proceedLink.click();
    await newPasswordField.waitFor({ state: 'visible', timeout: 5000 });
  }

  if (firstElement === 'proceed' || firstElement === 'password') {
    await newPasswordField.fill(user.password);
    await page.getByRole('textbox', { name: 'Confirm password' }).fill(user.password);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  const firstNameField = page.getByRole('textbox', { name: 'First name' });
  const backToAppLink = page.getByRole('link', { name: 'Back to Application' });

  const nextElement = await Promise.race([
    firstNameField.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'name'),
    backToAppLink.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'back'),
  ]).catch(() => 'none');

  if (nextElement === 'name') {
    await firstNameField.fill(user.firstName);
    await page.getByRole('textbox', { name: 'Last name' }).fill(user.lastName);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  const backLink = page.getByRole('link', { name: 'Back to Application' });
  if (await backLink.isVisible().catch(() => false)) {
    await backLink.click();
  }

  const emailField = page.getByRole('textbox', { name: 'Email' });
  if (await emailField.isVisible().catch(() => false)) {
    await emailField.fill(user.email);
    await page.getByRole('textbox', { name: 'Password' }).fill(user.password);
    await page.getByRole('button', { name: 'Sign In' }).click();
  }
}

async function settleKeycloakFlow(page: Page, user: TestUser, orgPortalUrl: string): Promise<void> {
  if (page.url().startsWith(orgPortalUrl)) {
    return;
  }

  const loginEmail = page.getByRole('textbox', { name: 'Email' });
  if (await loginEmail.isVisible().catch(() => false)) {
    logStep(`settleKeycloakFlow:login url=${page.url()}`);
    await loginEmail.fill(user.email);
    await page.getByRole('textbox', { name: 'Password' }).fill(user.password);
    await page.getByRole('button', { name: 'Sign In' }).click();

    const invalidCredentials = page.getByText(/invalid username or password/i).first();
    if (await invalidCredentials.isVisible().catch(() => false)) {
      await loginEmail.fill(user.email);
      await page.getByRole('textbox', { name: 'Password' }).fill(user.keycloakPassword);
      await page.getByRole('button', { name: 'Sign In' }).click();
    }
  }

  const newPasswordField = page.getByRole('textbox', { name: 'New Password' });
  if (await newPasswordField.isVisible().catch(() => false)) {
    logStep(`settleKeycloakFlow:update-password url=${page.url()}`);
    await newPasswordField.fill(user.password);
    await page.getByRole('textbox', { name: 'Confirm password' }).fill(user.password);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  const firstNameField = page.getByRole('textbox', { name: 'First name' });
  if (await firstNameField.isVisible().catch(() => false)) {
    logStep(`settleKeycloakFlow:update-profile url=${page.url()}`);
    await firstNameField.fill(user.firstName);
    await page.getByRole('textbox', { name: 'Last name' }).fill(user.lastName);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  const proceedLink = page.getByRole('link', { name: 'Click here to proceed' });
  if (await proceedLink.isVisible().catch(() => false)) {
    logStep(`settleKeycloakFlow:proceed url=${page.url()}`);
    await proceedLink.click();
  }

  const backToApplication = page.getByRole('link', { name: 'Back to Application' });
  if (await backToApplication.isVisible().catch(() => false)) {
    logStep(`settleKeycloakFlow:back-to-app url=${page.url()}`);
    await backToApplication.click();
  }

  await page.waitForLoadState('domcontentloaded', { timeout: 10000 }).catch(() => {});

  if (!page.url().startsWith(orgPortalUrl) && !page.url().includes('/keycloak/')) {
    await gotoWithRetry(page, orgPortalUrl);
  }
}

async function waitForAuthUsableState(page: Page): Promise<void> {
  const registerLink = page.getByText('Register', { exact: true });
  const loginEmail = page.getByRole('textbox', { name: 'Email' });
  const welcomeText = page.getByText('Welcome to the Platform Mesh Portal!');

  const state = await Promise.race([
    registerLink.waitFor({ state: 'visible', timeout: 10000 }).then(() => 'register'),
    loginEmail.waitFor({ state: 'visible', timeout: 10000 }).then(() => 'login'),
    welcomeText.waitFor({ state: 'visible', timeout: 10000 }).then(() => 'welcome'),
  ]).catch(() => 'none');

  if (state === 'none') {
    throw new Error('Portal landing page did not reach a usable auth state');
  }
}

async function gotoWithRetry(page: Page, url: string): Promise<void> {
  await page.goto(url, { waitUntil: 'load', timeout: 15000 });
}

async function fillOrganizationField(page: Page, value: string): Promise<void> {
  const host = page
    .locator('[data-testid="organization-management-onboard-input"]')
    .or(page.locator('ui5-input[placeholder="Enter organization name"]'))
    .first();
  const nativeInput = host.locator('input').first();
  logStep(`fillOrganizationField:start value=${value}`);

  if (await nativeInput.isVisible().catch(() => false)) {
    logStep('fillOrganizationField:using-native-input');
    await nativeInput.click();
    await nativeInput.fill(value);
    await expect(nativeInput).toHaveValue(value, { timeout: 5000 });
    logStep('fillOrganizationField:native-input-filled');
    return;
  }

  logStep('fillOrganizationField:using-ui5-host');
  await host.click();
  await page.keyboard.press('Meta+A').catch(() => {});
  await page.keyboard.press('Control+A').catch(() => {});
  await page.keyboard.press('Backspace').catch(() => {});
  await page.keyboard.type(value, { delay: 100 });

  const typedValue = await host.evaluate((element) => (element as HTMLElement & { value?: string }).value ?? '');
  logStep(`fillOrganizationField:typed-value=${typedValue}`);
  if (typedValue === value) {
    logStep('fillOrganizationField:keyboard-typing-worked');
    return;
  }

  await host.evaluate((element, nextValue) => {
    const ui5Input = element as HTMLElement & { value?: string };
    ui5Input.value = nextValue;
    ui5Input.setAttribute('value', nextValue);
    ui5Input.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true, data: nextValue, inputType: 'insertText' }));
    ui5Input.dispatchEvent(new Event('change', { bubbles: true, composed: true }));
  }, value);
  await expect.poll(
    async () => await host.evaluate((element) => (element as HTMLElement & { value?: string }).value ?? ''),
    { timeout: 5000 },
  ).toBe(value);
  logStep('fillOrganizationField:ui5-host-filled');
}

async function openOrganizationSwitchDropdown(page: Page): Promise<void> {
  const selectRoot = page.locator('.ui5-select-root').first();
  const opener = selectRoot.locator('[part="icon-wrapper"]').first();
  const option = page.getByRole('option').first();

  logStep('openOrganizationSwitchDropdown:start');
  await selectRoot.click({ force: true }).catch(() => {});
  await page.waitForTimeout(300);

  if (!await option.isVisible().catch(() => false)) {
    await opener.click({ force: true }).catch(() => {});
    await page.waitForTimeout(300);
  }

  if (!await option.isVisible().catch(() => false)) {
    await page.evaluate(() => {
      const root = document.querySelector('.ui5-select-root') as HTMLElement | null;
      const icon = root?.querySelector('[part="icon-wrapper"]') as HTMLElement | null;
      icon?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, composed: true }));
      icon?.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, composed: true }));
      icon?.dispatchEvent(new MouseEvent('click', { bubbles: true, composed: true }));
      root?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, composed: true }));
      root?.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, composed: true }));
      root?.dispatchEvent(new MouseEvent('click', { bubbles: true, composed: true }));
    }).catch(() => {});
    await page.waitForTimeout(300);
  }
  logStep('openOrganizationSwitchDropdown:done');
}

async function selectExistingOrganization(page: Page, value: string): Promise<boolean> {
  logStep(`selectExistingOrganization:start value=${value}`);

  await openOrganizationSwitchDropdown(page);

  const optionTexts = await page.locator('ui5-option')
    .evaluateAll((elements) =>
      elements
        .map((element) => (element.textContent ?? '').trim())
        .filter(Boolean),
    )
    .catch(() => []);
  const optionIndex = optionTexts.findIndex((text) => text === value);

  if (optionIndex === -1) {
    logStep(`selectExistingOrganization:not-found value=${value} options=${optionTexts.join(',') || 'none'}`);
    await page.keyboard.press('Escape').catch(() => {});
    return false;
  }

  const optionHost = page.locator('ui5-option').nth(optionIndex);
  logStep(`selectExistingOrganization:click value=${value}`);

  try {
    await optionHost.click({ force: true });
  } catch {
    logStep(`selectExistingOrganization:click-fallback value=${value}`);
    await optionHost.evaluate((el) => (el as HTMLElement).click());
  }

  await page.waitForTimeout(500);

  return true;
}

async function waitForOrganizationSwitchReady(page: Page): Promise<void> {
  const switchButton = page.locator('[data-testid="organization-management-switch-button"]').locator('button');

  await expect.poll(async () => {
    const enabled = await switchButton.isEnabled().catch(() => false);
    const title = await switchButton.getAttribute('title').catch(() => '');

    return {
      enabled,
      title,
    };
  }, { timeout: Number(orgReadyTimeoutSeconds) * 1000 }).toMatchObject({
    enabled: true,
  });
}

async function ensureWelcomePage(page: Page, user: TestUser): Promise<void> {
  await gotoWithRetry(page, portalBaseUrl);
  await page.waitForLoadState('domcontentloaded', { timeout: 10000 }).catch(() => {});

  await waitForAuthUsableState(page);

  await registerOrLoginUser(page, user);
  await page.waitForURL(`${portalBaseUrl}**`, { timeout: 10000 }).catch(() => {});
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});

  const landingState = await Promise.race([
    page.getByText('Welcome to the Platform Mesh Portal!').waitFor({ state: 'visible', timeout: 10000 }).then(() => 'welcome'),
    page.locator('[data-testid="organization-management-input"]').waitFor({ state: 'visible', timeout: 10000 }).then(() => 'org-management'),
    page.getByText("Welcome! Let's get started.", { exact: true }).waitFor({ state: 'visible', timeout: 10000 }).then(() => 'home'),
  ]).catch(() => 'none');

  if (landingState === 'none') {
    throw new Error(`Portal welcome page did not reach a usable post-login state, final URL=${page.url()}`);
  }
}

async function ensurePortalHome(page: Page, user?: TestUser, orgName?: string): Promise<void> {
  const org = orgName ?? newOrgName;
  const orgPortalBaseUrl = portalOrgUrl('/', org);

  logStep(`ensurePortalHome:start url=${page.url()}`);
  if (!page.url().startsWith(orgPortalBaseUrl)) {
    await gotoWithRetry(page, `${orgPortalBaseUrl}home`);
  }

  const loginButton = page.getByRole('button', { name: 'Sign In' });
  if (page.url().includes('/keycloak/') && await loginButton.isVisible().catch(() => false)) {
    if (user) {
      await loginUser(page, user);
    } else {
      await loginButton.click();
    }
  }

  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});

  // Production portal shows the org nav; dev setup shows a welcome text. Accept either.
  const portalReady = page.getByRole('button', { name: 'Platform Mesh Portal' })
    .or(page.getByRole('treeitem', { name: 'Overview' }))
    .or(page.getByText("Welcome! Let's get started.", { exact: true }))
    .first();
  await expect(portalReady).toBeVisible({ timeout: 15000 });
  logStep(`ensurePortalHome:done url=${page.url()}`);
}

async function switchToOrganization(page: Page, user: TestUser, createIfMissing: boolean, orgName?: string): Promise<void> {
  const org = orgName ?? newOrgName;
  const switchButton = page.locator('[data-testid="organization-management-switch-button"]').locator('button');
  const orgPortalUrl = portalOrgUrl('/home', org);
  const orgPortalBaseUrl = portalOrgUrl('/', org);
  const existingOrgAlert = page.getByText(new RegExp(`organization.*${org}.*already exists|${org}.*already exists`, 'i')).first();
  const alertCloseButton = page.getByRole('button', { name: 'Close' }).first();

  let orgSelected = await selectExistingOrganization(page, org);

  if (createIfMissing) {
    const onboardButton = page.locator('[data-testid="organization-management-onboard-button"]').locator('button');
    if (!orgSelected && await onboardButton.isVisible().catch(() => false)) {
      logStep(`switchToOrganization:onboard-missing-org org=${org}`);
      await fillOrganizationField(page, org);
      await onboardButton.click();

      if (await existingOrgAlert.isVisible().catch(() => false)) {
        logStep(`switchToOrganization:org-already-exists org=${org}`);
        if (await alertCloseButton.isVisible().catch(() => false)) {
          await alertCloseButton.click().catch(() => {});
        }
      }

      orgSelected = await selectExistingOrganization(page, org);
      await waitForOrganizationSwitchReady(page);
    }
  }

  if (!orgSelected) {
    throw new Error(`Organization ${org} was not found in the switch dropdown`);
  }

  await switchButton.waitFor({ state: 'visible', timeout: 60000 });
  await waitForOrganizationSwitchReady(page);
  await switchButton.click();

  await page.waitForLoadState('domcontentloaded', { timeout: 15000 }).catch(() => {});
  await waitForAuthUsableState(page);
  await loginToInitialKeycloak(page, user);
  await completeAccountSetup(page, user);
  await gotoWithRetry(page, orgPortalUrl);
  await settleKeycloakFlow(page, user, orgPortalUrl);

  if (!page.url().startsWith(orgPortalBaseUrl)) {
    throw new Error(`Organization switch did not land on ${orgPortalBaseUrl}, final URL=${page.url()}`);
  }

  await ensurePortalHome(page, user, org);
}

async function inviteUserToOrg(page: Page, userEmailToInvite: string): Promise<void> {
  logStep(`inviteUserToOrg:start email=${userEmailToInvite}`);

  await page.locator('[data-testid="members_members"]').click();
  await page.waitForLoadState('networkidle', { timeout: 10000 });

  const membersFrame = page.frameLocator('iframe[src*="organization/members"]');
  const addButton = membersFrame.locator('[data-testid="app-iam-member-list-add-button"]');
  await addButton.waitFor({ state: 'visible', timeout: 10000 });
  await expect(addButton).toBeEnabled({ timeout: 10000 });
  await addButton.scrollIntoViewIfNeeded();
  await addButton.click();

  const addMembersFrame = page.frameLocator('iframe[src*="organization/add-members"]');
  const userInput = addMembersFrame.locator('[data-testid="app-iam-member-add-dialog-user-search-input"]').locator('input');
  await userInput.waitFor({ state: 'visible', timeout: 10000 });
  await userInput.fill(userEmailToInvite);
  await addMembersFrame.getByRole('button', { name: 'Select Options' }).click();
  await page.waitForTimeout(1000);

  const inviteInFrame = addMembersFrame.locator('li.fd-combobox-list-item').filter({ hasText: 'Invite' }).filter({ hasText: userEmailToInvite });
  const inviteInPage = page.locator('li.fd-combobox-list-item').filter({ hasText: 'Invite' }).filter({ hasText: userEmailToInvite });
  const inviteOption = inviteInFrame.or(inviteInPage);
  await inviteOption.waitFor({ state: 'visible', timeout: 10000 });
  await inviteOption.click();
  await expect(addMembersFrame.getByRole('row').filter({ hasText: userEmailToInvite })).toBeVisible({ timeout: 15000 });
  await addMembersFrame.locator('[data-testid="app-iam-member-add-dialog-add-button"]').click();

  const memberEmailInList = membersFrame.locator('span.member-extra-information').filter({ hasText: userEmailToInvite });
  await expect(memberEmailInList).toBeVisible({ timeout: 15000 });

  logStep(`inviteUserToOrg:done email=${userEmailToInvite}`);
}

export {
  ensurePortalHome,
  ensureWelcomePage,
  fillOrganizationField,
  inviteUserToOrg,
  selectExistingOrganization,
  switchToOrganization,
};
