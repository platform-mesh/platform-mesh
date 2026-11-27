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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	kcpoperatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
)

// BundleKind is the kind of the Bundle.
const BundleKind = "Bundle"

// Bundle is the kcp PKI of one namespace. The cert-manager objects are unstructured, cert-manager
// has no separate API module and this module would get all of its dependencies.
type Bundle struct {
	metav1.TypeMeta `json:",inline"`

	// Issuers are cert-manager Issuers.
	Issuers []unstructured.Unstructured `json:"issuers"`
	// Certificates are cert-manager Certificates.
	Certificates []unstructured.Unstructured `json:"certificates"`
	// Secrets are the secrets of the Certificates.
	Secrets []corev1.Secret `json:"secrets"`
	// Kubeconfigs are the kcp-operator Kubeconfig objects. kcp-operator writes their kubeconfig
	// secrets again from the restored client certificates.
	Kubeconfigs []kcpoperatorv1alpha1.Kubeconfig `json:"kubeconfigs,omitempty"`
}

// NewBundle returns a Bundle with apiVersion and kind set.
func NewBundle() *Bundle {
	return &Bundle{TypeMeta: metav1.TypeMeta{APIVersion: SchemeGroupVersion.String(), Kind: BundleKind}}
}
