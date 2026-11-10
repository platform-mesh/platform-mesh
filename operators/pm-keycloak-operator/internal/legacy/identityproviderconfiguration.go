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
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// idpFinalizer is the finalizer security-operator set on legacy IdentityProviderConfigurations.
const idpFinalizer = "core.platform-mesh.io/idp-finalizer"

// IdentityProviderConfiguration adapts sub to the deprecated
// core.platform-mesh.io IdentityProviderConfiguration.
func IdentityProviderConfiguration(sub subroutines.Subroutine) *Subroutine {
	return &Subroutine{
		inner:            sub,
		convert:          convertIdentityProviderConfiguration,
		legacyFinalizers: []string{idpFinalizer},
	}
}

func convertIdentityProviderConfiguration(obj ctrlruntimeclient.Object) (ctrlruntimeclient.Object, func(), error) {
	//nolint:staticcheck // legacy kind served on purpose
	legacy, ok := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
	if !ok {
		return nil, nil, wrongType(obj, "*core.IdentityProviderConfiguration")
	}
	current := identityProviderConfigurationToCurrent(legacy)
	return current, func() { identityProviderConfigurationFromCurrent(current, legacy) }, nil
}

//nolint:staticcheck // legacy kind served on purpose
func identityProviderConfigurationToCurrent(in *pmcorev1alpha1.IdentityProviderConfiguration) *pmkeycloakv1alpha1.IdentityProviderConfiguration {
	out := &pmkeycloakv1alpha1.IdentityProviderConfiguration{
		ObjectMeta: *in.ObjectMeta.DeepCopy(),
		Spec: pmkeycloakv1alpha1.IdentityProviderConfigurationSpec{
			RegistrationAllowed: in.Spec.RegistrationAllowed,
		},
		Status: pmkeycloakv1alpha1.IdentityProviderConfigurationStatus{
			Conditions: in.Status.DeepCopy().Conditions,
		},
	}
	out.TypeMeta.APIVersion = pmkeycloakv1alpha1.SchemeGroupVersion.String()
	out.TypeMeta.Kind = "IdentityProviderConfiguration"
	for _, c := range in.Spec.Clients {
		out.Spec.Clients = append(out.Spec.Clients, pmkeycloakv1alpha1.IdentityProviderClientConfig{
			ClientType:             pmkeycloakv1alpha1.IdentityProviderClientType(c.ClientType),
			ClientName:             c.ClientName,
			RedirectURIs:           append([]string(nil), c.RedirectURIs...),
			PostLogoutRedirectURIs: append([]string(nil), c.PostLogoutRedirectURIs...),
			SecretRef:              c.SecretRef,
		})
	}
	if in.Status.ManagedClients != nil {
		out.Status.ManagedClients = make(map[string]pmkeycloakv1alpha1.ManagedClient, len(in.Status.ManagedClients))
		for name, mc := range in.Status.ManagedClients {
			out.Status.ManagedClients[name] = pmkeycloakv1alpha1.ManagedClient{
				ClientID:              mc.ClientID,
				RegistrationClientURI: mc.RegistrationClientURI,
				SecretRef:             mc.SecretRef,
			}
		}
	}
	return out
}

// identityProviderConfigurationFromCurrent copies metadata and status back;
// the spec is user-owned and never changed by the subroutines.
//
//nolint:staticcheck // legacy kind served on purpose
func identityProviderConfigurationFromCurrent(in *pmkeycloakv1alpha1.IdentityProviderConfiguration, out *pmcorev1alpha1.IdentityProviderConfiguration) {
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = in.Status.DeepCopy().Conditions
	out.Status.ManagedClients = nil
	if in.Status.ManagedClients != nil {
		out.Status.ManagedClients = make(map[string]pmcorev1alpha1.ManagedClient, len(in.Status.ManagedClients))
		for name, mc := range in.Status.ManagedClients {
			out.Status.ManagedClients[name] = pmcorev1alpha1.ManagedClient{
				ClientID:              mc.ClientID,
				RegistrationClientURI: mc.RegistrationClientURI,
				SecretRef:             mc.SecretRef,
			}
		}
	}
}
