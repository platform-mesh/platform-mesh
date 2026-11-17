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

package identityprovider

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestDefaultProviderAdmissionIsReadOnlyAndOwnershipAware(t *testing.T) {
	tests := []struct {
		name                                string
		operation                           admissionv1.Operation
		owner                               bool
		status                              int
		metadataOnly, reserved, wantAllowed bool
		wantLookups                         int
	}{
		{name: "available name", operation: admissionv1.Create, status: http.StatusNotFound, wantAllowed: true, wantLookups: 1},
		{name: "existing foreign realm", operation: admissionv1.Create, status: http.StatusOK, wantLookups: 1},
		{name: "owned realm cannot be recreated through create", operation: admissionv1.Create, status: http.StatusOK, owner: true, wantLookups: 1},
		{name: "owned realm can be updated", operation: admissionv1.Update, status: http.StatusOK, owner: true, wantAllowed: true, wantLookups: 1},
		{name: "foreign realm cannot be updated", operation: admissionv1.Update, status: http.StatusOK, wantLookups: 1},
		{name: "provider unavailable", operation: admissionv1.Create, status: http.StatusServiceUnavailable, wantLookups: 1},
		{name: "reserved realm", operation: admissionv1.Create, reserved: true},
		{name: "finalizer update despite outage", operation: admissionv1.Update, metadataOnly: true, wantAllowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _, tenant, _ := providerFixture(t)
			old := tenant.DeepCopy()
			tenant.Finalizers = append(tenant.Finalizers, "provider.example/test")
			if tt.reserved {
				tenant.Name = "master"
			}
			if tt.operation == admissionv1.Update && !tt.metadataOnly {
				tenant.Spec.Clients[0].RedirectURIs = []string{"https://portal.example/updated"}
			}
			lookups := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if req.URL.Path == "/realms/master/protocol/openid-connect/token" {
					require.Equal(t, http.MethodPost, req.Method)
					_, err := w.Write([]byte(`{"access_token":"fixture-token","token_type":"Bearer","expires_in":300}`))
					require.NoError(t, err)
					return
				}
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/admin/realms/org-a", req.URL.Path)
				lookups++
				w.WriteHeader(tt.status)
				if tt.status == http.StatusOK {
					owner := "foreign"
					if tt.owner {
						owner = engine.Cluster.String() + "/" + string(tenant.UID)
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"attributes": map[string]string{ownerAttribute: owner}}))
				}
			}))
			defer server.Close()
			classes, private, class, configuration := configurationFixture(t)
			require.NoError(t, pmidentityv1alpha1.AddToScheme(classes.Scheme()))
			class.Spec.ControllerName = directory.KeycloakControllerName
			class.Spec.AuthenticationPolicy.IssuerMode = pmidentityv1alpha1.IssuerModeDedicated
			require.NoError(t, classes.Create(t.Context(), class))
			require.NoError(t, classes.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(configuration), configuration))
			configuration.Spec.AdminURL, configuration.Spec.PublicURL = server.URL, server.URL
			configuration.Spec.CASecretRef = &corev1.LocalObjectReference{Name: "provider-ca"}
			require.NoError(t, classes.Update(t.Context(), configuration))
			require.NoError(t, private.Create(t.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "provider-ca", Namespace: "private"},
				Data: map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{
					Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
				})},
			}))
			handler := &AdmissionHandler{Classes: classes, Private: private, ClassName: "default", RuntimeNamespace: "private", Timeout: time.Second}
			raw, err := json.Marshal(tenant)
			require.NoError(t, err)
			previous, err := json.Marshal(old)
			require.NoError(t, err)
			response := handler.Handle(t.Context(), webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				UID: "request-uid", Operation: tt.operation,
				Kind:      metav1.GroupVersionKind{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Kind: "IdentityTenant"},
				Resource:  metav1.GroupVersionResource{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Resource: "identitytenants"},
				Object:    runtime.RawExtension{Raw: raw},
				OldObject: runtime.RawExtension{Raw: previous},
			}})
			require.Equal(t, tt.wantAllowed, response.Allowed, "%+v", response.Result)
			require.Equal(t, tt.wantLookups, lookups)
		})
	}
}

func TestDefaultProviderInvitationAdmissionAndLifecycle(t *testing.T) {
	engine, class, invitation, _ := invitationFixture(t)
	handler := &AdmissionHandler{
		Classes:   fake.NewClientBuilder().WithScheme(engine.Requests.Scheme()).WithObjects(class).Build(),
		ClassName: class.Name, Timeout: time.Second,
	}
	raw, err := json.Marshal(invitation)
	require.NoError(t, err)
	req := webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID: "request-uid", Operation: admissionv1.Create,
		Kind:     metav1.GroupVersionKind{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Kind: "Invitation"},
		Resource: metav1.GroupVersionResource{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Resource: "invitations"},
		Object:   runtime.RawExtension{Raw: raw},
	}}
	require.True(t, handler.Handle(t.Context(), req).Allowed)
	req.Operation, req.OldObject = admissionv1.Update, req.Object
	invitation.Spec.Email = "changed@example.org"
	req.Object.Raw, err = json.Marshal(invitation)
	require.NoError(t, err)
	require.False(t, handler.Handle(t.Context(), req).Allowed)
	handler.Classes = nil
	req.Object = req.OldObject
	require.True(t, handler.Handle(t.Context(), req).Allowed)
	req.Object.Raw = []byte("null")
	require.False(t, handler.Handle(t.Context(), req).Allowed)
	req.Operation = admissionv1.Delete
	require.True(t, handler.Handle(t.Context(), req).Allowed)
}

func TestDefaultProviderAdmissionRejectsNullTenantUpdates(t *testing.T) {
	handler := &AdmissionHandler{}
	response := handler.Handle(t.Context(), webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID: "request-uid", Operation: admissionv1.Update,
		Kind:      metav1.GroupVersionKind{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Kind: "IdentityTenant"},
		Resource:  metav1.GroupVersionResource{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Resource: "identitytenants"},
		Object:    runtime.RawExtension{Raw: []byte("null")},
		OldObject: runtime.RawExtension{Raw: []byte("null")},
	}})
	require.False(t, response.Allowed)
}
