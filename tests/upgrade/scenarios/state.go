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

package scenarios

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// What a scenario remembers from Seed for Validate: a Secret in the cluster
// Platform Mesh is installed into. It is kept there, and not in the test, so
// that it is from before the upgrade whatever runs the validation, also a
// later run on a kept cluster; and outside of kcp, so that it does not share
// the fate of what it describes.

const (
	stateNamespace = metav1.NamespaceDefault
	statePrefix    = "upgrade-test-"
	stateKey       = "state.json"
)

func stateSecrets(ctx context.Context, env Environment) (*kubernetes.Clientset, error) {
	cfg, err := env.Kube(ctx)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// remember keeps data under name unless something is kept under it already:
// what is remembered first, on the release, stays.
func remember(ctx context.Context, env Environment, name string, data map[string]string) error {
	kube, err := stateSecrets(ctx, env)
	if err != nil {
		return err
	}
	// As one value: the keys of data are free text, those of a Secret
	// are not.
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: stateNamespace, Name: statePrefix + name},
		Data:       map[string][]byte{stateKey: encoded},
	}
	_, err = kube.CoreV1().Secrets(stateNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("remembering %s: %w", name, err)
	}
	return nil
}

// recall returns what remember kept under name.
func recall(ctx context.Context, env Environment, name string) (map[string]string, error) {
	kube, err := stateSecrets(ctx, env)
	if err != nil {
		return nil, err
	}
	secret, err := kube.CoreV1().Secrets(stateNamespace).Get(ctx, statePrefix+name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("recalling %s: %w", name, err)
	}
	data := map[string]string{}
	if err := json.Unmarshal(secret.Data[stateKey], &data); err != nil {
		return nil, fmt.Errorf("recalling %s: %w", name, err)
	}
	return data, nil
}

// forget drops what remember kept under name, so that the next Seed
// remembers anew.
func forget(ctx context.Context, env Environment, name string) error {
	kube, err := stateSecrets(ctx, env)
	if err != nil {
		return err
	}
	err = kube.CoreV1().Secrets(stateNamespace).Delete(ctx, statePrefix+name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("forgetting %s: %w", name, err)
	}
	return nil
}
