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
)

// Condition types on AccessGrant.
const (
	// AccessGrantConditionValid indicates whether local state is valid.
	AccessGrantConditionValid = "Valid"

	// AccessGrantConditionHandshakeCompleted indicates the state
	// of the handshake towards the remote side.
	AccessGrantConditionHandshakeCompleted = "HandshakeCompleted"
)

// Reasons for the Accepted condition.
const (
	// AccessGrantAcceptedReason is set when the spec validated and the
	// mirror Secret and operator config resolved; the operator will
	// attempt the /access dial.
	AccessGrantAcceptedReason = "Accepted"

	// AccessGrantAccessTokenSecretMissingReason is set when the Secret
	// named by spec.accessTokenSecretRef is missing or lacks the
	// expected key.
	AccessGrantAccessTokenSecretMissingReason = "AccessTokenSecretMissing"

	// AccessGrantInvalidAccessTokenReason is set when the Secret's
	// token value does not parse as a valid access token.
	AccessGrantInvalidAccessTokenReason = "InvalidAccessToken"
)

// Reasons for the HandshakeCompleted condition.
const (
	// AccessGrantHandshakeSucceededReason is set on the Connected
	// condition after a successful /access 200.
	AccessGrantHandshakeSucceededReason = "HandshakeSucceeded"

	// AccessGrantAccessURLUnreachableReason is set when access-operator
	// cannot dial the access SDK (TLS, DNS, network) or /access
	// returned an unexpected status.
	AccessGrantAccessURLUnreachableReason = "AccessURLUnreachable"

	// AccessGrantSATokenPendingReason indicates the k8s SA-token
	// controller has not yet populated the SA-token Secret.
	AccessGrantSATokenPendingReason = "SATokenPending"

	// AccessGrantUnsupportedProtocolVersionReason is set when the
	// /versions probe returned a list of handshake versions that
	// share no overlap with the versions this operator supports.
	AccessGrantUnsupportedProtocolVersionReason = "UnsupportedProtocolVersion"
)

// AccessTokenSecretRef references a Secret in the same namespace as the
// AccessGrant containing the pma1 access token.
type AccessTokenSecretRef struct {
	// name is the name of the Secret.
	// +required
	Name string `json:"name"`

	// key is the key within the Secret data holding the access token.
	// Defaults to "accessToken".
	// +kubebuilder:default=accessToken
	// +optional
	Key string `json:"key,omitempty"`
}

type AccessGrantSpec struct {
	// saRef references the ServiceAccount, in the same namespace as this
	// AccessGrant. The JWT token for this ServiceAccount is delivered to
	// the user-side cluster in the /access request body.
	// +required
	SARef corev1.LocalObjectReference `json:"saRef"`

	// accessURL is the user-side access SDK's /access endpoint.
	// +required
	AccessURL string `json:"accessURL"`

	// accessTokenSecretRef references a Secret in the same namespace as
	// the AccessGrant containing the access token string.
	// +required
	AccessTokenSecretRef AccessTokenSecretRef `json:"accessTokenSecretRef"`

	// caBundle is an optional PEM-encoded CA bundle used to verify the
	// access SDK's /access TLS server certificate. When empty, system
	// CAs are used.
	// +optional
	CABundle string `json:"caBundle,omitempty"`
}

// AccessGrantStatus defines the observed state of AccessGrant.
type AccessGrantStatus struct {
	// conditions represent the current state of the AccessGrant.
	//
	// Known condition types are:
	//   - "Accepted": spec validated and local state resolved.
	//   - "Connected": state of the /access connection to the user side.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last generation reconciled by
	// access-operator.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:JSONPath=".status.conditions[?(@.type=='Connected')].status",name="Connected",type=string
// +kubebuilder:printcolumn:JSONPath=".status.conditions[?(@.type=='Connected')].reason",name="Reason",type=string,priority=1
// +kubebuilder:printcolumn:JSONPath=".spec.accessURL",name="AccessURL",type=string,priority=1
// +kubebuilder:printcolumn:JSONPath=".metadata.creationTimestamp",name="Age",type=date

// AccessGrant is the PM-side view of a remote-access connection to a
// Provider's user-side cluster. It is created in the Provider Workspace
// by the provider reconciler on a successful ProviderAccessRequest and
// consumed by access-operator, which runs the /access handshake and
// marks the grant Connected.
//
// AccessGrant is torn down with the Provider Workspace on Provider
// deletion.
type AccessGrant struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of AccessGrant.
	// +required
	Spec AccessGrantSpec `json:"spec"`

	// status defines the observed state of AccessGrant.
	// +optional
	Status AccessGrantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessGrantList contains a list of AccessGrant.
type AccessGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessGrant `json:"items"`
}

func (a *AccessGrant) GetConditions() []metav1.Condition {
	return a.Status.Conditions
}

func (a *AccessGrant) SetConditions(conditions []metav1.Condition) {
	a.Status.Conditions = conditions
}
