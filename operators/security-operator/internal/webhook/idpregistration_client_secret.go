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

package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	idpRegistrationSecretNamespace = "default"
	idpRegistrationSecretDataKey   = "client_secret"
	idpRegistrationSecretLabel     = "core.platform-mesh.io/idpregistration"
)

func clientSecretNameForRegistration(registrationName string) string {
	return fmt.Sprintf("idpregistration-%s-client-secret", registrationName)
}

func upsertClientSecret(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	secretName string,
	secretValue string,
	registrationName string,
) error {
	key := ctrlruntimeclient.ObjectKey{Name: secretName, Namespace: idpRegistrationSecretNamespace}

	existing := &corev1.Secret{}
	err := cl.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		return cl.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: idpRegistrationSecretNamespace,
				Labels: map[string]string{
					idpRegistrationSecretLabel: registrationName,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				idpRegistrationSecretDataKey: []byte(secretValue),
			},
		})
	}
	if err != nil {
		return fmt.Errorf("reading client secret %q: %w", secretName, err)
	}

	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[idpRegistrationSecretDataKey] = []byte(secretValue)
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[idpRegistrationSecretLabel] = registrationName
	return cl.Update(ctx, existing)
}
