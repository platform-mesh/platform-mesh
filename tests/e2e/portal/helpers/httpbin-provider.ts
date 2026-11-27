import {
  httpbinApiExportName,
  httpbinNamespace,
  httpbinProviderKubeconfigPath,
  httpbinProviderPath,
  kcpClusterServer,
} from './constants';
import { logStep } from './log';
import { runAdminKubectl, runKubectlWithKubeconfig, runRuntimeKubectl } from './runtime';

const providerReadyTimeout = '180s';

function ensureExampleHttpbinProviderWorkspace(
  runtimeKubectl = runRuntimeKubectl,
  providerKubectl = runKubectlWithKubeconfig,
): void {
  logStep('provider:wait:start');
  const timeoutArgs = [
    `--timeout=${providerReadyTimeout}`,
    `--request-timeout=${providerReadyTimeout}`,
  ];

  runtimeKubectl([
    'rollout', 'status', 'deployment/api-syncagent',
    '--namespace', httpbinNamespace,
    ...timeoutArgs,
  ]);
  runtimeKubectl([
    'rollout', 'status', 'deployment/example-httpbin-operator',
    '--namespace', httpbinNamespace,
    ...timeoutArgs,
  ]);
  providerKubectl(httpbinProviderKubeconfigPath, [
    'wait',
    `apiexports.apis.kcp.io/${httpbinApiExportName}`,
    '--for=jsonpath={.spec.resources[?(@.name=="httpbins")].schema}',
    ...timeoutArgs,
  ]);
  logStep('provider:wait:done');
}

type APIBinding = {
  metadata: { name: string; deletionTimestamp?: string };
  spec: { reference: { export?: { name: string; path: string } } };
};

function waitForHttpbinBindingReady(org: string, account: string, kubectl = runAdminKubectl): void {
  const server = kcpClusterServer(`root:orgs:${org}:${account}`);
  const run = (args: string[]) => kubectl(['--server', server, ...args]);
  logStep(`provider:binding:start account=${account}`);

  const bindings: APIBinding[] = JSON.parse(run(['get', 'apibindings.apis.kcp.io', '-o', 'json', '--request-timeout=10s'])).items;
  const matches = bindings.filter(binding =>
    binding.spec.reference.export?.name === httpbinApiExportName && binding.spec.reference.export.path === httpbinProviderPath);
  if (matches.length !== 1) {
    throw new Error(`Expected one HTTPBin binding in ${org}/${account}, found ${matches.length}`);
  }
  const binding = matches[0];
  if (binding.metadata.deletionTimestamp) {
    throw new Error(`HTTPBin binding ${binding.metadata.name} is already terminating`);
  }

  const resource = `apibindings.apis.kcp.io/${binding.metadata.name}`;
  const timeoutArgs = ['--timeout=120s', '--request-timeout=120s'];
  run(['wait', resource, '--for=jsonpath={.status.phase}=Bound', ...timeoutArgs]);
  run(['wait', resource, '--for=jsonpath={.metadata.finalizers[?(@=="core.platform-mesh.io/apibinding-finalizer")]}', ...timeoutArgs]);
  logStep(`provider:binding:done account=${account}`);
}

export { ensureExampleHttpbinProviderWorkspace, waitForHttpbinBindingReady };
