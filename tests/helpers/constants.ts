import { existsSync } from 'node:fs';
import path from 'node:path';

const baseDomain = process.env.BASE_DOMAIN || 'portal.localhost';
const portalBaseUrl = process.env.PORTAL_BASE_URL || 'https://portal.localhost:8443/';
const kcpUrl = process.env.KCP_URL || (baseDomain === 'portal.localhost' ? `https://kcp.api.${baseDomain}:8443` : `https://kcp.api.${baseDomain}`);
const kindContext = process.env.KIND_CONTEXT || 'kind-platform-mesh';
const runId = process.env.TEST_RUN_ID || `${Date.now()}`.slice(-8);
const newOrgName = process.env.ORG_NAME || 'default';
const testAccountName = process.env.TEST_ACCOUNT_NAME || `testaccount-${runId}`;
const testNamespaceName = process.env.TEST_NAMESPACE_NAME || `test-${runId}`;
const defaultHttpBinName = process.env.DEFAULT_HTTPBIN_NAME || `test-default-${runId}`;
const testNamespaceHttpBinName = process.env.TEST_NAMESPACE_HTTPBIN_NAME || `test-namespace-${runId}`;
const kubeconfigSmokeConfigMapName = process.env.KUBECONFIG_SMOKE_CONFIGMAP_NAME || 'oidc-kubeconfig-smoke-test';
const inviteName = process.env.INVITE_NAME || `invite-${runId}`;
const accountReadyTimeoutSeconds = process.env.ACCOUNT_READY_TIMEOUT_SECONDS || '180';
const orgReadyTimeoutSeconds = process.env.ORG_READY_TIMEOUT_SECONDS || '300';
const keycloakBaseUrl = process.env.KEYCLOAK_BASE_URL || 'https://portal.localhost:8443/keycloak';
const keycloakAdminUser = process.env.KEYCLOAK_ADMIN_USER || 'keycloak-admin';
const keycloakAdminPassword = process.env.KEYCLOAK_ADMIN_PASSWORD || 'admin';
const repoRoot = path.resolve(process.cwd(), '..');
const kcpRootCaPath = process.env.KCP_ROOT_CA_PATH || path.join(repoRoot, 'local-setup', 'scripts', 'certs', 'root-ca.crt');
const mkcertCaPath = process.env.MKCERT_CA_PATH || path.join(repoRoot, 'local-setup', 'scripts', 'certs', 'ca.crt');
const adminKubeconfigPath = process.env.ADMIN_KUBECONFIG || path.join(repoRoot, '.secret/kcp/admin.kubeconfig');
const httpbinProviderManifestPath = path.join(repoRoot, 'local-setup', 'example-data', 'root', 'providers', 'httpbin-provider');
const exampleDataOverlayPath = path.join(repoRoot, 'local-setup', 'kustomize', 'overlays', 'example-data-sharded');
const exampleDataRemoteOverlayPath = path.join(repoRoot, 'local-setup', 'kustomize', 'overlays', 'example-data-remote');
const exampleHttpbinProviderRuntimeComponentPath = path.join(repoRoot, 'local-setup', 'kustomize', 'components', 'example-httpbin-provider-runtime');
const exampleHttpbinProviderFluxcdComponentPath = path.join(repoRoot, 'local-setup', 'kustomize', 'components', 'example-httpbin-provider-fluxcd');
const exampleHttpbinProviderArgocdComponentPath = path.join(repoRoot, 'local-setup', 'kustomize', 'components', 'example-httpbin-provider-argocd');

// Remote mode = both kubeconfigs are present (start.sh writes both when invoked
// with --remote).  Allows the e2e helpers to switch overlay/wait targets without
// the Taskfile having to thread an extra env var through.
const infraKubeconfigPath = process.env.INFRA_KUBECONFIG || path.join(repoRoot, '.secret/platform-mesh-infra.kubeconfig');
const runtimeKubeconfigPath = process.env.RUNTIME_KUBECONFIG || path.join(repoRoot, '.secret/platform-mesh.kubeconfig');
const remoteMode = existsSync(infraKubeconfigPath) && existsSync(runtimeKubeconfigPath);
const exampleMarketplaceEntryName = process.env.EXAMPLE_MARKETPLACE_ENTRY_NAME || 'orchestrate.platform-mesh.io-orchestrate.platform-mesh.io';
const exampleProviderDisplayName = process.env.EXAMPLE_PROVIDER_DISPLAY_NAME || 'ABC MSP Provider';

type TestUser = {
  email: string;
  password: string;
  firstName: string;
  lastName: string;
  keycloakPassword: string;
};

const primaryUser: TestUser = {
  email: process.env.TEST_USER_EMAIL || 'username@sap.com',
  password: process.env.TEST_USER_PASSWORD || 'MyPass1234',
  firstName: process.env.TEST_USER_FIRST_NAME || 'Firstname',
  lastName: process.env.TEST_USER_LAST_NAME || 'Lastname',
  keycloakPassword: process.env.TEST_USER_KEYCLOAK_PASSWORD || 'password',
};

const invitedUser: TestUser = {
  email: process.env.INVITED_USER_EMAIL || 'inviteusername@sap.com',
  password: process.env.INVITED_USER_PASSWORD || 'MyPass1234',
  firstName: process.env.INVITED_USER_FIRST_NAME || 'Invited',
  lastName: process.env.INVITED_USER_LAST_NAME || 'User',
  keycloakPassword: process.env.INVITED_USER_KEYCLOAK_PASSWORD || 'password',
};

function kcpClusterServer(clusterPath: string): string {
  return `${kcpUrl}/clusters/${clusterPath}`;
}

export {
  portalBaseUrl,
  baseDomain,
  kcpUrl,
  kindContext,
  kcpClusterServer,
  runId,
  newOrgName,
  testAccountName,
  testNamespaceName,
  defaultHttpBinName,
  testNamespaceHttpBinName,
  kubeconfigSmokeConfigMapName,
  inviteName,
  accountReadyTimeoutSeconds,
  orgReadyTimeoutSeconds,
  keycloakBaseUrl,
  keycloakAdminUser,
  keycloakAdminPassword,
  repoRoot,
  kcpRootCaPath,
  mkcertCaPath,
  adminKubeconfigPath,
  httpbinProviderManifestPath,
  exampleDataOverlayPath,
  exampleDataRemoteOverlayPath,
  exampleHttpbinProviderRuntimeComponentPath,
  exampleHttpbinProviderFluxcdComponentPath,
  exampleHttpbinProviderArgocdComponentPath,
  infraKubeconfigPath,
  runtimeKubeconfigPath,
  remoteMode,
  exampleMarketplaceEntryName,
  exampleProviderDisplayName,
  primaryUser,
  invitedUser,
};
export type { TestUser };
