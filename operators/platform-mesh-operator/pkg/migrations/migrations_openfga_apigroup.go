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

package migrations

import (
	"context"
	"slices"

	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	providerOpenfgaPlatformMeshIOExport = "provider-openfga.platform-mesh.io"
	// openfgaTerminator is the pm-openfga-operator terminator; it replaces
	// securityTerminator, which the removed security-operator served.
	// kcp stamps terminators at creation, so existing workspaces get it here.
	openfgaTerminator   = "root:openfga"
	securityTerminator  = "root:security"
	providersPath       = "root:providers"
	providersSystemName = "system"
)

// openfgaAPIGroupSplit prepares existing installations for the
// openfga.platform-mesh.io API group: organization and account
// LogicalClusters get the root:openfga terminator instead of root:security,
// provider workspaces bind provider-openfga.platform-mesh.io so the model
// generator writes AuthorizationModels there. Stores are migrated by
// pm-openfga-operator itself (legacy Stores create their successor).
type openfgaAPIGroupSplit struct{}

func (openfgaAPIGroupSplit) Name() string { return "openfga-apigroup-split" }

func (m openfgaAPIGroupSplit) Run(ctx context.Context, deps Deps) error {
	if hasFeatureToggle(deps.Instance, featureDisableOpenFGA) {
		return nil
	}
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("migration", m.Name())

	orgsClient, err := deps.KcpHelper.NewKcpClient(deps.Config, orgsPath)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", orgsPath)
	}
	var orgs kcptenancyv1alpha1.WorkspaceList
	if err := orgsClient.List(ctx, &orgs); err == nil {
		// root:orgs may not exist yet on a fresh install; nothing to migrate.
		for _, org := range orgs.Items {
			if org.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
				continue
			}
			if err := replaceSecurityTerminatorTree(ctx, deps, orgsPath+":"+org.Name); err != nil {
				return err
			}
			log.Debug().Str("organization", org.Name).Msg("organization checked")
		}
	}

	providersClient, err := deps.KcpHelper.NewKcpClient(deps.Config, providersPath)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", providersPath)
	}
	var providers kcptenancyv1alpha1.WorkspaceList
	if err := providersClient.List(ctx, &providers); err != nil {
		return nil //nolint:nilerr
	}
	for _, provider := range providers.Items {
		// Initializing workspaces have no bindings yet; applying one here collides
		// with the type's defaultAPIBindings.
		if provider.Name == providersSystemName || provider.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			continue
		}
		providerPath := providersPath + ":" + provider.Name
		providerClient, err := deps.KcpHelper.NewKcpClient(deps.Config, providerPath)
		if err != nil {
			return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", providerPath)
		}
		if err := ensureBinding(ctx, providerClient, providerOpenfgaPlatformMeshIOExport); err != nil {
			return gcerrors.Wrap(err, "Failed to bind %s in %s", providerOpenfgaPlatformMeshIOExport, providerPath)
		}
	}
	return nil
}

// replaceSecurityTerminatorTree swaps the terminator on the workspace at path
// and every account workspace below it.
func replaceSecurityTerminatorTree(ctx context.Context, deps Deps, path string) error {
	client, err := deps.KcpHelper.NewKcpClient(deps.Config, path)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", path)
	}
	if err := replaceSecurityTerminator(ctx, client); err != nil {
		return gcerrors.Wrap(err, "Failed to replace the %s terminator in %s", securityTerminator, path)
	}

	var children kcptenancyv1alpha1.WorkspaceList
	if err := client.List(ctx, &children); err != nil {
		return gcerrors.Wrap(err, "Failed to list workspaces in %s", path)
	}
	for _, child := range children.Items {
		if child.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			continue
		}
		if err := replaceSecurityTerminatorTree(ctx, deps, path+":"+child.Name); err != nil {
			return err
		}
	}
	return nil
}

// replaceSecurityTerminator puts root:openfga into the LogicalCluster's
// terminators where root:security was; nothing serves the old one anymore.
// kcp copies spec.terminators to the status at creation only and terminates
// by the status, so both are patched.
func replaceSecurityTerminator(ctx context.Context, client ctrlruntimeclient.Client) error {
	var lc kcpcorev1alpha1.LogicalCluster
	if err := client.Get(ctx, types.NamespacedName{Name: kcpcorev1alpha1.LogicalClusterName}, &lc); err != nil {
		return gcerrors.Wrap(err, "Failed to get LogicalCluster")
	}
	// Without the security terminator the workspace never extended the security
	// type (created without OpenFGA or already with openfga); nothing to hand over.
	spec := handOverSecurityTerminator(lc.Spec.Terminators)
	status := handOverSecurityTerminator(lc.Status.Terminators)

	if !slices.Equal(spec, lc.Spec.Terminators) {
		patch := ctrlruntimeclient.MergeFrom(lc.DeepCopy())
		lc.Spec.Terminators = spec
		if err := client.Patch(ctx, &lc, patch); err != nil {
			return gcerrors.Wrap(err, "Failed to patch spec.terminators")
		}
	}
	if !slices.Equal(status, lc.Status.Terminators) {
		patch := ctrlruntimeclient.MergeFrom(lc.DeepCopy())
		lc.Status.Terminators = status
		if err := client.Status().Patch(ctx, &lc, patch); err != nil {
			return gcerrors.Wrap(err, "Failed to patch status.terminators")
		}
	}
	return nil
}

// handOverSecurityTerminator returns terminators with root:openfga in place of root:security.
func handOverSecurityTerminator(terminators []kcpcorev1alpha1.LogicalClusterTerminator) []kcpcorev1alpha1.LogicalClusterTerminator {
	if !slices.Contains(terminators, securityTerminator) {
		return terminators
	}
	out := slices.DeleteFunc(slices.Clone(terminators), func(t kcpcorev1alpha1.LogicalClusterTerminator) bool {
		return t == securityTerminator
	})
	if !slices.Contains(out, openfgaTerminator) {
		out = append(out, openfgaTerminator)
	}
	return out
}

// ensureBinding binds export from root:platform-mesh-system into an existing
// workspace; kcp applies a type's defaultAPIBindings only at creation.
func ensureBinding(ctx context.Context, client ctrlruntimeclient.Client, export string) error {
	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBindingList"})
	if err := client.List(ctx, bindings); err != nil {
		return gcerrors.Wrap(err, "Failed to list APIBindings")
	}
	if hasBindingToExport(bindings, export) {
		return nil
	}
	return applyBinding(ctx, client, export, export, platformMeshSystemPath, nil)
}
