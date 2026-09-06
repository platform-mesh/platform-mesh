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

package webhook

import (
	"context"
	"fmt"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	iclient "go.platform-mesh.io/security-operator/internal/client"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"

	"github.com/kcp-dev/logicalcluster/v3"
)

type clusterClientResolver interface {
	ClusterClient(ctx context.Context, reg *pmcorev1alpha1.IdPRegistration) (ctrlruntimeclient.Client, error)
}

// SetupIdPRegistrationMutatingWebhookWithManager registers mutation for IdPRegistration.
func SetupIdPRegistrationMutatingWebhookWithManager(mgr ctrl.Manager, kcpClientGetter iclient.KCPClientGetter) error {
	return SetupIdPRegistrationMutatingWebhookWithClusterResolver(mgr, clusterClientResolverFromGetter(kcpClientGetter))
}

type kcpClientGetterResolver struct {
	getter iclient.KCPClientGetter
}

func clusterClientResolverFromGetter(getter iclient.KCPClientGetter) clusterClientResolver {
	return kcpClientGetterResolver{getter: getter}
}

func (r kcpClientGetterResolver) ClusterClient(ctx context.Context, reg *pmcorev1alpha1.IdPRegistration) (ctrlruntimeclient.Client, error) {
	if cl, err := r.getter.NewClientFromContext(ctx); err == nil {
		return cl, nil
	}

	lc := logicalcluster.From(reg)
	if lc.Empty() {
		return nil, fmt.Errorf("no cluster set in context and %q annotation missing on object", logicalcluster.AnnotationKey)
	}

	cl, err := r.getter.NewClientForLogicalCluster(ctx, lc.String())
	if err != nil {
		return nil, fmt.Errorf("getting client for logical cluster %q: %w", lc, err)
	}

	return cl, nil
}

func SetupIdPRegistrationMutatingWebhookWithClusterResolver(mgr ctrl.Manager, resolver clusterClientResolver) error {
	return mcruntime.NewWebhookManagedBy(mgr, &pmcorev1alpha1.IdPRegistration{}).
		WithDefaulter(&idpRegistrationDefaulter{resolver: resolver}).
		Complete()
}

var _ admission.Defaulter[*pmcorev1alpha1.IdPRegistration] = (*idpRegistrationDefaulter)(nil)

type idpRegistrationDefaulter struct {
	resolver clusterClientResolver
}

func (d *idpRegistrationDefaulter) Default(ctx context.Context, reg *pmcorev1alpha1.IdPRegistration) error {
	if reg.Spec.OIDC == nil {
		return nil
	}

	secretValue := strings.TrimSpace(reg.Spec.OIDC.ClientSecret)
	reg.Spec.OIDC.ClientSecret = ""

	if secretValue == "" {
		return nil
	}

	cl, err := d.resolver.ClusterClient(ctx, reg)
	if err != nil {
		return fmt.Errorf("resolving cluster client: %w", err)
	}

	secretName := strings.TrimSpace(reg.Spec.OIDC.ClientSecretRef.Name)
	if secretName == "" {
		if strings.TrimSpace(reg.Name) == "" {
			return fmt.Errorf("metadata.name is required when setting oidc.clientSecret")
		}
		secretName = clientSecretNameForRegistration(reg.Name)
	}

	if err := upsertClientSecret(ctx, cl, secretName, secretValue, reg.Name); err != nil {
		return fmt.Errorf("persisting client secret: %w", err)
	}

	reg.Spec.OIDC.ClientSecretRef.Name = secretName
	return nil
}
