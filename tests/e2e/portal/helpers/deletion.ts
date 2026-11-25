import { kcpClusterServer } from './constants';
import { logStep } from './log';
import { runAdminKubectl } from './runtime';

const bindingResource = 'apibindings.apis.kcp.io';
const providerPath = 'root:providers:httpbin-provider';
const exportName = 'orchestrate.platform-mesh.io';
const deletionBindingName = 'e2e-httpbin-account-deletion';
const bindingFinalizer = 'core.platform-mesh.io/apibinding-finalizer';

type APIBinding = {
  apiVersion: string;
  metadata: { name: string; deletionTimestamp?: string };
  spec: { reference: { export?: { name: string; path: string } }; [key: string]: unknown };
};

function isHttpbinBinding(binding: APIBinding): boolean {
  const ref = binding.spec.reference.export;
  return ref?.name === exportName && ref.path === providerPath;
}

export function prepareHttpbinDeletionBinding(org: string, account: string, kubectl = runAdminKubectl): void {
  const path = `root:orgs:${org}:${account}`;
  const run = (args: string[], input?: string) => kubectl(['--server', kcpClusterServer(path), ...args], input);
  const waitArgs = ['--timeout=120s', '--request-timeout=120s'];
  const start = Date.now();
  logStep(`deletion:binding-setup:start account=${account}`);

  const bindings: APIBinding[] = JSON.parse(run(['get', bindingResource, '-o', 'json', '--request-timeout=10s'])).items;
  const collision = bindings.find(binding => binding.metadata.name === deletionBindingName);
  if (collision && !isHttpbinBinding(collision)) {
    throw new Error(`${path}: ${deletionBindingName} references another provider`);
  }
  const providerBindings = bindings.filter(isHttpbinBinding);
  const eligibleBindings = providerBindings.filter(binding =>
    !binding.metadata.name.includes('platform-mesh.io') && !binding.metadata.name.includes('kcp.io'));
  if (eligibleBindings.length > 1 || (eligibleBindings.length === 0 && providerBindings.length > 1)) {
    throw new Error(`${path}: the httpbin binding fixture is ambiguous`);
  }
  const existing = eligibleBindings[0];
  const original = existing ?? providerBindings[0];
  if (!original) {
    throw new Error(`${path}: no binding to ${providerPath}:${exportName}; the httpbin fixture is missing`);
  }
  if (original.metadata.deletionTimestamp) {
    throw new Error(`${path}: the httpbin binding fixture is already terminating`);
  }
  const name = existing?.metadata.name ?? deletionBindingName;

  // Use an eligible name so the generator adds its finalizer; default bindings are excluded.
  if (!existing) {
    run(['create', '-f', '-', '--request-timeout=10s'], JSON.stringify({
      apiVersion: original.apiVersion,
      kind: 'APIBinding',
      metadata: { name },
      spec: original.spec,
    }));
  }

  // Release the old binding's resource ownership so the replacement can become Bound.
  const originals = providerBindings.filter(binding => binding.metadata.name !== name);
  if (originals.length > 0) {
    run([
      'delete', ...originals.map(binding => `${bindingResource}/${binding.metadata.name}`),
      '--ignore-not-found', ...waitArgs,
    ]);
  }

  // Require the operator's finalizer to exercise PR #710's blocked deletion case.
  const resource = `${bindingResource}/${name}`;
  run(['wait', resource, '--for=jsonpath={.status.phase}=Bound', ...waitArgs]);
  run(['wait', resource, `--for=jsonpath={.metadata.finalizers[?(@=="${bindingFinalizer}")]}`, ...waitArgs]);
  logStep(`deletion:binding-setup:done account=${account} elapsedMs=${Date.now() - start}`);
}
