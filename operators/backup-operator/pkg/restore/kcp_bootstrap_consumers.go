package restore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type tokenRecipe struct {
	SecretName      string
	LogicalPath     string
	ServiceAccount  string
	Workload        workloadRef
	FallbackToAdmin bool
}

var applicationTokenRecipes = []tokenRecipe{
	{
		SecretName:     "iam-service-kubeconfig",
		LogicalPath:    "root:platform-mesh-system",
		ServiceAccount: "platform-mesh-provider-iam-service-kubeconfig",
		Workload:       platformDeployment("iam-service"),
	},
	{
		SecretName: "rebac-authz-webhook-kubeconfig",
		// The ReBAC path-aware provider watches the core APIExport endpoint
		// slice in the provider workspace. Its application ServiceAccount is
		// created there as well.
		LogicalPath:     platformSystemPath,
		ServiceAccount:  "platform-mesh-provider-rebac-authz-webhook-kubeconfig",
		Workload:        platformDeployment("rebac-authz-webhook"),
		FallbackToAdmin: true,
	},
}

type applicationWorkloadCheckError struct{ err error }

func (e applicationWorkloadCheckError) Error() string { return e.err.Error() }
func (e applicationWorkloadCheckError) Unwrap() error { return e.err }

// repairBootstrapConsumers replaces source-cluster credentials and stale VWS
// schema routing before those consumers are allowed to initialize caches.
func (p *PlatformRecoverySubroutine) repairBootstrapConsumers(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	steps := []struct {
		repair func() (bool, error)
		error  string
		wait   string
	}{
		{func() (bool, error) { return p.ensureReBACBootstrapCredential(ctx, pr) }, "repair ReBAC bootstrap credential", "waiting for ReBAC bootstrap credential"},
		{func() (bool, error) { return p.ensureVirtualWorkspacesBootstrapCredential(ctx, pr) }, "repair virtual-workspaces bootstrap credential", "waiting for virtual-workspaces bootstrap credential"},
		{func() (bool, error) { return p.ensureVirtualWorkspacesSchemaServer(ctx) }, "repair virtual-workspaces schema server", "waiting for virtual-workspaces schema server update"},
		{func() (bool, error) { return p.ensureExtensionManagerBootstrapCredential(ctx, pr) }, "repair extension-manager bootstrap credential", "waiting for extension-manager bootstrap credential"},
		{func() (bool, error) { return p.ensurePortalBootstrapCredential(ctx, pr) }, "repair portal bootstrap credential", "waiting for portal bootstrap credential"},
	}
	for _, step := range steps {
		changed, err := step.repair()
		if err != nil {
			return recoveryWait{}, fmt.Errorf("%s: %w", step.error, err)
		}
		if changed {
			return recoveryWait{5 * time.Second, step.wait}, nil
		}
	}
	return recoveryWait{}, nil
}

// recoverApplicationTokens restores bootstrap access before refreshing tokens,
// then waits for every token consumer to use its destination credential.
func (p *PlatformRecoverySubroutine) recoverApplicationTokens(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	changed, err := p.ensureKCPApplicationTokenBootstrapAccess(ctx)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("grant application token bootstrap access: %w", err)
	}
	if changed {
		return recoveryWait{5 * time.Second, "waiting for application token bootstrap access"}, nil
	}
	changed, err = p.ensureReBACEndpointDiscoveryAccess(ctx)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("grant ReBAC endpoint discovery access: %w", err)
	}
	if changed {
		return recoveryWait{5 * time.Second, "waiting for ReBAC endpoint discovery access"}, nil
	}
	changed, err = p.ensureApplicationTokens(ctx, pr)
	if err != nil {
		return recoveryWait{}, err
	}
	if changed {
		return recoveryWait{10 * time.Second, "waiting for application workloads after token refresh"}, nil
	}

	for _, recipe := range applicationTokenRecipes {
		ready, err := recipe.Workload.ready(ctx, p.client)
		if err != nil {
			return recoveryWait{}, applicationWorkloadCheckError{err}
		}
		if !ready {
			return recoveryWait{10 * time.Second, fmt.Sprintf("waiting for Deployment/%s/%s", recipe.Workload.Namespace, recipe.Workload.Name)}, nil
		}
	}

	virtualWorkspaces := platformDeployment("virtual-workspaces")
	ready, err := virtualWorkspaces.ready(ctx, p.client)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("check virtual-workspaces bootstrap: %w", err)
	}
	if !ready {
		return recoveryWait{10 * time.Second, "waiting for virtual-workspaces bootstrap before restoring KCP authorization webhook"}, nil
	}
	return recoveryWait{}, nil
}

// kcpBootstrapCredentialRequest describes a restored kubeconfig that must be
// rewritten with a credential trusted by the destination KCP instance.
type kcpBootstrapCredentialRequest struct {
	secretName           string
	logicalPath          string
	workload             workloadRef
	credentialAnnotation string
	restartAnnotation    string
	component            string
	serverOverride       string
}

// ensureReBACBootstrapCredential gives ReBAC a fresh, locally trusted KCP
// client configuration before ReBAC itself becomes part of KCP authorization.
// This breaks the restore-time dependency cycle: KCP needs ReBAC for normal
// authorization, while ReBAC needs KCP API discovery to start.
func (p *PlatformRecoverySubroutine) ensureReBACBootstrapCredential(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	recipe := applicationTokenRecipes[1] // rebac-authz-webhook-kubeconfig
	return p.ensureKCPBootstrapCredentialWithServer(
		ctx,
		pr,
		recipe.SecretName,
		recipe.LogicalPath,
		recipe.Workload,
		rebacBootstrapCredentialAnnotation,
		rebacCredentialRestartAnnotation,
		"ReBAC",
		kcpReBACBootstrapURLDefault,
	)
}

func (p *PlatformRecoverySubroutine) ensureVirtualWorkspacesBootstrapCredential(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	return p.ensureKCPBootstrapCredential(
		ctx,
		pr,
		"account-operator-kubeconfig",
		virtualWorkspaceSchemaPath,
		platformDeployment("virtual-workspaces"),
		virtualWorkspacesCredentialAnnotation,
		virtualWorkspacesRestartAnnotation,
		"virtual-workspaces",
	)
}

func (p *PlatformRecoverySubroutine) ensureExtensionManagerBootstrapCredential(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	return p.ensureKCPBootstrapCredential(
		ctx,
		pr,
		"extension-manager-operator-kubeconfig",
		platformSystemPath,
		platformDeployment("extension-manager-operator-operator"),
		extensionManagerCredentialAnnotation,
		extensionManagerRestartAnnotation,
		"extension-manager-operator",
	)
}

func (p *PlatformRecoverySubroutine) ensureKCPBootstrapCredential(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	secretName, logicalPath string,
	workload workloadRef,
	credentialAnnotation, restartAnnotation, component string,
) (bool, error) {
	return p.ensureKCPBootstrapCredentialRequest(ctx, pr, kcpBootstrapCredentialRequest{
		secretName:           secretName,
		logicalPath:          logicalPath,
		workload:             workload,
		credentialAnnotation: credentialAnnotation,
		restartAnnotation:    restartAnnotation,
		component:            component,
	})
}

// ensureKCPBootstrapCredentialWithServer is the ReBAC-specific form that
// preserves logical root-proxy routing for descendant workspace resolution.
func (p *PlatformRecoverySubroutine) ensureKCPBootstrapCredentialWithServer(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	secretName, logicalPath string,
	workload workloadRef,
	credentialAnnotation, restartAnnotation, component, serverOverride string,
) (bool, error) {
	return p.ensureKCPBootstrapCredentialRequest(ctx, pr, kcpBootstrapCredentialRequest{
		secretName:           secretName,
		logicalPath:          logicalPath,
		workload:             workload,
		credentialAnnotation: credentialAnnotation,
		restartAnnotation:    restartAnnotation,
		component:            component,
		serverOverride:       serverOverride,
	})
}

func (p *PlatformRecoverySubroutine) ensureKCPBootstrapCredentialRequest(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	request kcpBootstrapCredentialRequest,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)
	var targetSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: request.secretName}, &targetSecret); err != nil {
		return false, fmt.Errorf("get %s kubeconfig Secret: %w", request.component, err)
	}
	if targetSecret.Annotations != nil && targetSecret.Annotations[request.credentialAnnotation] == string(pr.UID) {
		return false, nil
	}

	// Destination root-client credentials are trusted by the destination KCP;
	// source kubeconfig-kcp-admin credentials restored by Velero are not.
	recoveryConfig, err := p.kcpRecoveryRESTConfig(ctx, request.logicalPath)
	if err != nil {
		return false, fmt.Errorf("build destination KCP bootstrap credential: %w", err)
	}
	targetConfig, err := clientcmd.Load(targetSecret.Data["kubeconfig"])
	if err != nil {
		return false, fmt.Errorf("parse %s kubeconfig: %w", request.component, err)
	}
	if len(targetConfig.Clusters) == 0 || len(targetConfig.AuthInfos) == 0 {
		return false, fmt.Errorf("%s kubeconfig has no clusters or credentials", request.component)
	}
	for _, targetCluster := range targetConfig.Clusters {
		targetCluster.Server = recoveryConfig.Host
		if request.serverOverride != "" {
			targetCluster.Server = request.serverOverride
		}
		targetCluster.CertificateAuthority = ""
		targetCluster.CertificateAuthorityData = recoveryConfig.CAData
		targetCluster.InsecureSkipTLSVerify = false
	}
	for _, targetAuth := range targetConfig.AuthInfos {
		targetAuth.Token = ""
		targetAuth.Exec = nil
		targetAuth.AuthProvider = nil
		targetAuth.ClientCertificate = ""
		targetAuth.ClientKey = ""
		targetAuth.ClientCertificateData = recoveryConfig.CertData
		targetAuth.ClientKeyData = recoveryConfig.KeyData
	}
	raw, err := clientcmd.Write(*targetConfig)
	if err != nil {
		return false, fmt.Errorf("write %s bootstrap kubeconfig: %w", request.component, err)
	}
	patchBase := targetSecret.DeepCopy()
	targetSecret.Data["kubeconfig"] = raw
	if targetSecret.Annotations == nil {
		targetSecret.Annotations = map[string]string{}
	}
	targetSecret.Annotations[request.credentialAnnotation] = string(pr.UID)
	if err := p.client.Patch(ctx, &targetSecret, ctrlruntimeclient.MergeFrom(patchBase)); err != nil {
		return false, fmt.Errorf("patch %s bootstrap kubeconfig: %w", request.component, err)
	}
	if request.workload.Name != "" {
		if _, err := restartDeployment(ctx, p.client, request.workload, request.restartAnnotation, string(pr.UID)); err != nil {
			return false, fmt.Errorf("restart %s after bootstrap credential repair: %w", request.component, err)
		}
	}
	log.Info().Str("subroutine", platformRecoverySubroutineName).Str("platformRestore", pr.Name).Str("component", request.component).Str("secret", request.secretName).Str("logicalPath", request.logicalPath).Msg("repaired kubeconfig with destination KCP credential for bootstrap")
	return true, nil
}

// ensureVirtualWorkspacesSchemaServer keeps bootstrap schema reads on root KCP
// while virtual-workspaces rebuilds definitions after a restore.
func (p *PlatformRecoverySubroutine) ensureVirtualWorkspacesSchemaServer(ctx context.Context) (bool, error) {
	return p.ensureVirtualWorkspacesArgument(
		ctx,
		"--server-url=",
		"--server-url="+kcpRootSystemBaseURLDefault,
		"--server-url=",
	)
}

func (p *PlatformRecoverySubroutine) ensureVirtualWorkspaceSchemaWorkspace(ctx context.Context) (bool, error) {
	return p.ensureVirtualWorkspacesArgument(
		ctx,
		"--resource-schema-workspace=",
		"--resource-schema-workspace="+virtualWorkspaceSchemaPath,
		"--resource-schema-workspace",
	)
}

func (p *PlatformRecoverySubroutine) ensureVirtualWorkspacesArgument(ctx context.Context, prefix, expected, argumentName string) (bool, error) {
	var deployment appsv1.Deployment
	workload := platformDeployment("virtual-workspaces")
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: workload.Namespace, Name: workload.Name}, &deployment); err != nil {
		return false, fmt.Errorf("get virtual-workspaces Deployment: %w", err)
	}
	for containerIndex := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[containerIndex]
		for argumentIndex, argument := range container.Args {
			if !strings.HasPrefix(argument, prefix) {
				continue
			}
			if argument == expected {
				return false, nil
			}
			base := deployment.DeepCopy()
			container.Args[argumentIndex] = expected
			if err := p.client.Patch(ctx, &deployment, ctrlruntimeclient.MergeFrom(base)); err != nil {
				return false, fmt.Errorf("update virtual-workspaces %s: %w", strings.TrimSuffix(prefix, "="), err)
			}
			return true, nil
		}
	}
	return false, fmt.Errorf("virtual-workspaces Deployment has no %s argument", argumentName)
}

// ensurePortalBootstrapCredential preserves Portal's provider-scoped content
// virtual-workspace kubeconfig and destination CA trust.
func (p *PlatformRecoverySubroutine) ensurePortalBootstrapCredential(ctx context.Context, pr *v1alpha1.PlatformRestore) (bool, error) {
	var target, admin, serverCA, rootCA corev1.Secret
	for _, item := range []struct {
		name   string
		secret *corev1.Secret
	}{{"portal-kubeconfig", &target}, {kcpAdminSecretName, &admin}, {"root-server-ca", &serverCA}, {"root-ca", &rootCA}} {
		if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: item.name}, item.secret); err != nil {
			return false, fmt.Errorf("get %s Secret: %w", item.name, err)
		}
	}
	if target.Annotations != nil && target.Annotations[portalCredentialAnnotation] == string(pr.UID) {
		return false, nil
	}
	portalConfig, err := clientcmd.Load(target.Data["kubeconfig"])
	if err != nil {
		return false, fmt.Errorf("parse portal kubeconfig: %w", err)
	}
	adminConfig, err := clientcmd.Load(admin.Data["kubeconfig"])
	if err != nil {
		return false, fmt.Errorf("parse destination KCP admin kubeconfig: %w", err)
	}
	_, adminAuth, err := currentAuthInfoEntry(adminConfig)
	if err != nil {
		return false, fmt.Errorf("select destination KCP admin credential: %w", err)
	}
	if len(serverCA.Data["tls.crt"]) == 0 || len(rootCA.Data["tls.crt"]) == 0 {
		return false, fmt.Errorf("destination KCP CA bundle is incomplete")
	}
	caBundle := append(append([]byte{}, serverCA.Data["tls.crt"]...), '\n')
	caBundle = append(caBundle, rootCA.Data["tls.crt"]...)
	for _, auth := range portalConfig.AuthInfos {
		*auth = *adminAuth.DeepCopy()
	}
	for _, cluster := range portalConfig.Clusters {
		cluster.Server = strings.TrimRight(kcpBaseURLDefault, "/") + "/services/contentconfigurations"
		cluster.CertificateAuthority = ""
		cluster.CertificateAuthorityData = caBundle
		cluster.InsecureSkipTLSVerify = false
	}
	raw, err := clientcmd.Write(*portalConfig)
	if err != nil {
		return false, fmt.Errorf("write portal kubeconfig: %w", err)
	}
	base := target.DeepCopy()
	target.Data["kubeconfig"] = raw
	if target.Annotations == nil {
		target.Annotations = map[string]string{}
	}
	target.Annotations[portalCredentialAnnotation] = string(pr.UID)
	if err := p.client.Patch(ctx, &target, ctrlruntimeclient.MergeFrom(base)); err != nil {
		return false, fmt.Errorf("patch portal kubeconfig: %w", err)
	}
	if _, err := restartDeployment(ctx, p.client, platformDeployment("portal"), portalRestartAnnotation, string(pr.UID)); err != nil {
		return false, fmt.Errorf("restart portal after bootstrap credential repair: %w", err)
	}
	return true, nil
}
