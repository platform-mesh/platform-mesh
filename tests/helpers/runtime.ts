import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';

import { adminKubeconfigPath, infraKubeconfigPath, kindContext, remoteMode, runtimeKubeconfigPath } from './constants';

function runKubectlWithKubeconfig(kubeconfigPath: string, args: string[], input?: string): string {
  return execFileSync('kubectl', ['--kubeconfig', kubeconfigPath, ...args], {
    encoding: 'utf8',
    input,
  }).trim();
}

function runAdminKubectl(args: string[], input?: string): string {
  return execFileSync('kubectl', ['--kubeconfig', adminKubeconfigPath, ...args], {
    encoding: 'utf8',
    input,
  }).trim();
}

// In remote mode, pin to the runtime kubeconfig explicitly so we don't depend
// on the developer's ambient KUBECONFIG pointing at the right cluster.  In
// single-cluster mode, prefer the development runtime kubeconfig when present,
// otherwise fall back to the kind-platform-mesh context.
function runRuntimeKubectl(args: string[], input?: string): string {
  if (remoteMode || existsSync(runtimeKubeconfigPath)) {
    return runKubectlWithKubeconfig(runtimeKubeconfigPath, args, input);
  }
  const { KUBECONFIG: _omit, ...envWithoutKubeconfig } = process.env;
  return execFileSync('kubectl', ['--context', kindContext, ...args], {
    encoding: 'utf8',
    env: envWithoutKubeconfig,
    input,
  }).trim();
}

// Only meaningful in remote mode (operator + flux/argo run on infra).  Callers
// should guard with `remoteMode` before invoking.
function runInfraKubectl(args: string[], input?: string): string {
  return runKubectlWithKubeconfig(infraKubeconfigPath, args, input);
}

export { runKubectlWithKubeconfig, runAdminKubectl, runInfraKubectl, runRuntimeKubectl };
