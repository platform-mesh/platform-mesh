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

package legacy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// adopter hands the backend state of a deprecated object over to its
// openfga.platform-mesh.io successor of the same name in the same logical
// cluster: the legacy object is marked migrated and its status is copied
// once, so the successor continues with the existing OpenFGA store, tuples or
// policy tuples instead of recreating them.
// legacyProvider is the provider serving the deprecated kind.
type adopter struct {
	name           string
	mgr            mcmanager.Manager
	legacyProvider string
	newLegacy      func() ctrlruntimeclient.Object
	// handOver copies the legacy status into current, if current has none yet,
	// and may move legacy metadata along; legacy is patched afterwards.
	handOver func(legacy, current ctrlruntimeclient.Object)
}

var _ subroutines.Processor = &adopter{}

// GetName implements subroutines.Subroutine.
func (a *adopter) GetName() string { return a.name }

// Process implements subroutines.Processor.
func (a *adopter) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	engaged, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.OK(), errors.New("no cluster in context")
	}
	cluster, err := a.mgr.GetCluster(ctx, config.MultiProviderName(a.legacyProvider, config.LogicalClusterName(engaged)))
	if errors.Is(err, multicluster.ErrClusterNotFound) {
		return subroutines.OK(), nil
	}
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting legacy cluster for %q: %w", engaged, err)
	}
	cl := cluster.GetClient()
	legacy := a.newLegacy()
	if err := cl.Get(ctx, types.NamespacedName{Name: obj.GetName()}, legacy); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), fmt.Errorf("getting legacy %s %q: %w", a.name, obj.GetName(), err)
	}
	if Migrated(legacy) {
		return subroutines.OK(), nil
	}
	patch := ctrlruntimeclient.MergeFrom(legacy.DeepCopyObject().(ctrlruntimeclient.Object))
	a.handOver(legacy, obj)
	annotations := legacy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[MigratedAnnotation] = "true"
	legacy.SetAnnotations(annotations)
	if err := cl.Patch(ctx, legacy, patch); err != nil {
		return subroutines.OK(), fmt.Errorf("marking legacy %s %q migrated: %w", a.name, obj.GetName(), err)
	}
	return subroutines.OK(), nil
}

// StoreAdopter takes over the OpenFGA store of a same-name deprecated Store.
func StoreAdopter(mgr mcmanager.Manager) subroutines.Subroutine {
	return &adopter{
		name:           "StoreAdopter",
		mgr:            mgr,
		legacyProvider: config.FgaProviderName,
		//nolint:staticcheck // legacy kind served on purpose
		newLegacy: func() ctrlruntimeclient.Object { return &pmcorev1alpha1.Store{} },
		handOver: func(legacyObj, currentObj ctrlruntimeclient.Object) {
			//nolint:staticcheck // legacy kind served on purpose
			legacy := legacyObj.(*pmcorev1alpha1.Store)
			current := currentObj.(*pmopenfgav1alpha1.Store)
			moveDependencyFinalizers(legacy, current)
			if current.Status.StoreID != "" {
				return
			}
			current.Status.StoreID = legacy.Status.StoreID
			current.Status.AuthorizationModelID = legacy.Status.AuthorizationModelID
			current.Status.ManagedTuples = tuplesToCurrent(legacy.Status.ManagedTuples)
		},
	}
}

// moveDependencyFinalizers hands the AuthorizationModel reservations
// (finalizers with DependencyFinalizerPrefix) from the legacy Store to its
// successor, so the legacy Store can be released and the successor is
// protected until the models are gone.
func moveDependencyFinalizers(from, to ctrlruntimeclient.Object) {
	kept := make([]string, 0, len(from.GetFinalizers()))
	for _, f := range from.GetFinalizers() {
		if !strings.HasPrefix(f, DependencyFinalizerPrefix) {
			kept = append(kept, f)
			continue
		}
		if !slices.Contains(to.GetFinalizers(), f) {
			to.SetFinalizers(append(to.GetFinalizers(), f))
		}
	}
	from.SetFinalizers(kept)
}

// AuthorizationModelAdopter takes over the tuples of a same-name deprecated AuthorizationModel.
func AuthorizationModelAdopter(mgr mcmanager.Manager) subroutines.Subroutine {
	return &adopter{
		name:           "AuthorizationModelAdopter",
		mgr:            mgr,
		legacyProvider: config.ProviderFgaProviderName,
		//nolint:staticcheck // legacy kind served on purpose
		newLegacy: func() ctrlruntimeclient.Object { return &pmcorev1alpha1.AuthorizationModel{} },
		handOver: func(legacyObj, currentObj ctrlruntimeclient.Object) {
			//nolint:staticcheck // legacy kind served on purpose
			legacy := legacyObj.(*pmcorev1alpha1.AuthorizationModel)
			current := currentObj.(*pmopenfgav1alpha1.AuthorizationModel)
			if current.Status.ManagedTuples != nil {
				return
			}
			current.Status.ManagedTuples = tuplesToCurrent(legacy.Status.ManagedTuples)
		},
	}
}

// APIExportPolicyAdopter takes over the managed expressions of a same-name deprecated APIExportPolicy.
func APIExportPolicyAdopter(mgr mcmanager.Manager) subroutines.Subroutine {
	return &adopter{
		name:           "APIExportPolicyAdopter",
		mgr:            mgr,
		legacyProvider: config.SystemProviderName,
		//nolint:staticcheck // legacy kind served on purpose
		newLegacy: func() ctrlruntimeclient.Object { return &pmcorev1alpha1.APIExportPolicy{} },
		handOver: func(legacyObj, currentObj ctrlruntimeclient.Object) {
			//nolint:staticcheck // legacy kind served on purpose
			legacy := legacyObj.(*pmcorev1alpha1.APIExportPolicy)
			current := currentObj.(*pmopenfgav1alpha1.APIExportPolicy)
			if current.Status.ManagedAllowExpressions != nil {
				return
			}
			current.Status.ManagedAllowExpressions = append([]string(nil), legacy.Status.ManagedAllowExpressions...)
		},
	}
}

// storeSuccessor creates the openfga.platform-mesh.io Store for a deprecated
// Store that has none yet. Stores are platform-owned (created by the org
// initializer and the platform-mesh-operator), so the operator migrates them
// itself; the successor then adopts the legacy status.
type storeSuccessor struct {
	mgr mcmanager.Manager
}

var _ subroutines.Processor = &storeSuccessor{}

// StoreSuccessor runs first on the legacy Store chain.
func StoreSuccessor(mgr mcmanager.Manager) subroutines.Subroutine {
	return &storeSuccessor{mgr: mgr}
}

// GetName implements subroutines.Subroutine.
func (s *storeSuccessor) GetName() string { return "StoreSuccessor" }

// Process implements subroutines.Processor.
//
//nolint:staticcheck // legacy kind served on purpose
func (s *storeSuccessor) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	legacy, ok := obj.(*pmcorev1alpha1.Store)
	if !ok {
		return subroutines.OK(), wrongType(obj, "*core.Store")
	}
	if Migrated(legacy) {
		return subroutines.OK(), nil
	}
	engaged, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.OK(), errors.New("no cluster in context")
	}
	cluster, err := s.mgr.GetCluster(ctx, config.MultiProviderName(config.OpenfgaProviderName, config.LogicalClusterName(engaged)))
	if errors.Is(err, multicluster.ErrClusterNotFound) {
		// Not bound to openfga.platform-mesh.io yet.
		return subroutines.OK(), nil
	}
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting openfga cluster for %q: %w", engaged, err)
	}
	var current pmopenfgav1alpha1.Store
	err = cluster.GetClient().Get(ctx, types.NamespacedName{Name: legacy.Name}, &current)
	if err == nil {
		return subroutines.OK(), nil
	}
	if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return subroutines.OK(), fmt.Errorf("getting Store %q: %w", legacy.Name, err)
	}
	current = pmopenfgav1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{Name: legacy.Name},
		Spec:       StoreToCurrent(legacy).Spec,
	}
	if err := cluster.GetClient().Create(ctx, &current); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), fmt.Errorf("creating Store %q: %w", legacy.Name, err)
	}
	return subroutines.OK(), nil
}
