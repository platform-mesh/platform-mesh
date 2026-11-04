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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Conditions for the AccessIdentity type.
const (
	// AccessIdentityReady is the condition type reported on
	// AccessIdentity.status.conditions.
	AccessIdentityReady = "Ready"
)

// Reasons for the AccessIdentityReady condition.
const (
	// AccessIdentityReadyReason: the access token has been minted and the
	// current state of the status reflects it.
	AccessIdentityReadyReason = "TokenReady"

	// AccessIdentityNotReadyAwaitingSecretReason: the Secret holding the access token
	// is present but missing or malformed; the controller is waiting
	// for it to be rewritten correctly.
	AccessIdentityNotReadyAwaitingSecretReason = "AwaitingSecretWrite"
)

const (
	// AccessTokenSecretKey is the Secret data key under which
	// the access token is stored.
	AccessTokenSecretKey = "accessToken"

	// TokenIDLabelKey is name of the label to note down the associated
	// token ID (e.g. on the managed kubeconfig Secret).
	TokenIDLabelKey = "access-sdk.platform-mesh.io/token-id"
)

// AccessIdentitySpec defines the desired state of an AccessIdentity.
type AccessIdentitySpec struct{}

// AccessIdentityStatus defines the observed state of an AccessIdentity.
type AccessIdentityStatus struct {
	// tokenID is the public portion of the access token minted for this
	// identity. The full token lives in a Secret.
	// +optional
	TokenID string `json:"tokenID,omitempty"`

	// conditions represent the current state of the AccessIdentity.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last generation reconciled by the
	// access SDK.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:JSONPath=".status.tokenID",name="TokenID",type=string,priority=1
// +kubebuilder:printcolumn:JSONPath=".metadata.creationTimestamp",name="Age",type=date

// AccessIdentity is a standing authorization on the user-side cluster for
// a PM instance to deliver kubeconfigs via /access. On creation, the
// access SDK mints an access token (tokenID + HMAC secret), writes the
// full token into a Secret, and starts serving /access calls
// authenticated against that identity's HMAC secret. One AccessIdentity
// maps to many Access objects over time.
type AccessIdentity struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of AccessIdentity.
	// +required
	Spec AccessIdentitySpec `json:"spec"`

	// status defines the observed state of AccessIdentity.
	// +optional
	Status AccessIdentityStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessIdentityList contains a list of AccessIdentity.
type AccessIdentityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessIdentity `json:"items"`
}

func (a *AccessIdentity) GetConditions() []metav1.Condition {
	return a.Status.Conditions
}

func (a *AccessIdentity) SetConditions(conditions []metav1.Condition) {
	a.Status.Conditions = conditions
}
