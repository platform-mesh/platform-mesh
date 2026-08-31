/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License for the specific language governing permissions and
limitations under the License.
*/

package idpregistration

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clientSecretNamespace = "default"
	clientSecretLabel     = "core.platform-mesh.io/idpregistration"
)

func deleteManagedClientSecret(ctx context.Context, cl ctrlruntimeclient.Client, secretName, registrationName string) error {
	if strings.TrimSpace(secretName) == "" {
		return nil
	}

	secret := &corev1.Secret{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: secretName, Namespace: clientSecretNamespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if secret.Labels[clientSecretLabel] != registrationName {
		return fmt.Errorf("secret %q is not managed by IdPRegistration %q", secretName, registrationName)
	}

	if err := cl.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
