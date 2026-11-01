import { execFileSync } from 'node:child_process';

import { adminKubeconfigPath, kcpClusterServer } from './constants';
import { logStep } from './log';

type ShardName = 'root' | 'triton';

function runKubectl(args: string[], input?: string): string {
  return execFileSync('kubectl', ['--kubeconfig', adminKubeconfigPath, ...args], {
    encoding: 'utf8',
    input,
  }).trim();
}

// pre-creates workspace type for organization workspace with minimum data in it
// account-operator than patches it with the rest of the data 
// it's needed to pre-create workspace with sharding selector
function createWorkspaceType(typeName: string, parentPath: string): void {
  logStep(`createWorkspaceType:start type=${typeName} parent=${parentPath}`);

  const manifest = `apiVersion: tenancy.kcp.io/v1alpha1
kind: WorkspaceType
metadata:
  name: ${typeName}
spec:
  extend:
    with:
    - name: org
      path: root
`;

  runKubectl(['apply', '--server', kcpClusterServer(parentPath), '-f', '-'], manifest);
  logStep(`createWorkspaceType:done type=${typeName}`);
}

// pre-creates workspace to allocate it on the specified shard
function createWorkspaceWithShard(
  workspaceName: string,
  parentPath: string,
  shardName: ShardName,
  typeName: string,
  typePath: string,
): void {
  logStep(`createWorkspaceWithShard:start workspace=${workspaceName} shard=${shardName}`);

  const manifest = `apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata:
  name: ${workspaceName}
spec:
  type:
    name: ${typeName}
    path: ${typePath}
  location:
    selector:
      matchLabels:
        name: ${shardName}
`;

  runKubectl(['apply', '--server', kcpClusterServer(parentPath), '-f', '-'], manifest);
  logStep(`createWorkspaceWithShard:done workspace=${workspaceName} shard=${shardName}`);
}

function getWorkspaceShardFromAnnotation(workspaceName: string, parentPath: string): string {
  try {
    return runKubectl([
      'get', `workspaces.tenancy.kcp.io/${workspaceName}`,
      '--server', kcpClusterServer(parentPath),
      '-o', 'jsonpath={.metadata.annotations.core\\.kcp\\.io/shard}',
    ]);
  } catch {
    return '';
  }
}

function waitForWorkspaceOnShard(
  workspaceName: string,
  parentPath: string,
  expectedShard: ShardName,
  timeoutSeconds = 180,
): void {
  logStep(`waitForWorkspaceOnShard:start workspace=${workspaceName} shard=${expectedShard} timeout=${timeoutSeconds}s`);

  const server = kcpClusterServer(parentPath);
  const startTime = Date.now();
  const timeoutMs = timeoutSeconds * 1000;

  while (Date.now() - startTime < timeoutMs) {
    try {
      const result = runKubectl([
        'get', `workspaces.tenancy.kcp.io/${workspaceName}`,
        '--server', server,
        '-o', 'jsonpath={.status.phase},{.metadata.annotations.core\\.kcp\\.io/shard}',
      ]);

      const [phase, currentShard] = result.split(',');

      if (phase === 'Ready' && currentShard === expectedShard) {
        logStep(`waitForWorkspaceOnShard:done workspace=${workspaceName} shard=${currentShard}`);
        return;
      }

      execFileSync('sleep', ['2']);
    } catch {
      execFileSync('sleep', ['2']);
    }
  }

  throw new Error(`Workspace ${workspaceName} did not become ready on shard=${expectedShard} within ${timeoutSeconds}s`);
}

function waitForAccountReadyInOrg(accountName: string, orgName: string, timeoutSeconds = 180): void {
  logStep(`waitForAccountReadyInOrg:start account=${accountName} org=${orgName}`);

  runKubectl([
    'wait', '--server', kcpClusterServer(`root:orgs:${orgName}`),
    '--for=condition=Ready', `--timeout=${timeoutSeconds}s`,
    `accounts.core.platform-mesh.io/${accountName}`,
  ]);

  logStep(`waitForAccountReadyInOrg:done account=${accountName}`);
}

function preCreateShardedOrgWorkspace(orgName: string, orgShard: ShardName): void {
  logStep(`preCreateShardedOrgWorkspace:start org=${orgName} shard=${orgShard}`);

  const typeName = `${orgName}-org`;
  createWorkspaceType(typeName, 'root:orgs');
  createWorkspaceWithShard(orgName, 'root:orgs', orgShard, typeName, 'root:orgs');

  logStep(`preCreateShardedOrgWorkspace:done org=${orgName} shard=${orgShard}`);
}

function preCreateShardedAccountWorkspace(accountName: string, orgName: string, accountShard: ShardName): void {
  logStep(`preCreateShardedAccountWorkspace:start account=${accountName} shard=${accountShard}`);

  createWorkspaceWithShard(
    accountName,
    `root:orgs:${orgName}`,
    accountShard,
    `${orgName}-account`,
    'root:orgs',
  );

  logStep(`preCreateShardedAccountWorkspace:done account=${accountName} shard=${accountShard}`);
}

function waitForShardedAccountReady(
  accountName: string,
  orgName: string,
  expectedShard: ShardName,
  timeoutSeconds = 180,
): void {
  logStep(`waitForShardedAccountReady:start account=${accountName} shard=${expectedShard}`);

  waitForWorkspaceOnShard(accountName, `root:orgs:${orgName}`, expectedShard, timeoutSeconds);
  waitForAccountReadyInOrg(accountName, orgName, timeoutSeconds);

  logStep(`waitForShardedAccountReady:done account=${accountName}`);
}

function verifyShardAssignments(
  orgName: string,
  orgShard: ShardName,
  accounts: Array<{ name: string; shard: ShardName }>,
): void {
  logStep('verifyShardAssignments:start');

  const actualOrgShard = getWorkspaceShardFromAnnotation(orgName, 'root:orgs');
  if (actualOrgShard !== orgShard) {
    throw new Error(`Org ${orgName} on wrong shard: expected ${orgShard}, got ${actualOrgShard}`);
  }
  logStep(`verifyShardAssignments:org org=${orgName} shard=${actualOrgShard} ✓`);

  for (const account of accounts) {
    const actualShard = getWorkspaceShardFromAnnotation(account.name, `root:orgs:${orgName}`);
    if (actualShard !== account.shard) {
      throw new Error(`Account ${account.name} on wrong shard: expected ${account.shard}, got ${actualShard}`);
    }
    logStep(`verifyShardAssignments:account account=${account.name} shard=${actualShard} ✓`);
  }

  logStep('verifyShardAssignments:done');
}

export {
  type ShardName,
  preCreateShardedOrgWorkspace,
  preCreateShardedAccountWorkspace,
  waitForShardedAccountReady,
  waitForWorkspaceOnShard,
  verifyShardAssignments,
};
