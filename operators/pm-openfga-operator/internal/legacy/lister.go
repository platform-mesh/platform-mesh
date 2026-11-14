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
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// AuthorizationModelLister lists openfga.platform-mesh.io AuthorizationModels
// together with the deprecated ones that have no successor yet, converted.
// Other list types are served by Current alone.
type AuthorizationModelLister struct {
	Current iclient.Lister
	// Legacy lists the deprecated kind; nil without LegacyAPIs.
	Legacy iclient.Lister
}

var _ iclient.Lister = AuthorizationModelLister{}

// List implements iclient.Lister.
func (l AuthorizationModelLister) List(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
	models, ok := list.(*pmopenfgav1alpha1.AuthorizationModelList)
	if !ok || l.Legacy == nil {
		return l.Current.List(ctx, list, opts...)
	}
	if err := l.Current.List(ctx, models, opts...); err != nil {
		return err
	}
	//nolint:staticcheck // legacy kind served on purpose
	var legacyModels pmcorev1alpha1.AuthorizationModelList
	if err := l.Legacy.List(ctx, &legacyModels, opts...); err != nil {
		return fmt.Errorf("listing legacy AuthorizationModels: %w", err)
	}
	for i := range legacyModels.Items {
		if Migrated(&legacyModels.Items[i]) {
			continue
		}
		models.Items = append(models.Items, *AuthorizationModelToCurrent(&legacyModels.Items[i]))
	}
	return nil
}
