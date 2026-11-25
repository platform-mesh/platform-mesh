import { expect, test } from '@playwright/test';

import { kcpClusterServer } from './constants';
import { prepareHttpbinDeletionBinding } from './deletion';

const org = 'org-root-helper-test';
const provider = 'root:providers:httpbin-provider';
const accountPath = `root:orgs:${org}:account`;
const bindingResource = 'apibindings.apis.kcp.io';
const bindingName = 'orchestrate.platform-mesh.io-default';
const replacementName = 'e2e-httpbin-account-deletion';
const finalizer = 'core.platform-mesh.io/apibinding-finalizer';

function binding(name: string, exportName = 'orchestrate.platform-mesh.io', deletionTimestamp?: string) {
  return {
    apiVersion: 'apis.kcp.io/v1alpha2',
    metadata: { name, deletionTimestamp, finalizers: ['apis.kcp.io/apibinding-finalizer'] },
    spec: {
      reference: { export: { name: exportName, path: provider } },
      permissionClaims: [{ group: 'apis.kcp.io', resource: 'apibindings', state: 'Accepted', selector: { matchAll: true } }],
    },
    status: { phase: 'Bound' },
  };
}

function fixture(bindings = [binding(bindingName)]) {
  const calls: { args: string[]; input?: string }[] = [];
  const kubectl = (args: string[], input?: string): string => {
    calls.push({ args, input });
    return JSON.stringify({ items: bindings });
  };
  return { calls, kubectl };
}

test('replaces only the default HTTPBin binding and preserves its permission claims', () => {
  const original = binding(bindingName);
  const { calls, kubectl } = fixture([original, binding('unrelated-provider', 'another-api')]);
  prepareHttpbinDeletionBinding(org, 'account', kubectl);

  const manifest = JSON.parse(calls.find(call => call.args.includes('create'))!.input!);
  expect(manifest).toEqual({
    apiVersion: original.apiVersion, kind: 'APIBinding', metadata: { name: replacementName }, spec: original.spec,
  });
  const deletion = calls.find(call => call.args.includes('delete'))!;
  expect(deletion.args.filter(arg => arg.startsWith(`${bindingResource}/`))).toEqual([`${bindingResource}/${bindingName}`]);
  expect(calls.flatMap(call => call.args.filter(arg => arg.startsWith('--for=')))).toEqual([
    '--for=jsonpath={.status.phase}=Bound',
    `--for=jsonpath={.metadata.finalizers[?(@=="${finalizer}")]}`,
  ]);
  for (const call of calls) expect(call.args).toContain(kcpClusterServer(accountPath));
});

test('reuses an eligible HTTPBin binding without recreating it', () => {
  const { calls, kubectl } = fixture([binding('httpbin')]);
  prepareHttpbinDeletionBinding(org, 'account', kubectl);

  expect(calls.some(call => call.args.includes('create') || call.args.includes('delete'))).toBe(false);
  const waits = calls.filter(call => call.args.includes('wait'));
  expect(waits).toHaveLength(2);
  for (const call of waits) expect(call.args).toContain(`${bindingResource}/httpbin`);
});

const invalidFixtures = [
  { name: 'missing', bindings: [], error: 'fixture is missing' },
  { name: 'ambiguous', bindings: [binding(bindingName), binding('second.platform-mesh.io-binding')], error: 'ambiguous' },
  { name: 'terminating', bindings: [binding(bindingName, undefined, '2026-11-25T00:00:00Z')], error: 'already terminating' },
  { name: 'foreign name collision', bindings: [binding(bindingName), binding(replacementName, 'another-api')], error: 'references another provider' },
];
for (const { name, bindings, error } of invalidFixtures) {
  test(`rejects the ${name} fixture before changing bindings`, () => {
    const { calls, kubectl } = fixture(bindings);
    expect(() => prepareHttpbinDeletionBinding(org, 'account', kubectl)).toThrow(error);
    expect(calls).toHaveLength(1);
  });
}

for (const [name, condition] of [['binding readiness', '.status.phase'], ['the operator finalizer', '.metadata.finalizers']]) {
  test(`propagates a failed wait for ${name}`, () => {
    const { kubectl } = fixture();
    const failure = new Error(`timed out waiting for ${condition}`);
    expect(() => prepareHttpbinDeletionBinding(org, 'account', (args, input) => {
      if (args.some(arg => arg.startsWith('--for=') && arg.includes(condition))) throw failure;
      return kubectl(args, input);
    })).toThrow(failure);
  });
}
