import { httpbinNamespace, httpbinProviderKubeconfigPath } from './constants';
import { logStep } from './log';
import { runKubectlWithKubeconfig, runRuntimeKubectl } from './runtime';

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
    'apiexports.apis.kcp.io/orchestrate.platform-mesh.io',
    '--for=jsonpath={.spec.resources[?(@.name=="httpbins")].schema}',
    ...timeoutArgs,
  ]);
  logStep('provider:wait:done');
}

export { ensureExampleHttpbinProviderWorkspace };
