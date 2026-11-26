import { expect, test } from '@playwright/test';

import { httpbinNamespace, httpbinProviderKubeconfigPath, httpbinProviderPath, kcpClusterServer } from './constants';
import { ensureExampleHttpbinProviderWorkspace, waitForHttpbinBindingReady } from './httpbin-provider';

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
      'wait', 'apiexports.apis.kcp.io/httpbin',
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

const org = 'org-helper-test';
const bindingName = 'httpbin-a1b2c';

function binding(name = bindingName, exportName = 'httpbin', providerPath = httpbinProviderPath) {
  return {
    metadata: { name },
    spec: { reference: { export: { name: exportName, path: providerPath } } },
  };
}

test('waits for the native HTTPBin binding without changing account resources', () => {
  const bindings = [
    binding(),
    binding('another-api', 'another-api'),
    binding('other-provider', 'httpbin', 'root:providers:other'),
  ];
  const calls: string[][] = [];
  waitForHttpbinBindingReady(org, 'account', args => {
    calls.push(args);
    return JSON.stringify({ items: bindings });
  });

  expect(calls.map(args => args[2])).toEqual(['get', 'wait', 'wait']);
  for (const args of calls) expect(args[1]).toBe(kcpClusterServer(`root:orgs:${org}:account`));
  for (const args of calls.slice(1)) expect(args).toContain(`apibindings.apis.kcp.io/${bindingName}`);
  expect(calls.flatMap(args => args.filter(arg => arg.startsWith('--for=')))).toEqual([
    '--for=jsonpath={.status.phase}=Bound',
    '--for=jsonpath={.metadata.finalizers[?(@=="core.platform-mesh.io/apibinding-finalizer")]}',
  ]);
});

const invalidBindings = [
  { name: 'missing', items: [], error: 'found 0' },
  { name: 'ambiguous', items: [binding(), binding('httpbin-second')], error: 'found 2' },
  {
    name: 'terminating',
    items: [{ ...binding(), metadata: { name: bindingName, deletionTimestamp: '2026-11-26T00:00:00Z' } }],
    error: 'already terminating',
  },
];
for (const { name, items, error } of invalidBindings) {
  test(`rejects the ${name} binding before waiting`, () => {
    const calls: string[][] = [];
    expect(() => waitForHttpbinBindingReady(org, 'account', args => {
      calls.push(args);
      return JSON.stringify({ items });
    })).toThrow(error);
    expect(calls).toHaveLength(1);
  });
}
