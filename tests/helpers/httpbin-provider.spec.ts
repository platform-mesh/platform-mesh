import { expect, test } from '@playwright/test';

import { httpbinNamespace, httpbinProviderKubeconfigPath } from './constants';
import { ensureExampleHttpbinProviderWorkspace } from './httpbin-provider';

type Call = { target: 'runtime' | 'provider'; args: string[]; kubeconfig?: string };

const expectedCalls: Call[] = [
  {
    target: 'runtime',
    args: [
      'rollout', 'status', 'deployment/api-syncagent',
      '--namespace', httpbinNamespace,
      '--timeout=180s', '--request-timeout=180s',
    ],
  },
  {
    target: 'runtime',
    args: [
      'rollout', 'status', 'deployment/example-httpbin-operator',
      '--namespace', httpbinNamespace,
      '--timeout=180s', '--request-timeout=180s',
    ],
  },
  {
    target: 'provider',
    kubeconfig: httpbinProviderKubeconfigPath,
    args: [
      'wait', 'apiexports.apis.kcp.io/orchestrate.platform-mesh.io',
      '--for=jsonpath={.spec.resources[?(@.name=="httpbins")].schema}',
      '--timeout=180s', '--request-timeout=180s',
    ],
  },
];

test('waits for the provider deployments and API without provisioning resources', () => {
  const calls: Call[] = [];
  ensureExampleHttpbinProviderWorkspace(
    args => { calls.push({ target: 'runtime', args }); return ''; },
    (kubeconfig, args) => { calls.push({ target: 'provider', kubeconfig, args }); return ''; },
  );
  expect(calls).toEqual(expectedCalls);
});

for (const [index, stage] of ['api-syncagent', 'HTTPBin operator', 'API publication'].entries()) {
  test(`stops and propagates ${stage} failures`, () => {
    const calls: Call[] = [];
    const failure = new Error(`${stage} unavailable`);
    const run = (call: Call): string => {
      calls.push(call);
      if (calls.length === index + 1) {
        throw failure;
      }
      return '';
    };

    let caught: unknown;
    try {
      ensureExampleHttpbinProviderWorkspace(
        args => run({ target: 'runtime', args }),
        (kubeconfig, args) => run({ target: 'provider', kubeconfig, args }),
      );
    } catch (error) {
      caught = error;
    }
    expect(caught).toBe(failure);
    expect(calls).toEqual(expectedCalls.slice(0, index + 1));
  });
}
