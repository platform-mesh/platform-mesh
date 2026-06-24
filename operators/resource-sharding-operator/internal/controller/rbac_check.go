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
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func CheckTargetPermissions(ctx context.Context, c ctrlruntimeclient.Client, gvr schema.GroupVersionResource) error {
	requiredVerbs := []string{"get", "list", "watch", "patch"}

	for _, verb := range requiredVerbs {
		sar := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:    gvr.Group,
					Resource: gvr.Resource,
					Verb:     verb,
				},
			},
		}

		if err := c.Create(ctx, sar); err != nil {
			return fmt.Errorf("creating SSAR for verb %q: %w", verb, err)
		}

		if !sar.Status.Allowed {
			return fmt.Errorf("missing permission: %s on %s", verb, gvr.String())
		}
	}

	return nil
}
