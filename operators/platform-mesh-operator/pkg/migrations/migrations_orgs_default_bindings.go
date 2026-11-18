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

	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const platformMeshSystemPath = "root:platform-mesh-system"

// orgsDefaultBindings removes the core.platform-mesh.io and fga.platform-mesh.io bindings
// root:orgs got from the orgs WorkspaceType's defaultAPIBindings.
// The bindings applied from the kcp manifests adopt their instances.
// Safe to remove once every environment has reconciled past this change.
//
// Background is that the APIBinding was applied both via the
// defaultAPIBindings and the pm-operator, leading to duplicated
// APIBindings, one of which fails to apply.
type orgsDefaultBindings struct{}

func (orgsDefaultBindings) Name() string { return "orgs-default-bindings" }

func (orgsDefaultBindings) Run(ctx context.Context, deps Deps) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("migration", "orgs-default-bindings")

	orgsClient, err := deps.KcpHelper.NewKcpClient(deps.Config, "root:orgs")
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for root:orgs workspace")
	}

	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBindingList"})
	if err := orgsClient.List(ctx, bindings); err != nil {
		// root:orgs may not exist yet on a fresh install; nothing to migrate.
		return nil //nolint:nilerr
	}

	for _, export := range []string{corePlatformMeshIOExport, fgaPlatformMeshIOExport} {
		// Without the manifest binding nothing adopts the instances.
		if !hasManifestBinding(bindings, export) {
			continue
		}

		for i := range bindings.Items {
			binding := &bindings.Items[i]
			if binding.GetName() == export || !bindsExport(binding, export) {
				continue
			}

			if err := deleteWithSuccessorWait(ctx, orgsClient, binding); err != nil {
				return gcerrors.Wrap(err, "Failed to remove default %s binding in root:orgs", export)
			}
			if err := waitForBindingGone(ctx, orgsClient, binding.GetName()); err != nil {
				return gcerrors.Wrap(err, "Failed to remove default %s binding in root:orgs", export)
			}

			log.Info().Str("binding", binding.GetName()).Str("successor", export).
				Msg("default binding removed from root:orgs, resources adopted by the manifest binding")
		}
	}

	return nil
}

// hasManifestBinding reports whether bindings holds the binding the kcp manifests apply for export.
func hasManifestBinding(bindings *unstructured.UnstructuredList, export string) bool {
	for i := range bindings.Items {
		binding := &bindings.Items[i]
		if binding.GetName() == export && bindsExport(binding, export) && binding.GetDeletionTimestamp() == nil {
			return true
		}
	}
	return false
}

// bindsExport reports whether binding references export in root:platform-mesh-system.
func bindsExport(binding *unstructured.Unstructured, export string) bool {
	name, _, _ := unstructured.NestedString(binding.Object, "spec", "reference", "export", "name")
	path, _, _ := unstructured.NestedString(binding.Object, "spec", "reference", "export", "path")
	return name == export && path == platformMeshSystemPath
}
