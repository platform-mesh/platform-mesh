package backup

import (
	"context"
	"fmt"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	credentialInventorySubroutineName = "credential-inventory"
	RestoreCredentialsLabel           = "platform-mesh.io/restore-credentials"
)

var identityCredentialSecrets = []string{
	"keycloak-admin",
	"keycloak-db-credentials",
	"cnpg-keycloak-user",

	"iam-client-secret",
	"iam-service-kubeconfig",
	"security-operator-client-secret",
	"security-operator-kubeconfig",
	"kcp-webhook-secret",

	"security-initializer-kubeconfig",
	"security-operator-ca-secret",
	"security-operator-webhook-server-cert",
	"security-terminator-kubeconfig",

	"kcp-cluster-admin-client-cert",
	"kubeconfig-kcp-admin",
	"kubeconfig-cert-kubeconfig-kcp-admin",

	"frontproxy-merged-client-ca",

	"root-ca",
	"root-server",
	"root-server-ca",
	"root-client",
	"root-client-ca",
	"root-service-account",
	"root-service-account-ca",
	"root-requestheader-client-ca",
	"root-frontproxy-kubeconfig",
	"root-frontproxy-requestheader",
	"root-frontproxy-server",
	"root-proxy-kubeconfig",
	"root-proxy-merged-client-ca",
	"root-proxy-requestheader",
	"root-proxy-server",
	"root-kcp-operator",
	"root-virtual-workspaces",
	"root-logical-cluster-admin",
	"root-logical-cluster-admin-kubeconfig",
	"root-external-logical-cluster-admin",
	"root-external-logical-cluster-admin-kubeconfig",

	"nereus-client",
	"nereus-client-kubeconfig",
	"nereus-server",
	"nereus-service-account",
	"nereus-virtual-workspaces",
	"nereus-logical-cluster-admin",
	"nereus-logical-cluster-admin-kubeconfig",
	"nereus-external-logical-cluster-admin",
	"nereus-external-logical-cluster-admin-kubeconfig",

	"triton-client",
	"triton-client-kubeconfig",
	"triton-server",
	"triton-service-account",
	"triton-virtual-workspaces",
	"triton-logical-cluster-admin",
	"triton-logical-cluster-admin-kubeconfig",
	"triton-external-logical-cluster-admin",
	"triton-external-logical-cluster-admin-kubeconfig",

	"cache-server-client-certificate",
	"cache-server-kubeconfig",
}

type CredentialInventorySubroutine struct {
	name      string
	namespace string
	client    ctrlruntimeclient.Client
}

func NewCredentialInventorySubroutine(namespace string, cli ctrlruntimeclient.Client) *CredentialInventorySubroutine {
	return &CredentialInventorySubroutine{
		name:      credentialInventorySubroutineName,
		namespace: namespace,
		client:    cli,
	}
}

func (c *CredentialInventorySubroutine) GetName() string {
	return c.name
}

func (c *CredentialInventorySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	backup, ok := obj.(*v1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected v1alpha1.PlatformBackup, got %T", obj)
	}

	log := logger.LoadLoggerFromContext(ctx)

	for _, name := range identityCredentialSecrets {
		var secret corev1.Secret
		err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Namespace: c.namespace,
			Name:      name,
		}, &secret)

		if apierrors.IsNotFound(err) {
			log.Warn().
				Str("subroutine", c.name).
				Str("platformBackup", backup.Name).
				Str("secret", name).
				Msg("identity credential secret not found; skipping")
			continue
		}

		if err != nil {
			return subroutines.OK(), false, fmt.Errorf("failed to get credential secret %s/%s: %w", c.namespace, name, err)
		}

		if secret.Labels == nil {
			secret.Labels = map[string]string{}
		}

		if secret.Labels[RestoreCredentialsLabel] == "true" {
			continue
		}

		patchBase := secret.DeepCopy()
		secret.Labels[RestoreCredentialsLabel] = "true"

		if err := c.client.Patch(ctx, &secret, ctrlruntimeclient.MergeFrom(patchBase)); err != nil {
			return subroutines.OK(), false, fmt.Errorf("failed to label credential secret %s/%s: %w", c.namespace, name, err)
		}

		log.Info().
			Str("subroutine", c.name).
			Str("platformBackup", backup.Name).
			Str("secret", name).
			Msg("labeled identity credential secret for restore")
	}

	return subroutines.OK(), false, nil
}
