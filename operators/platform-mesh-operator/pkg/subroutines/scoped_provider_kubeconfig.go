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

package subroutines

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcpapiv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpapiv1alpha2 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha2"
)

const (
	defaultScopedSANamespace       = "default"
	secondsPerDay                  = 86400
	defaultTokenExpirationSeconds  = 7 * secondsPerDay
	scopedClusterRolePrefix        = "platform-mesh-provider-"
	scopedSAPrefix                 = "platform-mesh-provider-"
	scopedWorkspaceAccessCRBPrefix = "platform-mesh-workspace-access-"
	kcpWorkspaceAccessRoleName     = "system:kcp:workspace:access"

	scopedKubeconfigSecretKey = "kubeconfig"
	scopedTokenSecretKey      = "token"
	// scopedTokenRenewalCheckInterval is how often the operator re-checks scoped tokens for renewal.
	scopedTokenRenewalCheckInterval = 6 * time.Hour
)

func resolveAPIExport(ctx context.Context, kcpHelper KcpHelper, cfg *rest.Config, apiExportName, apiExportPath string) (*kcpapiv1alpha2.APIExport, error) {
	if apiExportName == "" {
		return nil, fmt.Errorf("cannot resolve APIExport: APIExportName is required")
	}
	if apiExportPath == "" {
		return nil, fmt.Errorf("cannot resolve APIExport: workspace path is required")
	}

	kcpClient, err := kcpHelper.NewKcpClient(rest.CopyConfig(cfg), apiExportPath)
	if err != nil {
		return nil, err
	}

	var export kcpapiv1alpha2.APIExport
	if err := kcpClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: apiExportName}, &export); err != nil {
		return nil, fmt.Errorf("get APIExport %s in workspace %s: %w", apiExportName, apiExportPath, err)
	}
	return &export, nil
}

func getPolicyRulesFromAPIExport(export *kcpapiv1alpha2.APIExport) ([]rbacv1.PolicyRule, error) {
	var rules []rbacv1.PolicyRule

	for _, res := range export.Spec.Resources {
		group := res.Group
		resource := res.Name
		if resource == "" {
			continue
		}
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{group},
			Resources: []string{resource},
			Verbs:     []string{"*"},
		})
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{group},
			Resources: []string{resource + "/status"},
			Verbs:     []string{"get", "update", "patch"},
		})
	}

	for _, claim := range export.Spec.PermissionClaims {
		group := claim.Group
		resource := claim.Resource
		if resource == "" {
			continue
		}
		verbs := claim.Verbs
		if len(verbs) == 0 {
			verbs = []string{"*"}
		}
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{group},
			Resources: []string{resource},
			Verbs:     verbs,
		})
		if hasUpdatePatchVerbs(verbs) {
			rules = append(rules, rbacv1.PolicyRule{
				APIGroups: []string{group},
				Resources: []string{resource + "/status"},
				Verbs:     []string{"get", "update", "patch"},
			})
		}
	}

	if export.Name != "" {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups:     []string{"apis.kcp.io"},
			Resources:     []string{"apiexports/content"},
			ResourceNames: []string{export.Name},
			Verbs:         []string{"*"},
		})
	}

	rules = append(rules, rbacv1.PolicyRule{
		APIGroups: []string{"apis.kcp.io"},
		Resources: []string{"apiexportendpointslices"},
		Verbs:     []string{"get", "list", "watch"},
	})

	rules = append(rules, rbacv1.PolicyRule{
		APIGroups: []string{"apis.kcp.io"},
		Resources: []string{"apibindings"},
		Verbs:     []string{"get", "list", "watch"},
	})

	rules = append(rules, rbacv1.PolicyRule{
		NonResourceURLs: []string{
			"/api", "/api/*",
			"/apis", "/apis/*",
			"/clusters/*",
			"/services", "/services/*",
		},
		Verbs: []string{"get"},
	})

	return rules, nil
}

func hasUpdatePatchVerbs(verbs []string) bool {
	for _, v := range verbs {
		if v == "*" || v == "update" || v == "patch" {
			return true
		}
	}
	return false
}

// ScopedRBACSpec defines a single ClusterRole + ClusterRoleBinding to create.
type ScopedRBACSpec struct {
	// RoleSuffix is appended to the base ClusterRole name.
	// Empty string means use the base name without suffix.
	RoleSuffix string
	// Rules are the PolicyRules for this ClusterRole.
	Rules []rbacv1.PolicyRule
}

// ensureScopedProviderServiceAccountAndRBAC creates:
// - ServiceAccount named platform-mesh-provider-{providerSuffix}
// - ClusterRole per ScopedRBACSpec, named platform-mesh-provider-{providerSuffix}[-{spec.RoleSuffix}]
// - ClusterRoleBinding per ClusterRole, binding to the same ServiceAccount
// - Workspace access ClusterRoleBinding for the ServiceAccount
func ensureScopedProviderServiceAccountAndRBAC(
	ctx context.Context,
	kcpClient ctrlruntimeclient.Client,
	providerSuffix string,
	rbacSpecs []ScopedRBACSpec,
) (saName string, err error) {
	if providerSuffix == "" {
		return "", fmt.Errorf("provider suffix for scoped RBAC is empty")
	}
	if len(rbacSpecs) == 0 {
		return "", fmt.Errorf("rbacSpecs is empty")
	}

	saNamespace := defaultScopedSANamespace
	saName = scopedSAPrefix + providerSuffix
	workspaceAccessCRBName := scopedWorkspaceAccessCRBPrefix + providerSuffix

	if err := ensureScopedNamespaceExists(ctx, kcpClient, saNamespace); err != nil {
		return "", fmt.Errorf("ensure namespace %s for scoped ServiceAccount: %w", saNamespace, err)
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: saNamespace,
			Name:      saName,
		},
	}
	if err := kcpClient.Create(ctx, sa); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create ServiceAccount %s: %w", saName, err)
		}
	}

	for _, spec := range rbacSpecs {
		crName := scopedClusterRolePrefix + providerSuffix
		if spec.RoleSuffix != "" {
			crName = crName + "-" + spec.RoleSuffix
		}

		cr := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: crName},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, kcpClient, cr, func() error {
			cr.Rules = spec.Rules
			return nil
		}); err != nil {
			return "", fmt.Errorf("create or update ClusterRole %s: %w", crName, err)
		}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: crName},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, kcpClient, crb, func() error {
			crb.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     crName,
			}
			crb.Subjects = []rbacv1.Subject{
				{
					Kind:      rbacv1.ServiceAccountKind,
					Namespace: saNamespace,
					Name:      saName,
				},
			}
			return nil
		}); err != nil {
			return "", fmt.Errorf("create or update ClusterRoleBinding %s: %w", crName, err)
		}
	}

	workspaceAccessCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: workspaceAccessCRBName},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, kcpClient, workspaceAccessCRB, func() error {
		workspaceAccessCRB.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     kcpWorkspaceAccessRoleName,
		}
		workspaceAccessCRB.Subjects = []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: saNamespace,
				Name:      saName,
			},
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("create or update ClusterRoleBinding %s for workspace access: %w", workspaceAccessCRBName, err)
	}

	return saName, nil
}

func ensureScopedNamespaceExists(ctx context.Context, kcpClient ctrlruntimeclient.Client, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is empty")
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	if err := kcpClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createTokenForSA(ctx context.Context, kcpWorkspaceClient ctrlruntimeclient.Client, namespace, saName string, expirationSeconds int64) (string, error) {
	expSec := expirationSeconds
	if expSec <= 0 {
		expSec = defaultTokenExpirationSeconds
	}
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      saName,
		},
	}
	tr := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &expSec,
		},
	}
	if err := kcpWorkspaceClient.SubResource("token").Create(ctx, sa, tr); err != nil {
		return "", fmt.Errorf("create token for ServiceAccount %s/%s: %w", namespace, saName, err)
	}
	if tr.Status.Token == "" {
		return "", fmt.Errorf("empty token in TokenRequest status for ServiceAccount %s/%s", namespace, saName)
	}
	return tr.Status.Token, nil
}

// virtualWorkspaceServerURLFromSlice returns status.apiExportEndpoints[0].url as the kubeconfig cluster server (kcp’s published VirtualWorkspace URL).
func virtualWorkspaceServerURLFromSlice(slice *kcpapiv1alpha1.APIExportEndpointSlice) (string, error) {
	if slice == nil {
		return "", fmt.Errorf("nil APIExportEndpointSlice")
	}
	if len(slice.Status.APIExportEndpoints) == 0 {
		return "", fmt.Errorf("no endpoints in APIExportEndpointSlice %q", slice.Name)
	}
	raw := strings.TrimSpace(slice.Status.APIExportEndpoints[0].URL)
	if raw == "" {
		return "", fmt.Errorf("empty endpoint URL on APIExportEndpointSlice %q", slice.Name)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint URL on APIExportEndpointSlice %q: %w", slice.Name, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid endpoint URL on APIExportEndpointSlice %q: missing scheme or host", slice.Name)
	}
	if u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("invalid endpoint URL on APIExportEndpointSlice %q: missing path", slice.Name)
	}
	return strings.TrimSuffix(raw, "/"), nil
}

// virtualWorkspacePathFromSlice returns only the URL path from status (for joining to a different base host, e.g. admin kubeconfig front-proxy).
func virtualWorkspacePathFromSlice(slice *kcpapiv1alpha1.APIExportEndpointSlice) (string, error) {
	if slice == nil {
		return "", fmt.Errorf("nil APIExportEndpointSlice")
	}
	if len(slice.Status.APIExportEndpoints) == 0 {
		return "", fmt.Errorf("no endpoints in APIExportEndpointSlice %q", slice.Name)
	}
	raw := slice.Status.APIExportEndpoints[0].URL
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("invalid endpoint URL on APIExportEndpointSlice %q", slice.Name)
	}
	return strings.TrimSuffix(u.Path, "/"), nil
}

// apiExportLocationFromEndpointSlice returns spec.export name and path from the slice object (no trimming).
func apiExportLocationFromEndpointSlice(slice *kcpapiv1alpha1.APIExportEndpointSlice) (apiExportName, exportWorkspacePath string, err error) {
	if slice == nil {
		return "", "", fmt.Errorf("nil APIExportEndpointSlice")
	}
	n := slice.Name
	if slice.Spec.APIExport.Name == "" {
		return "", "", fmt.Errorf("APIExportEndpointSlice %q has empty spec.export.name", n)
	}
	if slice.Spec.APIExport.Path == "" {
		return "", "", fmt.Errorf("APIExportEndpointSlice %q has empty spec.export.path", n)
	}
	return slice.Spec.APIExport.Name, slice.Spec.APIExport.Path, nil
}

// resolveAPIExportVirtualWorkspaceRawPath returns the URL path segment for joining to front-proxy host (admin kubeconfig), from APIExportEndpointSlice status in sliceWorkspacePath (typically ProviderConnection.Path).
func resolveAPIExportVirtualWorkspaceRawPath(ctx context.Context, kcpHelper KcpHelper, baseCfg *rest.Config, sliceWorkspacePath, sliceName string) (string, error) {
	if sliceName == "" {
		return "", fmt.Errorf("APIExportEndpointSlice name is empty")
	}
	if strings.TrimSpace(sliceWorkspacePath) == "" {
		return "", fmt.Errorf("APIExportEndpointSlice workspace path is empty")
	}
	name := sliceName
	sliceClient, err := kcpHelper.NewKcpClient(rest.CopyConfig(baseCfg), sliceWorkspacePath)
	if err != nil {
		return "", fmt.Errorf("kcp client for APIExportEndpointSlice workspace: %w", err)
	}
	var endpointSlice kcpapiv1alpha1.APIExportEndpointSlice
	if err := sliceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, &endpointSlice); err != nil {
		return "", fmt.Errorf("get APIExportEndpointSlice %q in %s: %w", name, sliceWorkspacePath, err)
	}
	return virtualWorkspacePathFromSlice(&endpointSlice)
}

// parseScopedKubeconfigExportSource validates that exactly one of endpointSliceName or apiExportNames is set.
func parseScopedKubeconfigExportSource(pc pmcorev1alpha1.ProviderConnection) (endpointSliceName string, apiExportNames []string, err error) {
	endpointSliceName = ptr.Deref(pc.EndpointSliceName, "")
	apiExportNames = pc.APIExportNames

	if endpointSliceName != "" && len(apiExportNames) > 0 {
		return "", nil, fmt.Errorf("scoped kubeconfig: set only one of endpointSliceName or apiExportNames")
	}
	if endpointSliceName == "" && len(apiExportNames) == 0 {
		return "", nil, fmt.Errorf("scoped kubeconfig requires endpointSliceName or apiExportNames")
	}
	return endpointSliceName, apiExportNames, nil
}

func createScopedKubeconfigURLForAPIExportName(operatorCfg config.OperatorConfig, instance *pmcorev1alpha1.PlatformMesh, pcPath string, external bool) (string, error) {
	hostPort := fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	if external {
		hostPort = fmt.Sprintf("https://kcp.api.%s:%d", instance.Spec.Exposure.BaseDomain, instance.Spec.Exposure.Port)
	}
	hostURL, err := url.JoinPath(hostPort, "clusters", pcPath)
	if err != nil {
		return "", errors.Wrap(err, "build scoped workspace cluster server URL")
	}
	return hostURL, nil
}

// rewriteScopedVirtualWorkspaceURLToFrontProxy replaces the host from KCP's APIExportEndpointSlice status URL
// with the same base URL used for admin provider kubeconfigs (in-cluster front-proxy Service DNS, or exposure URL when pc.External),
// preserving path and raw query. This matches HandleProviderConnection's url.JoinPath(hostPort, address.Path) behavior.
func rewriteScopedVirtualWorkspaceURLToFrontProxy(hostURL string, operatorCfg config.OperatorConfig, instance *pmcorev1alpha1.PlatformMesh, pcExternal bool) (string, error) {
	u, err := url.Parse(hostURL)
	if err != nil {
		return "", fmt.Errorf("parse virtual workspace URL: %w", err)
	}
	if u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("virtual workspace URL %q has no path", hostURL)
	}
	hostPort := fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	if pcExternal {
		if instance.Spec.Exposure == nil {
			return "", fmt.Errorf("provider connection with external: true requires spec.exposure")
		}
		hostPort = fmt.Sprintf("https://kcp.api.%s:%d", instance.Spec.Exposure.BaseDomain, instance.Spec.Exposure.Port)
	}
	out, err := url.JoinPath(hostPort, u.Path)
	if err != nil {
		return "", err
	}
	out = strings.TrimSuffix(out, "/")
	if u.RawQuery != "" {
		return out + "?" + u.RawQuery, nil
	}
	return out, nil
}

// writeScopedKubeconfigToSecret builds a scoped kubeconfig: ServiceAccount token in pc.Path, RBAC from APIExports; server is virtual workspace when endpointSliceName is set, else workspace cluster URL when apiExportNames is set.
func writeScopedKubeconfigToSecret(
	ctx context.Context,
	k8sClient ctrlruntimeclient.Client,
	kcpHelper KcpHelper,
	cfg *rest.Config,
	instance *pmcorev1alpha1.PlatformMesh,
	pc pmcorev1alpha1.ProviderConnection,
) error {
	log := logger.LoadLoggerFromContext(ctx)
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	pcPath := strings.TrimSpace(pc.Path)
	if pcPath == "" {
		return fmt.Errorf("scoped kubeconfig requires Path (workspace)")
	}

	endpointSliceName, apiExportNames, err := parseScopedKubeconfigExportSource(pc)
	if err != nil {
		return err
	}

	kcpWorkspaceClient, err := kcpHelper.NewKcpClient(rest.CopyConfig(cfg), pcPath)
	if err != nil {
		return errors.Wrap(err, "kcp client for provider workspace")
	}

	var hostURL string
	var saName string

	caData := cfg.CAData
	if caData == nil {
		caData = []byte{}
	}
	caData = AppendRootShardCAPEMIfMissing(ctx, k8sClient, &operatorCfg, caData)

	if endpointSliceName != "" {
		var endpointSlice kcpapiv1alpha1.APIExportEndpointSlice
		if err := kcpWorkspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: endpointSliceName}, &endpointSlice); err != nil {
			return fmt.Errorf("get APIExportEndpointSlice %q in %s: %w", endpointSliceName, pcPath, err)
		}
		hostURL, err = virtualWorkspaceServerURLFromSlice(&endpointSlice)
		if err != nil {
			return err
		}
		sliceStatusURL := hostURL
		hostURL, err = rewriteScopedVirtualWorkspaceURLToFrontProxy(hostURL, operatorCfg, instance, pc.External)
		if err != nil {
			return errors.Wrap(err, "rewrite scoped virtual workspace URL to front-proxy base")
		}
		if hostURL != sliceStatusURL {
			log.Info().
				Str("secret", pc.Secret).
				Str("sliceStatusURL", sliceStatusURL).
				Str("serverURL", hostURL).
				Msg("Rewrote scoped virtual workspace server URL to in-cluster front-proxy base")
		}
		apiExportName, exportWorkspacePath, err := apiExportLocationFromEndpointSlice(&endpointSlice)
		if err != nil {
			return err
		}
		log.Info().
			Str("secret", pc.Secret).
			Str("path", pcPath).
			Str("endpointSlice", endpointSliceName).
			Str("apiExport", apiExportName).
			Str("hostURL", hostURL).
			Msg("Using scoped kubeconfig virtual workspace URL")

		export, err := resolveAPIExport(ctx, kcpHelper, cfg, apiExportName, exportWorkspacePath)
		if err != nil {
			return errors.Wrap(err, "resolve APIExport")
		}
		rules, err := getPolicyRulesFromAPIExport(export)
		if err != nil {
			return errors.Wrap(err, "build RBAC from APIExport")
		}

		saName, err = ensureScopedProviderServiceAccountAndRBAC(ctx, kcpWorkspaceClient, pc.Secret, []ScopedRBACSpec{
			{RoleSuffix: "", Rules: rules},
		})
		if err != nil {
			return errors.Wrap(err, "ensure ServiceAccount and RBAC")
		}
	} else {
		hostURL, err = createScopedKubeconfigURLForAPIExportName(operatorCfg, instance, pcPath, pc.External)
		if err != nil {
			return err
		}
		log.Info().
			Str("secret", pc.Secret).
			Str("path", pcPath).
			Strs("apiExports", apiExportNames).
			Str("hostURL", hostURL).
			Msg("Using scoped kubeconfig workspace cluster URL with multiple APIExports")

		var rbacSpecs []ScopedRBACSpec
		for _, exportName := range apiExportNames {
			export, err := resolveAPIExport(ctx, kcpHelper, cfg, exportName, pcPath)
			if err != nil {
				return fmt.Errorf("resolve APIExport %s: %w", exportName, err)
			}
			rules, err := getPolicyRulesFromAPIExport(export)
			if err != nil {
				return fmt.Errorf("get policy rules from APIExport %s: %w", exportName, err)
			}
			sanitizedExport := strings.ReplaceAll(exportName, ".", "-")
			rbacSpecs = append(rbacSpecs, ScopedRBACSpec{
				RoleSuffix: sanitizedExport,
				Rules:      rules,
			})
		}

		saName, err = ensureScopedProviderServiceAccountAndRBAC(ctx, kcpWorkspaceClient, pc.Secret, rbacSpecs)
		if err != nil {
			return errors.Wrap(err, "ensure ServiceAccount and RBAC")
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: pc.Secret, Namespace: ptr.Deref(pc.Namespace, operatorCfg.KCP.Namespace)},
	}
	if err := k8sClient.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(secret), secret); err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "get provider secret")
	}
	sa := &corev1.ServiceAccount{}
	if err := kcpWorkspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: defaultScopedSANamespace, Name: saName}, sa); err != nil {
		return errors.Wrap(err, "get ServiceAccount")
	}

	// Keep the stored token until half of its lifetime has passed, so the secret is not rewritten on every reconcile.
	token := string(secret.Data[scopedTokenSecretKey])
	if scopedTokenNeedsRenewal(token, string(sa.UID), time.Now()) {
		token, err = createTokenForSA(ctx, kcpWorkspaceClient, defaultScopedSANamespace, saName, defaultTokenExpirationSeconds)
		if err != nil {
			return errors.Wrap(err, "create token for ServiceAccount")
		}
		log.Info().Str("secret", pc.Secret).Msg("Issued new scoped kubeconfig token")
	}
	kubeconfig := buildScopedKubeconfig(hostURL, token, caData)
	kubeconfigBytes, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return errors.Wrap(err, "write kubeconfig")
	}

	_, err = controllerutil.CreateOrUpdate(ctx, k8sClient, secret, func() error {
		secret.Data = map[string][]byte{
			scopedKubeconfigSecretKey: kubeconfigBytes,
			scopedTokenSecretKey:      []byte(token),
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "write provider secret")
	}
	return nil
}

// scopedTokenNeedsRenewal reports whether the token is unreadable, bound to another ServiceAccount UID, or past half of its lifetime.
func scopedTokenNeedsRenewal(token, saUID string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return true
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}
	var claims struct {
		IssuedAt   int64 `json:"iat"`
		Expiry     int64 `json:"exp"`
		Kubernetes struct {
			ServiceAccount struct {
				UID string `json:"uid"`
			} `json:"serviceaccount"`
		} `json:"kubernetes.io"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return true
	}
	if claims.Expiry <= claims.IssuedAt || claims.Kubernetes.ServiceAccount.UID != saUID {
		return true
	}
	halfLife := claims.IssuedAt + (claims.Expiry-claims.IssuedAt)/2
	return now.Unix() >= halfLife
}

// buildScopedKubeconfig references the token file next to the kubeconfig so client-go reloads a renewed token; the inline token is the fallback.
func buildScopedKubeconfig(hostURL string, token string, caData []byte) *clientcmdapi.Config {
	return &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"default-cluster": {
				Server:                   hostURL,
				CertificateAuthorityData: caData,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default-auth": {
				Token:     token,
				TokenFile: scopedTokenSecretKey,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default-context": {
				Cluster:  "default-cluster",
				AuthInfo: "default-auth",
			},
		},
		CurrentContext: "default-context",
	}
}
