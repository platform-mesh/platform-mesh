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
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Store adapts sub to the deprecated core.platform-mesh.io Store.
func Store(sub subroutines.Subroutine) *Subroutine {
	return &Subroutine{inner: sub, convert: convertStore}
}

// AuthorizationModel adapts sub to the deprecated core.platform-mesh.io AuthorizationModel.
func AuthorizationModel(sub subroutines.Subroutine) *Subroutine {
	return &Subroutine{inner: sub, convert: convertAuthorizationModel}
}

// APIExportPolicy adapts sub to the deprecated core.platform-mesh.io APIExportPolicy.
func APIExportPolicy(sub subroutines.Subroutine) *Subroutine {
	return &Subroutine{inner: sub, convert: convertAPIExportPolicy}
}

func convertStore(obj ctrlruntimeclient.Object) (ctrlruntimeclient.Object, func(), error) {
	//nolint:staticcheck // legacy kind served on purpose
	legacy, ok := obj.(*pmcorev1alpha1.Store)
	if !ok {
		return nil, nil, wrongType(obj, "*core.Store")
	}
	current := StoreToCurrent(legacy)
	return current, func() { storeFromCurrent(current, legacy) }, nil
}

func convertAuthorizationModel(obj ctrlruntimeclient.Object) (ctrlruntimeclient.Object, func(), error) {
	//nolint:staticcheck // legacy kind served on purpose
	legacy, ok := obj.(*pmcorev1alpha1.AuthorizationModel)
	if !ok {
		return nil, nil, wrongType(obj, "*core.AuthorizationModel")
	}
	current := AuthorizationModelToCurrent(legacy)
	return current, func() { authorizationModelFromCurrent(current, legacy) }, nil
}

func convertAPIExportPolicy(obj ctrlruntimeclient.Object) (ctrlruntimeclient.Object, func(), error) {
	//nolint:staticcheck // legacy kind served on purpose
	legacy, ok := obj.(*pmcorev1alpha1.APIExportPolicy)
	if !ok {
		return nil, nil, wrongType(obj, "*core.APIExportPolicy")
	}
	current := apiExportPolicyToCurrent(legacy)
	return current, func() { apiExportPolicyFromCurrent(current, legacy) }, nil
}

func tuplesToCurrent(in []pmcorev1alpha1.Tuple) []pmopenfgav1alpha1.Tuple {
	if in == nil {
		return nil
	}
	out := make([]pmopenfgav1alpha1.Tuple, 0, len(in))
	for _, t := range in {
		out = append(out, pmopenfgav1alpha1.Tuple{Object: t.Object, Relation: t.Relation, User: t.User})
	}
	return out
}

func tuplesFromCurrent(in []pmopenfgav1alpha1.Tuple) []pmcorev1alpha1.Tuple {
	if in == nil {
		return nil
	}
	out := make([]pmcorev1alpha1.Tuple, 0, len(in))
	for _, t := range in {
		out = append(out, pmcorev1alpha1.Tuple{Object: t.Object, Relation: t.Relation, User: t.User})
	}
	return out
}

// StoreToCurrent converts a deprecated Store to its openfga.platform-mesh.io shape.
//
//nolint:staticcheck // legacy kind served on purpose
func StoreToCurrent(in *pmcorev1alpha1.Store) *pmopenfgav1alpha1.Store {
	out := &pmopenfgav1alpha1.Store{
		ObjectMeta: *in.ObjectMeta.DeepCopy(),
		Spec: pmopenfgav1alpha1.StoreSpec{
			CoreModule: in.Spec.CoreModule,
			Tuples:     tuplesToCurrent(in.Spec.Tuples),
		},
		Status: pmopenfgav1alpha1.StoreStatus{
			Conditions:           in.Status.DeepCopy().Conditions,
			StoreID:              in.Status.StoreID,
			AuthorizationModelID: in.Status.AuthorizationModelID,
			ManagedTuples:        tuplesToCurrent(in.Status.ManagedTuples),
		},
	}
	out.APIVersion = pmopenfgav1alpha1.SchemeGroupVersion.String()
	out.Kind = "Store"
	return out
}

// storeFromCurrent copies metadata and status back; the spec is owner-controlled.
//
//nolint:staticcheck // legacy kind served on purpose
func storeFromCurrent(in *pmopenfgav1alpha1.Store, out *pmcorev1alpha1.Store) {
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = in.Status.DeepCopy().Conditions
	out.Status.StoreID = in.Status.StoreID
	out.Status.AuthorizationModelID = in.Status.AuthorizationModelID
	out.Status.ManagedTuples = tuplesFromCurrent(in.Status.ManagedTuples)
}

// AuthorizationModelToCurrent converts a deprecated AuthorizationModel to its openfga.platform-mesh.io shape.
//
//nolint:staticcheck // legacy kind served on purpose
func AuthorizationModelToCurrent(in *pmcorev1alpha1.AuthorizationModel) *pmopenfgav1alpha1.AuthorizationModel {
	out := &pmopenfgav1alpha1.AuthorizationModel{
		ObjectMeta: *in.ObjectMeta.DeepCopy(),
		Spec: pmopenfgav1alpha1.AuthorizationModelSpec{
			StoreRef: pmopenfgav1alpha1.WorkspaceStoreRef{
				Name:    in.Spec.StoreRef.Name,
				Cluster: in.Spec.StoreRef.Cluster,
				Path:    in.Spec.StoreRef.Path,
			},
			Model:  in.Spec.Model,
			Tuples: tuplesToCurrent(in.Spec.Tuples),
		},
		Status: pmopenfgav1alpha1.AuthorizationModelStatus{
			Conditions:    in.Status.DeepCopy().Conditions,
			ManagedTuples: tuplesToCurrent(in.Status.ManagedTuples),
		},
	}
	out.APIVersion = pmopenfgav1alpha1.SchemeGroupVersion.String()
	out.Kind = "AuthorizationModel"
	return out
}

// AuthorizationModelSpecFromCurrent converts a spec to the deprecated shape.
func AuthorizationModelSpecFromCurrent(in pmopenfgav1alpha1.AuthorizationModelSpec) pmcorev1alpha1.AuthorizationModelSpec {
	return pmcorev1alpha1.AuthorizationModelSpec{
		StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
			Name:    in.StoreRef.Name,
			Cluster: in.StoreRef.Cluster,
			Path:    in.StoreRef.Path,
		},
		Model:  in.Model,
		Tuples: tuplesFromCurrent(in.Tuples),
	}
}

//nolint:staticcheck // legacy kind served on purpose
func authorizationModelFromCurrent(in *pmopenfgav1alpha1.AuthorizationModel, out *pmcorev1alpha1.AuthorizationModel) {
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = in.Status.DeepCopy().Conditions
	out.Status.ManagedTuples = tuplesFromCurrent(in.Status.ManagedTuples)
}

//nolint:staticcheck // legacy kind served on purpose
func apiExportPolicyToCurrent(in *pmcorev1alpha1.APIExportPolicy) *pmopenfgav1alpha1.APIExportPolicy {
	out := &pmopenfgav1alpha1.APIExportPolicy{
		ObjectMeta: *in.ObjectMeta.DeepCopy(),
		Spec: pmopenfgav1alpha1.APIExportPolicySpec{
			APIExportRef: pmopenfgav1alpha1.APIExportRef{
				Name:        in.Spec.APIExportRef.Name,
				ClusterPath: in.Spec.APIExportRef.ClusterPath,
			},
			AllowPathExpressions: slices.Clone(in.Spec.AllowPathExpressions),
		},
		Status: pmopenfgav1alpha1.APIExportPolicyStatus{
			Conditions:              in.Status.DeepCopy().Conditions,
			ManagedAllowExpressions: slices.Clone(in.Status.ManagedAllowExpressions),
		},
	}
	out.APIVersion = pmopenfgav1alpha1.SchemeGroupVersion.String()
	out.Kind = "APIExportPolicy"
	return out
}

//nolint:staticcheck // legacy kind served on purpose
func apiExportPolicyFromCurrent(in *pmopenfgav1alpha1.APIExportPolicy, out *pmcorev1alpha1.APIExportPolicy) {
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = in.Status.DeepCopy().Conditions
	out.Status.ManagedAllowExpressions = slices.Clone(in.Status.ManagedAllowExpressions)
}
