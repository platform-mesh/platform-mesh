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

package controller

import (
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	"go.platform-mesh.io/pm-openfga-operator/internal/legacy"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// chain wraps subs for the kind, preceded by before, if any.
func chain(mgr mcmanager.Manager, before func(mcmanager.Manager) subroutines.Subroutine, wrap func(subroutines.Subroutine) subroutines.Subroutine, subs ...subroutines.Subroutine) []subroutines.Subroutine {
	out := make([]subroutines.Subroutine, 0, len(subs)+1)
	if before != nil {
		out = append(out, before(mgr))
	}
	for _, sub := range subs {
		out = append(out, wrap(sub))
	}
	return out
}

// Kinds selects the API group a reconciler serves: the current
// openfga.platform-mesh.io kinds or the deprecated core.platform-mesh.io ones
// (feature gate LegacyAPIs).
type Kinds struct {
	// Suffix distinguishes controller and metric names ("" or "-legacy").
	Suffix string
	// Wrap* adapt a subroutine written for the current kinds to the kind
	// the reconciler runs on.
	WrapStore              func(subroutines.Subroutine) subroutines.Subroutine
	WrapAuthorizationModel func(subroutines.Subroutine) subroutines.Subroutine
	WrapAPIExportPolicy    func(subroutines.Subroutine) subroutines.Subroutine
	// Before* run first in the chain, unwrapped: the current kinds adopt
	// same-name deprecated objects, legacy Stores create their successor.
	BeforeStore              func(mcmanager.Manager) subroutines.Subroutine
	BeforeAuthorizationModel func(mcmanager.Manager) subroutines.Subroutine
	BeforeAPIExportPolicy    func(mcmanager.Manager) subroutines.Subroutine

	NewStore               func() ctrlruntimeclient.Object
	NewAuthorizationModel  func() ctrlruntimeclient.Object
	NewAPIExportPolicy     func() ctrlruntimeclient.Object
	NewAPIExportPolicyList func() ctrlruntimeclient.ObjectList
	// AuthorizationModelStoreRef returns the Store an AuthorizationModel of this group belongs to.
	AuthorizationModelStoreRef func(ctrlruntimeclient.Object) (name, cluster string, ok bool)
	// APIExportPolicyAllowPathExpressions returns spec.allowPathExpressions of an APIExportPolicy of this group.
	APIExportPolicyAllowPathExpressions func(ctrlruntimeclient.Object) []string

	// Providers whose clusters serve the kinds.
	StoreProvider              string
	AuthorizationModelProvider string
	APIExportPolicyProvider    string
}

// CurrentKinds serves openfga.platform-mesh.io.
func CurrentKinds() Kinds {
	return Kinds{
		WrapStore:                identity,
		WrapAuthorizationModel:   identity,
		WrapAPIExportPolicy:      identity,
		BeforeStore:              legacy.StoreAdopter,
		BeforeAuthorizationModel: legacy.AuthorizationModelAdopter,
		BeforeAPIExportPolicy:    legacy.APIExportPolicyAdopter,
		NewStore:                 func() ctrlruntimeclient.Object { return &pmopenfgav1alpha1.Store{} },
		NewAuthorizationModel:    func() ctrlruntimeclient.Object { return &pmopenfgav1alpha1.AuthorizationModel{} },
		NewAPIExportPolicy:       func() ctrlruntimeclient.Object { return &pmopenfgav1alpha1.APIExportPolicy{} },
		NewAPIExportPolicyList:   func() ctrlruntimeclient.ObjectList { return &pmopenfgav1alpha1.APIExportPolicyList{} },
		AuthorizationModelStoreRef: func(obj ctrlruntimeclient.Object) (string, string, bool) {
			model, ok := obj.(*pmopenfgav1alpha1.AuthorizationModel)
			if !ok {
				return "", "", false
			}
			return model.Spec.StoreRef.Name, model.Spec.StoreRef.Cluster, true
		},
		APIExportPolicyAllowPathExpressions: func(obj ctrlruntimeclient.Object) []string {
			policy, ok := obj.(*pmopenfgav1alpha1.APIExportPolicy)
			if !ok {
				return nil
			}
			return policy.Spec.AllowPathExpressions
		},
		StoreProvider:              config.OpenfgaProviderName,
		AuthorizationModelProvider: config.ProviderOpenfgaProviderName,
		APIExportPolicyProvider:    config.OpenfgaProviderName,
	}
}

// LegacyKinds serves the deprecated core.platform-mesh.io kinds through the
// fga, provider-fga and system exports.
//
//nolint:staticcheck // legacy kinds served on purpose
func LegacyKinds() Kinds {
	return Kinds{
		Suffix:                 "-legacy",
		BeforeStore:            legacy.StoreSuccessor,
		WrapStore:              func(sub subroutines.Subroutine) subroutines.Subroutine { return legacy.Store(sub) },
		WrapAuthorizationModel: func(sub subroutines.Subroutine) subroutines.Subroutine { return legacy.AuthorizationModel(sub) },
		WrapAPIExportPolicy:    func(sub subroutines.Subroutine) subroutines.Subroutine { return legacy.APIExportPolicy(sub) },
		NewStore:               func() ctrlruntimeclient.Object { return &pmcorev1alpha1.Store{} },
		NewAuthorizationModel:  func() ctrlruntimeclient.Object { return &pmcorev1alpha1.AuthorizationModel{} },
		NewAPIExportPolicy:     func() ctrlruntimeclient.Object { return &pmcorev1alpha1.APIExportPolicy{} },
		NewAPIExportPolicyList: func() ctrlruntimeclient.ObjectList { return &pmcorev1alpha1.APIExportPolicyList{} },
		AuthorizationModelStoreRef: func(obj ctrlruntimeclient.Object) (string, string, bool) {
			model, ok := obj.(*pmcorev1alpha1.AuthorizationModel)
			if !ok {
				return "", "", false
			}
			return model.Spec.StoreRef.Name, model.Spec.StoreRef.Cluster, true
		},
		APIExportPolicyAllowPathExpressions: func(obj ctrlruntimeclient.Object) []string {
			policy, ok := obj.(*pmcorev1alpha1.APIExportPolicy)
			if !ok {
				return nil
			}
			return policy.Spec.AllowPathExpressions
		},
		StoreProvider:              config.FgaProviderName,
		AuthorizationModelProvider: config.ProviderFgaProviderName,
		APIExportPolicyProvider:    config.SystemProviderName,
	}
}

// StoreEngageOptions engages the clusters serving Stores of this group.
func (k Kinds) StoreEngageOptions() mcbuilder.EngageOptions {
	return WithClustersFromNamedProvider(k.StoreProvider, config.ProviderSeparator)
}

// AuthorizationModelEngageOptions engages the clusters serving AuthorizationModels of this group.
func (k Kinds) AuthorizationModelEngageOptions() mcbuilder.EngageOptions {
	return WithClustersFromNamedProvider(k.AuthorizationModelProvider, config.ProviderSeparator)
}

// APIExportPolicyEngageOptions engages the clusters serving APIExportPolicies of this group.
func (k Kinds) APIExportPolicyEngageOptions() mcbuilder.EngageOptions {
	return WithClustersFromNamedProvider(k.APIExportPolicyProvider, config.ProviderSeparator)
}

func identity(sub subroutines.Subroutine) subroutines.Subroutine { return sub }
