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

package subroutine

import (
	"context"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

type ProviderVisibilityPolicySubroutine struct {
	kcpClientGetter iclient.KCPClientGetter
}

func NewProviderVisibilityPolicySubroutine(kcp iclient.KCPClientGetter) *ProviderVisibilityPolicySubroutine {
	return &ProviderVisibilityPolicySubroutine{
		kcpClientGetter: kcp,
	}
}

var _ subroutines.Processor = &ProviderVisibilityPolicySubroutine{}

// GetName implements [subroutines.Processor].
func (p *ProviderVisibilityPolicySubroutine) GetName() string {
	return "ProviderVisibilityPolicySubroutine"
}

func (p *ProviderVisibilityPolicySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	policy, ok := obj.(*pmcorev1alpha1.ProviderVisibilityPolicy)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected type %T for reconciler %q", obj, p.GetName())
	}

	var providerExports []pmcorev1alpha1.ResolvedProviderExport
	for _, export := range policy.Spec.ProviderExports {
		// provider Cluster ID
		clusterID, err := clusterIDFromPath(
			ctx,
			p.kcpClientGetter,
			export.ProviderRef.ClusterPath,
		)
		if err != nil {
			return subroutines.OK(), fmt.Errorf(
				"getting client for cluster %q: %w", export.ProviderRef.ClusterPath, err)
		}
		resolvedExport := pmcorev1alpha1.ResolvedProviderExport{
			ClusterID:      clusterID,
			ClusterPath:    export.ProviderRef.ClusterPath,
			APIExportNames: export.APIExportNames,
		}
		providerExports = append(providerExports, resolvedExport)
	}

	orig := policy.DeepCopy()
	policy.Status.ResolvedProviderExports = providerExports

	cl, err := p.kcpClientGetter.NewClientFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting client from context: %w", err)
	}
	if err := cl.Status().Patch(ctx, policy, ctrlruntimeclient.MergeFrom(orig)); err != nil {
		return subroutines.OK(), fmt.Errorf("failed to patch ProviderVisibilityPolicy status: %w", err)
	}

	return subroutines.OK(), nil
}

// clusterIDFromPath returns the cluster ID stored as annotation on a cluster object at the given path.
func clusterIDFromPath(ctx context.Context, kcp iclient.KCPClientGetter, clusterPath string) (string, error) {
	kcpClient, err := kcp.NewClientForLogicalCluster(
		ctx,
		string(config.MultiProviderName(config.CoreProviderName, clusterPath)),
	)

	if err != nil {
		return "", err
	}

	var lc kcpcorev1alpha1.LogicalCluster
	err = kcpClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, &lc)
	if err != nil {
		return "", err
	}

	clusterID, ok := lc.Annotations["kcp.io/cluster"]
	if !ok {
		return "", fmt.Errorf("kcp.io/cluster annotation not found on logical cluster %s", clusterPath)
	}

	return clusterID, nil
}
