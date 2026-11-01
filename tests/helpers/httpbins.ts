import { expect, Locator, Page } from '@playwright/test';

import {
  defaultHttpBinName,
  exampleDataOverlayPath,
  exampleDataRemoteOverlayPath,
  exampleHttpbinProviderArgocdComponentPath,
  exampleHttpbinProviderFluxcdComponentPath,
  exampleHttpbinProviderRuntimeComponentPath,
  httpbinProviderManifestPath,
  kcpClusterServer,
  newOrgName,
  remoteMode,
  testAccountName,
  testNamespaceHttpBinName,
  testNamespaceName,
} from './constants';
import { logStep, portalOrgUrl } from './log';
import { runAdminKubectl, runInfraKubectl, runRuntimeKubectl } from './runtime';

async function clickRobust(locator: Locator): Promise<void> {
  try {
    await locator.click({ force: true, timeout: 5000 });
    return;
  } catch {
    await locator.evaluate((node) => {
      (node as HTMLElement).click();
    });
  }
}

async function clickFirstVisible(page: Page, selectors: string[]): Promise<void> {
  for (const selector of selectors) {
    const locator = page.locator(selector).first();
    if (await locator.isVisible().catch(() => false)) {
      await locator.click();
      return;
    }
  }

  throw new Error(`None of the selectors were visible: ${selectors.join(', ')}`);
}

async function closeDialogIfVisible(dialog: Locator, page: Page): Promise<void> {
  if (!await dialog.isVisible().catch(() => false)) {
    return;
  }

  const cancelButton = page.getByRole('button', { name: 'Cancel' }).first();
  const closeButton = page.getByRole('button', { name: 'Close' }).first();

  if (await cancelButton.isVisible().catch(() => false)) {
    await clickRobust(cancelButton);
  } else if (await closeButton.isVisible().catch(() => false)) {
    await clickRobust(closeButton);
  }

  await dialog.waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {});
}

function detectRemoteDeploymentTech(): 'fluxcd' | 'argocd' {
  // The argocd namespace is only created on the infra cluster in argocd remote
  // mode.  Falling back to fluxcd matches start.sh's default.
  const out = runInfraKubectl(['get', 'namespace', 'argocd', '--ignore-not-found', '-o', 'name']);
  return out.includes('namespace/argocd') ? 'argocd' : 'fluxcd';
}

function ensureExampleHttpbinProviderWorkspace(): void {
  if (remoteMode) {
    const tech = detectRemoteDeploymentTech();
    const infraComponentPath = tech === 'argocd'
      ? exampleHttpbinProviderArgocdComponentPath
      : exampleHttpbinProviderFluxcdComponentPath;

    // Mirrors the EXAMPLE_DATA branch in start.sh: a remote-aware overlay on
    // runtime (no fluxcd HelmReleases, no profile clobber), the runtime-side
    // component (just the namespace), and the infra-side component carrying
    // the source CRs + HelmReleases/Applications.
    runRuntimeKubectl(['apply', '-k', exampleDataRemoteOverlayPath]);
    runRuntimeKubectl(['apply', '-k', exampleHttpbinProviderRuntimeComponentPath]);
    runInfraKubectl(['apply', '-k', infraComponentPath]);
  } else {
    runRuntimeKubectl(['apply', '-k', exampleDataOverlayPath]);
  }

  runAdminKubectl([
    'create-workspace',
    'providers',
    '--type=root:providers',
    '--ignore-existing',
    '--server',
    kcpClusterServer('root'),
  ]);

  runAdminKubectl([
    'create-workspace',
    'httpbin-provider',
    '--type=root:provider',
    '--ignore-existing',
    '--server',
    kcpClusterServer('root:providers'),
  ]);

  runAdminKubectl([
    'apply',
    '-k',
    httpbinProviderManifestPath,
    '--server',
    kcpClusterServer('root:providers:httpbin-provider'),
  ]);

  // In remote mode the operator creates source CRs + HelmReleases/Applications
  // on infra; in single-cluster mode they live on the runtime cluster.  Wait
  // on whichever cluster owns them.
  const waitKubectl = remoteMode ? runInfraKubectl : runRuntimeKubectl;
  if (remoteMode && detectRemoteDeploymentTech() === 'argocd') {
    // The platform-mesh operator watches Applications in platform-mesh-system.
    waitKubectl([
      'wait',
      '--namespace',
      'platform-mesh-system',
      '--for=jsonpath={.status.health.status}=Healthy',
      'applications.argoproj.io',
      '--timeout=180s',
      'api-syncagent',
    ]);
    waitKubectl([
      'wait',
      '--namespace',
      'platform-mesh-system',
      '--for=jsonpath={.status.health.status}=Healthy',
      'applications.argoproj.io',
      '--timeout=180s',
      'example-httpbin-provider',
    ]);
  } else {
    waitKubectl([
      'wait',
      '--namespace',
      'platform-mesh-system',
      '--for=condition=Ready',
      'helmreleases',
      '--timeout=180s',
      'api-syncagent',
    ]);
    waitKubectl([
      'wait',
      '--namespace',
      'platform-mesh-system',
      '--for=condition=Ready',
      'helmreleases',
      '--timeout=180s',
      'example-httpbin-provider',
    ]);
  }
}

function ensureHttpBinExistsViaBackend(namespaceName: string, httpBinName: string, orgName?: string, accountName?: string): void {
  const org = orgName ?? newOrgName;
  const account = accountName ?? testAccountName;

  const manifest = [
    'apiVersion: orchestrate.platform-mesh.io/v1alpha1',
    'kind: HttpBin',
    'metadata:',
    `  name: ${httpBinName}`,
    `  namespace: ${namespaceName}`,
    '',
  ].join('\n');

  runAdminKubectl([
    'apply',
    '--server',
    kcpClusterServer(`root:orgs:${org}:${account}`),
    '-f',
    '-',
  ], manifest);

  runAdminKubectl([
    'wait',
    '--server',
    kcpClusterServer(`root:orgs:${org}:${account}`),
    '--for=condition=Ready',
    '--timeout=120s',
    '-n',
    namespaceName,
    `httpbins.orchestrate.platform-mesh.io/${httpBinName}`,
  ]);
}

async function ensureExampleHttpbinProvider(page: Page, orgName?: string, accountName?: string): Promise<void> {
  const org = orgName ?? newOrgName;
  const account = accountName ?? testAccountName;

  ensureExampleHttpbinProviderWorkspace();
  const url = portalOrgUrl(`/home/accounts/${account}/orchestrate_platform-mesh_io_httpbins`, org);
  await page.goto(url, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('load', { timeout: 10000 }).catch(() => {});
}

async function openNamespacesView(page: Page): Promise<void> {
  await clickFirstVisible(page, [
    '[data-testid="core_namespaces_namespaces"]',
    '[data-testid="namespaces_namespaces"]',
    '[data-testid="namespace_namespaces"]',
    'a:has-text("Namespaces")',
    '[role="link"]:has-text("Namespaces")',
  ]);

  const namespacesReadyLocators = [
    page.getByRole('button', { name: 'Create', exact: true }),
    page.getByRole('heading', { name: 'Namespaces', exact: true }),
    page.getByText('Namespaces', { exact: true }),
  ];

  await Promise.any(namespacesReadyLocators.map((locator) => locator.waitFor({ state: 'visible', timeout: 15000 })));
}

async function ensureNamespaceExists(page: Page, namespaceName: string): Promise<void> {
  logStep(`ensureNamespaceExists:start namespace=${namespaceName}`);
  await openNamespacesView(page);
  const namespaceRow = page.getByText(namespaceName, { exact: true }).first();
  const createDialog = page.locator('ui5-dialog[open]').first();

  if (await namespaceRow.isVisible().catch(() => false)) {
    return;
  }

  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await createDialog.waitFor({ state: 'visible', timeout: 10000 });
  await page.getByRole('textbox').first().fill(namespaceName);
  const nsSaveButton = page.getByRole('button', { name: /^(Save|Submit)$/ }).first();
  await expect(nsSaveButton).toBeEnabled({ timeout: 5000 });
  await nsSaveButton.click();
  const alreadyExistsAlert = page.getByText(`namespaces \"${namespaceName}\" already exists`);
  await Promise.race([
    expect(namespaceRow).toBeVisible({ timeout: 30000 }),
    alreadyExistsAlert.waitFor({ state: 'visible', timeout: 30000 }),
  ]);

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

  await expect(page.getByText(namespaceName, { exact: true }).first()).toBeVisible({ timeout: 30000 });
  logStep(`ensureNamespaceExists:done namespace=${namespaceName}`);
}

async function openHttpBinsView(page: Page, orgName?: string, accountName?: string): Promise<void> {
  await ensureExampleHttpbinProvider(page, orgName, accountName);

  const httpBinsReadyLocators = [
    page.locator('[data-testid="namespace-selection-combobox"]').first(),
    page.getByRole('button', { name: 'Create' }).first(),
    page.getByRole('heading', { name: 'HttpBins', exact: true }),
  ];

  await Promise.any(httpBinsReadyLocators.map((locator) => locator.waitFor({ state: 'visible', timeout: 15000 })));
}

async function selectNamespaceScope(page: Page, namespaceName: string): Promise<void> {
  const scopeCombobox = page.locator('[data-testid="namespace-selection-combobox"]').first();
  await expect(scopeCombobox).toBeVisible({ timeout: 15000 });
  if ((await scopeCombobox.textContent().catch(() => ''))?.includes(namespaceName)) {
    return;
  }

  await scopeCombobox.click();
  await scopeCombobox.press('F4').catch(() => {});
  await page.waitForTimeout(500);

  const namespaceItem = page.locator(`[data-testid="namespace-selection-combobox-item-${namespaceName}"]`).or(
    page.getByRole('option', { name: namespaceName, exact: true }),
  ).first();

  if (await namespaceItem.isVisible().catch(() => false)) {
    await namespaceItem.click();
    await expect(scopeCombobox).toContainText(namespaceName);
    return;
  }

  const comboInput = scopeCombobox.locator('input').first();
  if (await comboInput.isVisible().catch(() => false)) {
    await comboInput.click();
    await comboInput.fill(namespaceName);
    await comboInput.press('Enter');
    if ((await scopeCombobox.textContent().catch(() => ''))?.includes(namespaceName)) {
      return;
    }
    await page.keyboard.press('Escape').catch(() => {});
  }

  throw new Error(`Unable to select namespace scope ${namespaceName}`);
}

async function ensureHttpBinExists(page: Page, namespaceName: string, httpBinName: string, orgName?: string, accountName?: string): Promise<void> {
  logStep(`ensureHttpBinExists:start namespace=${namespaceName} name=${httpBinName}`);
  for (let attempt = 0; attempt < 2; attempt++) {
    await openHttpBinsView(page, orgName, accountName);
    await selectNamespaceScope(page, namespaceName);

    const httpBinRow = page.getByRole('row').filter({ hasText: httpBinName }).first();
    if (!await httpBinRow.isVisible().catch(() => false)) {
      const createDialog = page.locator('ui5-dialog[open]').first();
      await page.getByRole("button", { name: "Create" }).click();
      await createDialog.waitFor({ state: "visible", timeout: 10000 });

      await page.getByRole('textbox').first().fill(httpBinName);

      // Select namespace in the create dialog dropdown
      const namespaceSelect = page.locator('[data-testid="generic-form-field-metadata.namespace"]').first();
      if (await namespaceSelect.isVisible().catch(() => false)) {
        await namespaceSelect.click();
        await page.waitForTimeout(300);

        // Click on the namespace option
        const namespaceOption = page.locator(`[data-testid="generic-form-field-metadata.namespace-option-${namespaceName}"]`).first();
        await expect(namespaceOption).toBeVisible({ timeout: 5000 });
        await namespaceOption.click();
        await page.waitForTimeout(300);
      }

      const submitButton = page.getByRole('button', { name: /^(Save|Submit)$/ }).first();
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          await expect(submitButton).toBeEnabled({ timeout: 5000 });
        } catch {
          break;
        }
        await submitButton.click();
        const outcome = await Promise.race([
          httpBinRow.waitFor({ state: "visible", timeout: 10000 }).then(() => "exists"),
          createDialog
            .waitFor({ state: "hidden", timeout: 10000 })
            .then(() => "submitted"),
        ]).catch(() => "retry");

        if (outcome === "submitted" || outcome === "exists") {
          break;
        }
      }

      await Promise.race([
        createDialog.waitFor({ state: "hidden", timeout: 30000 }),
        expect(httpBinRow).toBeVisible({ timeout: 30000 }),
      ]).catch(async () => {
        await closeDialogIfVisible(createDialog, page);
      });

      if (await createDialog.isVisible().catch(() => false)) {
        await closeDialogIfVisible(createDialog, page);
      }
    }

    if (await httpBinRow.isVisible().catch(() => false)) {
      ensureHttpBinExistsViaBackend(namespaceName, httpBinName, orgName, accountName);
      logStep(`ensureHttpBinExists:done namespace=${namespaceName} name=${httpBinName}`);
      return;
    }

    logStep(`ensureHttpBinExists:retry namespace=${namespaceName} name=${httpBinName} attempt=${attempt + 1}`);
    await page.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
    await page.waitForTimeout(3000);
  }

  ensureHttpBinExistsViaBackend(namespaceName, httpBinName, orgName, accountName);
  await openHttpBinsView(page, orgName, accountName);
  await selectNamespaceScope(page, namespaceName);
  const nameCell = page.getByRole('row').filter({ hasText: httpBinName }).first();
  await expect(nameCell).toBeVisible({ timeout: 30000 });
  logStep(`ensureHttpBinExists:done namespace=${namespaceName} name=${httpBinName}`);
}

async function assertHttpBinLinkWorks(page: Page, namespaceName: string, httpBinName: string, orgName?: string, accountName?: string): Promise<void> {
  logStep(`assertHttpBinLinkWorks:start namespace=${namespaceName} name=${httpBinName}`);
  await openHttpBinsView(page, orgName, accountName);
  await selectNamespaceScope(page, namespaceName);

  const row = page.getByRole('row').filter({ hasText: httpBinName }).first();
  await expect(row).toBeVisible({ timeout: 30000 });
  const link = row.locator('a[href^="http"]').first();
  await expect(link).toBeVisible({ timeout: 10000 });
  const href = await link.getAttribute('href');
  expect(href).toBeTruthy();

  const httpBinPage = await page.context().newPage();
  await httpBinPage.goto(href!, { waitUntil: 'domcontentloaded' });
  await expect(httpBinPage.locator('body')).toContainText(/httpbin/i, { timeout: 15000 });
  await httpBinPage.close();
  logStep(`assertHttpBinLinkWorks:done namespace=${namespaceName} name=${httpBinName}`);
}

export {
  assertHttpBinLinkWorks,
  clickRobust, defaultHttpBinName, ensureExampleHttpbinProviderWorkspace,
  ensureHttpBinExists,
  ensureNamespaceExists,
  openHttpBinsView,
  selectNamespaceScope, testNamespaceHttpBinName,
  testNamespaceName
};
