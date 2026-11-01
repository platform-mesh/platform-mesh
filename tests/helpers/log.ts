import { newOrgName, portalBaseUrl } from './constants';

function logStep(step: string): void {
  console.log(`[portal-e2e] ${new Date().toISOString()} ${step}`);
}

function portalOrgUrl(pathname = '', orgName?: string): string {
  const org = orgName ?? newOrgName;
  const base = new URL(portalBaseUrl);
  return `${base.protocol}//${org}.${base.host}/${pathname.replace(/^\//, '')}`;
}

export { logStep, portalOrgUrl };
