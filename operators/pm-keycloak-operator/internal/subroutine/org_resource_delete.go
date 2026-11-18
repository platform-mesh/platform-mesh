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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const orgResourceDeleteRequeue = 5 * time.Second

// deleteOrgResource deletes a named resource in root:orgs and reports whether
// deletion is still in progress (object exists or has a deletion timestamp).
func deleteOrgResource(ctx context.Context, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, name string) (pending bool, err error) {
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, err
	}

	if obj.GetDeletionTimestamp() != nil {
		return true, nil
	}

	if err := cl.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	return true, nil
}
