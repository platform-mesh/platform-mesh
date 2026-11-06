import test, { expect, Page } from '@playwright/test';

import { inviteUserToOrg, selectExistingOrganization, ensureWelcomePage } from './helpers/auth';
import { invitedUser, portalBaseUrl, primaryUser, runId, orgReadyTimeoutSeconds } from './helpers/constants';
import { logStep } from './helpers/log';
import { ensureAccountExists } from './helpers/account';
import { ensureKeycloakTestUser } from './helpers/backend';
import {
  type ShardName,
  preCreateShardedOrgWorkspace,
  preCreateShardedAccountWorkspace,
  waitForShardedAccountReady,
  waitForWorkspaceOnShard,
  verifyShardAssignments,
} from './helpers/sharding';

// ORG_SHARD controls which shard to create the org on.
// SHARDING_ACCOUNT_SHARDS is a comma-separated list of shards for accounts (default: triton,root).
// Set SHARDING_ACCOUNT_SHARDS=root for single-shard setups (e.g. remote development setup).
// SKIP_INVITE=true skips the inviteUserToOrg step (required for setups without example-data/mailpit).
const ORG_SHARD: ShardName = (process.env.SHARDING_ORG_SHARD as ShardName) || 'triton';
const ACCOUNT_SHARDS: ShardName[] = process.env.SHARDING_ACCOUNT_SHARDS
  ? (process.env.SHARDING_ACCOUNT_SHARDS.split(',') as ShardName[])
  : ['triton', 'root'];
const SKIP_INVITE = process.env.SKIP_INVITE === 'true';

const orgName = `org-${ORG_SHARD}-${runId}`;
const accounts = ACCOUNT_SHARDS.map((shard) => ({
  name: `acc-${shard}-${runId}`,
  shard,
}));

async function completeKeycloakSetup(page: Page): Promise<void> {
  // Handle password update flow
  const newPasswordField = page.getByRole('textbox', { name: 'New Password' });
  if (await newPasswordField.isVisible().catch(() => false)) {
    await newPasswordField.fill(primaryUser.password);
    await page.getByRole('textbox', { name: 'Confirm password' }).fill(primaryUser.password);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  // Handle profile update flow
  const firstNameField = page.getByRole('textbox', { name: 'First name' });
  if (await firstNameField.isVisible().catch(() => false)) {
    await firstNameField.fill(primaryUser.firstName);
    await page.getByRole('textbox', { name: 'Last name' }).fill(primaryUser.lastName);
    await page.getByRole('button', { name: 'Submit' }).click();
  }

  // Handle "back to application" link
  const backLink = page.getByRole('link', { name: 'Back to Application' });
  if (await backLink.isVisible().catch(() => false)) {
    await backLink.click();
  }

  // Handle redirect back to login after completing setup flows above
  if (page.url().includes('/keycloak/')) {
    const emailField = page.getByRole('textbox', { name: 'Email' });
    if (await emailField.isVisible({ timeout: 3000 }).catch(() => false)) {
      await emailField.fill(primaryUser.email);
      await page.getByRole('textbox', { name: 'Password' }).fill(primaryUser.password);
      await page.getByRole('button', { name: 'Sign In' }).click();
    }
  }
}

test.describe('Home Page', () => {
  test.setTimeout(600000);

  test('Register and navigate to portal', async ({ page }) => {
    logStep(`setup:start org=${orgName} shard=${ORG_SHARD}`);

    // 1. Register/login
    await ensureWelcomePage(page, primaryUser);

    // 2. Pre-create org workspace with shard selector
    preCreateShardedOrgWorkspace(orgName, ORG_SHARD);

    // 3. Create org via UI
    await page.locator('[data-testid="organization-management-input"]').locator('input').fill(orgName);
    await page.locator('[data-testid="organization-management-onboard-button"]').locator('button').click();

    // 4. Wait for org workspace to be ready on shard
    waitForWorkspaceOnShard(orgName, 'root:orgs', ORG_SHARD, Number(orgReadyTimeoutSeconds));

    // 5. Switch to org
    await selectExistingOrganization(page, orgName);
    const switchButton = page.locator('[data-testid="organization-management-switch-button"]').locator('button');
    await expect(switchButton).toBeEnabled({ timeout: 100000 });
    await switchButton.click();

    // 6. Complete Keycloak login for the org realm. The portal creates a new
    // Keycloak realm per org; ensure the test user exists there before logging in.
    ensureKeycloakTestUser(primaryUser, orgName);
    await page.getByRole('textbox', { name: 'Email' }).fill(primaryUser.email);
    await page.getByRole('textbox', { name: 'Password' }).fill(primaryUser.password);
    await page.getByRole('button', { name: 'Sign In' }).click();
    await completeKeycloakSetup(page);
    await page.waitForURL(`https://${orgName}.${new URL(portalBaseUrl).host}/**`, { timeout: 30000 });
    await expect(page.getByRole('button', { name: 'Platform Mesh Portal' }).or(
      page.getByText("Welcome! Let's get started.", { exact: true })
    ).first()).toBeVisible({ timeout: 15000 });

    // 7. Invite user (requires example-data/mailpit; skip in minimal setups)
    if (!SKIP_INVITE) {
      await inviteUserToOrg(page, invitedUser.email);
    }

    // 8. Create accounts with shard selectors
    for (const account of accounts) {
      preCreateShardedAccountWorkspace(account.name, orgName, account.shard);
      await ensureAccountExists(page, orgName, account.name);
      waitForShardedAccountReady(account.name, orgName, account.shard);
      logStep(`setup:account-created ${account.name}@${account.shard}`);
    }

    // 9. Verify shard assignments
    verifyShardAssignments(orgName, ORG_SHARD, accounts);

    logStep(`setup:done org=${orgName}`);
  });
});
