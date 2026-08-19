/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package restore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const identityValidationSubroutineName = "identity-validation"

type IdentityValidationSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

func NewIdentityValidationSubroutine(cli ctrlruntimeclient.Client) *IdentityValidationSubroutine {
	return &IdentityValidationSubroutine{name: identityValidationSubroutineName, client: cli}
}

func (i *IdentityValidationSubroutine) GetName() string {
	return i.name
}

func (i *IdentityValidationSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected pmbackupv1alpha1.PlatformRestore, got %T", obj)
	}

	if pr.Status.Phase == pmbackupv1alpha1.RestorePhaseFailed {
		return subroutines.OK(), false, nil
	}
	if pr.Status.Phase == pmbackupv1alpha1.RestorePhaseSucceeded {
		// A restore that was marked successful by an older operator may still
		// have a Portal bootstrap credential that cannot read its initial KCP
		// resources. Permit a metadata-only reconciliation to repair and check
		// that state without replaying the destructive restore stages.
		result, changed := i.repairPortalAccess(ctx, pr)
		return result, changed, nil
	}

	if !conditionIsTrue(pr, conditionPlatformRecovered) {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for platform recovery"), false, nil
	}

	if changed := setPhase(pr, pmbackupv1alpha1.RestorePhaseValidatingIdentity); changed {
		return subroutines.StopWithRequeue(time.Second, "phase set to ValidatingIdentity"), true, nil
	}

	// Platform recovery is intentionally terminal once its recovery conditions
	// have been recorded. Keep these repairs here as well: a later operator
	// version must be able to recover an already-running restore that is blocked
	// on KCP credentials or APIExport discovery.
	result, changed := i.repairPortalAccess(ctx, pr)
	if !result.IsContinue() || changed {
		return result, changed, nil
	}

	for _, w := range restoreManagedWorkloads {
		ready, err := w.ready(ctx, i.client)

		if err != nil {
			return subroutines.OK(), false, err
		}
		if !ready {
			return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("waiting for identity workload %s/%s/%s", w.Namespace, w.Kind, w.Name)), false, nil
		}
	}

	return subroutines.OK(), markRestoreSucceeded(pr), nil
}

// repairPortalAccess makes sure the destination KCP identity and its RBAC are
// usable by Portal before identity validation can complete. It is also safe for
// a terminal Succeeded restore, provided the caller only changes object metadata
// to request reconciliation.
func (i *IdentityValidationSubroutine) repairPortalAccess(ctx context.Context, pr *pmbackupv1alpha1.PlatformRestore) (subroutines.Result, bool) {
	recovery := NewPlatformRecoverySubroutine(i.client)
	claimsReady, err := recovery.ensureKCPAPIClaims(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("repair KCP front-proxy access: %v", err)), false
	}
	if !claimsReady {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for KCP front-proxy access to recover"), false
	}

	changed, err := recovery.ensureReBACBootstrapCredential(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("repair ReBAC bootstrap credential: %v", err)), false
	}
	if changed {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for ReBAC bootstrap credential"), false
	}
	changed, err = recovery.ensureExtensionManagerBootstrapCredential(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("repair extension-manager bootstrap credential: %v", err)), false
	}
	if changed {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for extension-manager bootstrap credential"), false
	}
	changed, err = recovery.ensurePortalBootstrapCredential(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("repair portal bootstrap credential: %v", err)), false
	}
	if changed {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for portal bootstrap credential"), false
	}

	// PlatformRecovery from an older operator may already have recorded its
	// terminal conditions while the temporary KCP webhook bootstrap window was
	// still open. Finish that state transition here as an idempotent repair so
	// an in-flight restore can recover without replaying destructive stages.
	bootstrapReady, err := recovery.ensureKCPWebhookBootstrapRestored(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("restore KCP webhook bootstrap: %v", err)), false
	}
	if !bootstrapReady {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for KCP webhook bootstrap restore"), false
	}
	authorizationReady, err := recovery.ensureKCPAuthorizationConfiguration(ctx)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("restore KCP authorization configuration: %v", err)), false
	}
	if !authorizationReady {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for kcp-operator to restore KCP authorization configuration"), false
	}
	frontProxyReady, err := recovery.ensureKCPFrontProxyReady(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("restart KCP front-proxy: %v", err)), false
	}
	if !frontProxyReady {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for KCP front-proxy cache refresh"), false
	}
	changed, err = recovery.ensureOrganizationAdmissionWebhookTrust(ctx)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("repair organization admission webhook trust: %v", err)), false
	}
	if changed {
		return subroutines.StopWithRequeue(5*time.Second, "waiting after organization admission webhook CA repair"), false
	}
	controllersReady, err := recovery.ensureOrganizationControllersReady(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("recover organization controllers: %v", err)), false
	}
	if !controllersReady {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for organization controllers to restart"), false
	}
	organizationsReady, err := recovery.restoredOrganizationResourcesReady(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("recover restored organizations: %v", err)), false
	}
	if !organizationsReady {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for restored accounts and identity providers to become ready"), false
	}
	if err := i.validateKCPDiscovery(ctx); err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false
	}

	return subroutines.OK(), false
}

func (i *IdentityValidationSubroutine) validateKCPDiscovery(ctx context.Context) error {
	var secret corev1.Secret
	if err := i.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      "kubeconfig-kcp-admin",
	}, &secret); err != nil {
		return fmt.Errorf("waiting for kubeconfig-kcp-admin secret: %w", err)
	}

	raw := secret.Data["kubeconfig"]
	if len(raw) == 0 {
		return fmt.Errorf("waiting for kubeconfig-kcp-admin secret to contain data.kubeconfig")
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return fmt.Errorf("failed to parse restored kcp admin kubeconfig: %w", err)
	}

	baseURL := os.Getenv("KCP_BASE_URL")
	if baseURL == "" {
		baseURL = "https://frontproxy-front-proxy.platform-mesh-system:8443"
	}

	cfg = rest.CopyConfig(cfg)
	cfg.Host = strings.TrimRight(baseURL, "/")
	cfg.APIPath = ""

	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return fmt.Errorf("failed to build kcp HTTP client: %w", err)
	}

	paths := []string{
		"/clusters/root/apis",
		"/clusters/root:orgs/apis",
		"/clusters/root:platform-mesh-system/apis",
		// These are the first two KCP reads made by Portal while it serves
		// /rest/envconfig and /rest/config. A healthy deployment alone is not
		// sufficient: restored RBAC can leave kcp-admin unable to make either
		// request, which otherwise produces a blank Portal after a "Succeeded"
		// restore.
		"/clusters/root:platform-mesh-system/apis/core.platform-mesh.io/v1alpha1/identityproviderconfigurations/welcome",
		"/clusters/root:orgs/api/v1/namespaces/default/secrets/portal-client-secret-welcome",
	}

	for _, p := range paths {
		if err := getKCPPath(ctx, httpClient, cfg.Host, p); err != nil {
			return err
		}
	}

	return nil
}

func getKCPPath(ctx context.Context, httpClient *http.Client, baseURL string, path string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}

	u.Path = path
	u.RawQuery = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("waiting for kcp discovery %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("waiting for kcp discovery %s: status=%d body=%s", path, resp.StatusCode, string(body))
}

// contentVirtualWorkspacePathResolved distinguishes an unresolved content
// virtual-workspace route from ordinary resource authorization. A 401/403 from
// the served API proves routing succeeded; the caller's identity simply lacks
// access to that resource. KCP returns a distinct message when it cannot map
// the requested logical workspace to a virtual workspace.
func contentVirtualWorkspacePathResolved(
	ctx context.Context,
	httpClient *http.Client,
	baseURL string,
	path string,
) (bool, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("parse content virtual-workspace base URL: %w", err)
	}
	u.Path = path
	u.RawQuery = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, fmt.Errorf("create content virtual-workspace request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request content virtual workspace %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return true, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return false, fmt.Errorf("read content virtual-workspace response: %w", err)
	}
	if strings.Contains(string(body), "Path not resolved to a valid virtual workspace") {
		return false, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return true, nil
	}
	return false, fmt.Errorf("content virtual workspace %s: status=%d body=%s", path, resp.StatusCode, string(body))
}
