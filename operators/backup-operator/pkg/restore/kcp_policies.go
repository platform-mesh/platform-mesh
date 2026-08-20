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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const kcpPolicyRestoreKeyPrefix = "platform-mesh/kcp-apiexport-policies"

const kcpWorkspaceRestoreKeyPrefix = "platform-mesh/kcp-workspaces"

// Root-scoped bootstrap operations use the root KCP Service. Non-root
// workspaces use their assigned shard and physical logical-cluster ID.
const (
	kcpRootSystemBaseURLDefault   = "https://root-kcp.platform-mesh-system.svc.cluster.local:6443"
	kcpNereusSystemBaseURLDefault = "https://nereus-shard-kcp.platform-mesh-system.svc.cluster.local:6443"
	kcpTritonSystemBaseURLDefault = "https://triton-shard-kcp.platform-mesh-system.svc.cluster.local:6443"
)

var (
	kcpPolicyGVR           = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "apiexportpolicies"}
	kcpWorkspaceRestoreGVR = schema.GroupVersionResource{Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces"}
	apiBindingGVR          = schema.GroupVersionResource{Group: "apis.kcp.io", Version: "v1alpha2", Resource: "apibindings"}
	apiExportGVR           = schema.GroupVersionResource{Group: "apis.kcp.io", Version: "v1alpha2", Resource: "apiexports"}
	apiResourceSchemaGVR   = schema.GroupVersionResource{Group: "apis.kcp.io", Version: "v1alpha1", Resource: "apiresourceschemas"}
)

var (
	kcpClusterRoleGVR        = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
	kcpClusterRoleBindingGVR = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}
)

const (
	kcpPolicyBootstrapRoleName    = "platform-restore-api-export-policy-bootstrap"
	kcpPolicyBootstrapBindingName = "platform-restore-api-export-policy-bootstrap"

	coreAPIBindingName                   = "core.platform-mesh.io"
	systemAPIBindingName                 = "system.platform-mesh.io"
	identityProviderResourceGroup        = "core.platform-mesh.io"
	identityProviderResourceName         = "identityproviderconfigurations"
	identityBindingStatusResetAnnotation = "restore.platform-mesh.io/apibinding-status-reset-v2-for"
	staleCoreBindingResetAnnotation      = "restore.platform-mesh.io/apibinding-status-reset-v1-for"
)

type kcpPolicyRestoreManifest map[string][]map[string]any

func (p *PlatformRecoverySubroutine) restoreKCPPolicies(ctx context.Context, pr *pmbackupv1alpha1.PlatformRestore) (bool, error) {
	cm, err := ensureRestoreStateConfigMap(ctx, p.client, pr)
	if err != nil {
		return false, err
	}
	const stateKey = "kcpAPIExportPoliciesRestoredFor"
	if cm.Data[stateKey] == string(pr.UID) {
		return false, nil
	}
	store, err := (&OpenFGADumpRestoreSubroutine{client: p.client}).s3(ctx, pr.Spec.Source.Storage)
	if err != nil {
		return false, err
	}
	key := fmt.Sprintf("%s/%s.json", kcpPolicyRestoreKeyPrefix, pr.Spec.Source.BackupID)
	object, err := store.GetObject(ctx, cnpgBackupBucket, key, minio.GetObjectOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = object.Close() }()
	raw, err := io.ReadAll(object)
	if err != nil {
		return false, err
	}
	var manifest kcpPolicyRestoreManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return false, fmt.Errorf("decode KCP APIExportPolicy manifest: %w", err)
	}
	bootstrapClients := make(map[string]dynamic.Interface, len(manifest))
	for path, policies := range manifest {
		// APIExportPolicies restore the authorization policy itself. They are
		// restored while the authorization webhook is temporarily disabled, using
		// the root recovery credential rather than kcp-admin.
		client, err := p.kcpRecoveryDynamicClient(ctx, path)
		if err != nil {
			return false, err
		}
		changed, err := ensureKCPPolicyBootstrapAccess(ctx, client)
		if err != nil {
			return false, fmt.Errorf("grant temporary APIExportPolicy bootstrap access in %s: %w", path, err)
		}
		if changed {
			return true, nil
		}
		bootstrapClients[path] = client
		for _, object := range policies {
			policy := &unstructured.Unstructured{Object: object}
			if policy.GetName() == "" {
				return false, fmt.Errorf("APIExportPolicy manifest entry in %s has no metadata.name", path)
			}
			policy.SetResourceVersion("")
			_, err = client.Resource(kcpPolicyGVR).Create(ctx, policy, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				current, getErr := client.Resource(kcpPolicyGVR).Get(ctx, policy.GetName(), metav1.GetOptions{})
				if getErr != nil {
					return false, getErr
				}
				policy.SetResourceVersion(current.GetResourceVersion())
				_, err = client.Resource(kcpPolicyGVR).Update(ctx, policy, metav1.UpdateOptions{})
			}
			if err != nil {
				return false, fmt.Errorf("apply APIExportPolicy %s in %s: %w", policy.GetName(), path, err)
			}
		}
	}
	for path, client := range bootstrapClients {
		if err := removeKCPPolicyBootstrapAccess(ctx, client); err != nil {
			return false, fmt.Errorf("remove temporary APIExportPolicy bootstrap access in %s: %w", path, err)
		}
	}
	base := cm.DeepCopy()
	cm.Data[stateKey] = string(pr.UID)
	if err := p.client.Patch(ctx, cm, ctrlruntimeclient.MergeFrom(base)); err != nil {
		return false, err
	}
	return true, nil
}

// kcpRecoveryDynamicClient uses the physical logical-cluster ID assigned by KCP
// for non-root workspaces. A shard-local KCP Service does not resolve the
// human path (root:orgs), but it does serve its assigned cluster ID. This also
// preserves the system:masters identity of the root recovery credential.
func (p *PlatformRecoverySubroutine) kcpRecoveryDynamicClient(ctx context.Context, logicalPath string) (dynamic.Interface, error) {
	config, err := p.kcpRecoveryRESTConfig(ctx, logicalPath)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(config)
}

// kcpRecoveryRESTConfig returns a destination-platform credential and endpoint
// for a logical workspace. It deliberately does not use restored kubeconfigs:
// those can contain source-platform client certificates after Velero restore.
func (p *PlatformRecoverySubroutine) kcpRecoveryRESTConfig(ctx context.Context, logicalPath string) (*rest.Config, error) {
	if logicalPath == "root" {
		return p.kcpSystemRESTConfig(ctx, logicalPath)
	}
	workspaceName := strings.TrimPrefix(logicalPath, "root:")
	if workspaceName == logicalPath || workspaceName == "" || strings.Contains(workspaceName, ":") {
		return nil, fmt.Errorf("unsupported KCP recovery logical path %q", logicalPath)
	}
	rootClient, err := p.kcpSystemDynamicClient(ctx, "root")
	if err != nil {
		return nil, err
	}
	workspace, err := rootClient.Resource(kcpWorkspaceRestoreGVR).Get(ctx, workspaceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get KCP Workspace %s: %w", workspaceName, err)
	}
	clusterID, found, err := unstructured.NestedString(workspace.Object, "spec", "cluster")
	if err != nil || !found || clusterID == "" {
		return nil, fmt.Errorf("KCP Workspace %s has no assigned cluster ID", workspaceName)
	}
	var clientSecret, caSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: "root-client"}, &clientSecret); err != nil {
		return nil, fmt.Errorf("get root KCP client credential: %w", err)
	}
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: "root-server-ca"}, &caSecret); err != nil {
		return nil, fmt.Errorf("get root KCP server CA: %w", err)
	}
	cert, key, ca := clientSecret.Data["tls.crt"], clientSecret.Data["tls.key"], caSecret.Data["tls.crt"]
	if len(cert) == 0 || len(key) == 0 || len(ca) == 0 {
		return nil, fmt.Errorf("root KCP client credential or server CA is incomplete")
	}
	baseURL, err := kcpWorkspaceShardBaseURL(workspace)
	if err != nil {
		return nil, err
	}
	return &rest.Config{
		Host: strings.TrimRight(baseURL, "/") + "/clusters/" + clusterID,
		TLSClientConfig: rest.TLSClientConfig{
			CertData: cert,
			KeyData:  key,
			CAData:   ca,
		},
		Timeout: 30 * time.Second,
	}, nil
}

func kcpWorkspaceShardBaseURL(workspace *unstructured.Unstructured) (string, error) {
	switch workspace.GetAnnotations()["core.kcp.io/shard"] {
	case "", "root":
		return kcpRootSystemBaseURLDefault, nil
	case "nereus":
		return kcpNereusSystemBaseURLDefault, nil
	case "triton":
		return kcpTritonSystemBaseURLDefault, nil
	default:
		return "", fmt.Errorf("KCP Workspace %s is assigned to unsupported shard %q", workspace.GetName(), workspace.GetAnnotations()["core.kcp.io/shard"])
	}
}

func ensureKCPPolicyBootstrapAccess(ctx context.Context, client dynamic.Interface) (bool, error) {
	role := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": kcpPolicyBootstrapRoleName},
		"rules": []any{map[string]any{
			"apiGroups": []any{"core.platform-mesh.io"},
			"resources": []any{"apiexportpolicies"},
			"verbs":     []any{"get", "list", "watch", "create", "update"},
		}},
	}}
	binding := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": kcpPolicyBootstrapBindingName},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     kcpPolicyBootstrapRoleName,
		},
		"subjects": []any{map[string]any{
			"kind":     "User",
			"name":     "root",
			"apiGroup": "rbac.authorization.k8s.io",
		}},
	}}
	changed := false
	if _, err := client.Resource(kcpClusterRoleGVR).Create(ctx, role, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, err
		}
	} else {
		changed = true
	}
	if _, err := client.Resource(kcpClusterRoleBindingGVR).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, err
		}
	} else {
		changed = true
	}
	return changed, nil
}

func removeKCPPolicyBootstrapAccess(ctx context.Context, client dynamic.Interface) error {
	if err := client.Resource(kcpClusterRoleBindingGVR).Delete(ctx, kcpPolicyBootstrapBindingName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := client.Resource(kcpClusterRoleGVR).Delete(ctx, kcpPolicyBootstrapRoleName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (p *PlatformRecoverySubroutine) kcpSystemDynamicClient(ctx context.Context, logicalPath string) (dynamic.Interface, error) {
	config, err := p.kcpSystemRESTConfig(ctx, logicalPath)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(config)
}

func (p *PlatformRecoverySubroutine) kcpSystemRESTConfig(ctx context.Context, logicalPath string) (*rest.Config, error) {
	var clientSecret, caSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: "root-client"}, &clientSecret); err != nil {
		return nil, fmt.Errorf("get root KCP client credential: %w", err)
	}
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: "root-server-ca"}, &caSecret); err != nil {
		return nil, fmt.Errorf("get root KCP server CA: %w", err)
	}
	cert, key, ca := clientSecret.Data["tls.crt"], clientSecret.Data["tls.key"], caSecret.Data["tls.crt"]
	if len(cert) == 0 || len(key) == 0 || len(ca) == 0 {
		return nil, fmt.Errorf("root KCP client credential or server CA is incomplete")
	}
	baseURL := kcpSystemBaseURL(logicalPath)
	return &rest.Config{
		Host: strings.TrimRight(baseURL, "/") + "/clusters/" + logicalPath,
		TLSClientConfig: rest.TLSClientConfig{
			CertData: cert,
			KeyData:  key,
			CAData:   ca,
		},
		Timeout: 30 * time.Second,
	}, nil
}

func kcpSystemBaseURL(logicalPath string) string {
	if baseURL := os.Getenv("KCP_SYSTEM_BASE_URL"); baseURL != "" {
		return baseURL
	}
	if logicalPath == orgsLogicalClusterPath {
		return kcpNereusSystemBaseURLDefault
	}
	return kcpRootSystemBaseURLDefault
}

func (p *PlatformRecoverySubroutine) restoreKCPWorkspaces(ctx context.Context, pr *pmbackupv1alpha1.PlatformRestore) (bool, error) {
	cm, err := ensureRestoreStateConfigMap(ctx, p.client, pr)
	if err != nil {
		return false, err
	}
	const stateKey = "kcpWorkspacesRestoredFor"
	if cm.Data[stateKey] == string(pr.UID) {
		return false, nil
	}
	store, err := (&OpenFGADumpRestoreSubroutine{client: p.client}).s3(ctx, pr.Spec.Source.Storage)
	if err != nil {
		return false, err
	}
	key := fmt.Sprintf("%s/%s.json", kcpWorkspaceRestoreKeyPrefix, pr.Spec.Source.BackupID)
	object, err := store.GetObject(ctx, cnpgBackupBucket, key, minio.GetObjectOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = object.Close() }()
	raw, err := io.ReadAll(object)
	if err != nil {
		return false, err
	}
	workspace := &unstructured.Unstructured{}
	if err := json.Unmarshal(raw, &workspace.Object); err != nil {
		return false, fmt.Errorf("decode KCP Workspace manifest: %w", err)
	}
	if workspace.GetName() != "orgs" {
		return false, fmt.Errorf("KCP Workspace manifest must be named orgs, got %q", workspace.GetName())
	}
	client, err := p.kcpSystemDynamicClient(ctx, "root")
	if err != nil {
		return false, err
	}
	_, err = client.Resource(kcpWorkspaceRestoreGVR).Create(ctx, workspace, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return false, fmt.Errorf("create KCP Workspace orgs: %w", err)
	}
	// A Workspace is a root-scoped resource. Checking an API through the new
	// workspace's URL is circular: the request cannot be routed until the
	// workspace is available. Wait for root-side scheduling and initialization.
	workspace, err = client.Resource(kcpWorkspaceRestoreGVR).Get(ctx, "orgs", metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get KCP Workspace orgs: %w", err)
	}
	if !kcpWorkspaceReady(workspace) {
		return false, fmt.Errorf("wait for KCP Workspace root:orgs to become ready")
	}
	base := cm.DeepCopy()
	cm.Data[stateKey] = string(pr.UID)
	if err := p.client.Patch(ctx, cm, ctrlruntimeclient.MergeFrom(base)); err != nil {
		return false, err
	}
	return true, nil
}

func kcpWorkspaceReady(workspace *unstructured.Unstructured) bool {
	if workspace.GetLabels()["tenancy.kcp.io/phase"] == "Ready" {
		return true
	}
	conditions, found, err := unstructured.NestedSlice(workspace.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	initialized, scheduled := false, false
	for _, condition := range conditions {
		entry, ok := condition.(map[string]any)
		if !ok || entry["status"] != "True" {
			continue
		}
		switch entry["type"] {
		case "WorkspaceInitialized":
			initialized = true
		case "WorkspaceScheduled":
			scheduled = true
		}
	}
	return initialized && scheduled
}

type kcpClaimsRecoveryProgress struct {
	recoveryWait
	changed bool
}

// recoverKCPClaims restores the KCP objects that make the core API export
// usable again. It deliberately preserves the old serial ordering: workspaces,
// policies, then APIBinding claims.
func (p *PlatformRecoverySubroutine) recoverKCPClaims(
	ctx context.Context,
	pr *pmbackupv1alpha1.PlatformRestore,
) (kcpClaimsRecoveryProgress, error) {
	changed, err := p.restoreKCPWorkspaces(ctx, pr)
	if err != nil {
		return kcpClaimsRecoveryProgress{}, fmt.Errorf("restore KCP Workspaces: %w", err)
	}
	if changed {
		return kcpClaimsRecoveryProgress{
			recoveryWait: recoveryWait{5 * time.Second, "waiting for restored KCP Workspaces"},
		}, nil
	}

	changed, err = p.restoreKCPPolicies(ctx, pr)
	if err != nil {
		return kcpClaimsRecoveryProgress{}, fmt.Errorf("restore KCP APIExportPolicies: %w", err)
	}
	if changed {
		return kcpClaimsRecoveryProgress{
			recoveryWait: recoveryWait{5 * time.Second, "waiting for restored KCP APIExportPolicies"},
		}, nil
	}

	claimsReady, err := p.ensureKCPAPIClaims(ctx, pr)
	if err != nil {
		return kcpClaimsRecoveryProgress{}, err
	}
	if !claimsReady {
		return kcpClaimsRecoveryProgress{
			recoveryWait: recoveryWait{10 * time.Second, "waiting for KCP APIBinding permission claims to recover"},
		}, nil
	}

	return kcpClaimsRecoveryProgress{
		changed: markCondition(
			pr,
			conditionKCPVirtualWorkspaceClaimsRecovered,
			"KCPVirtualWorkspaceClaimsRecovered",
			"front-proxy readers and the system.platform-mesh.io identity APIBinding are recovered",
		),
	}, nil
}
