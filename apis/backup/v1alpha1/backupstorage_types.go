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

// BackupStorageSpec describes an S3 compatible bucket where backups are stored.
type BackupStorageSpec struct {
	// Bucket is the name of the bucket.
	// +kubebuilder:validation:MinLength=1
	// +required
	Bucket string `json:"bucket"`

	// Prefix is put in front of every object key of a backup.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// Endpoint of the S3 API, for example http://minio:9000. Empty means AWS S3.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// +kubebuilder:default=us-east-1
	// +optional
	Region string `json:"region,omitempty"`

	// ForcePathStyle uses path style URLs, needed by most S3 compatible servers.
	// +optional
	ForcePathStyle bool `json:"forcePathStyle,omitempty"`

	// CredentialsSecretRef points to a Secret with the keys accessKeyID and secretAccessKey.
	// +required
	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`

	// EncryptionKeySecretRef points to a Secret with the key "key": 32 random bytes.
	// Secrets and certificates are encrypted with it before upload.
	// +required
	EncryptionKeySecretRef corev1.LocalObjectReference `json:"encryptionKeySecretRef"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.bucket`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BackupStorage is the bucket where Backups write their archive.
type BackupStorage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec BackupStorageSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// BackupStorageList contains a list of BackupStorage
type BackupStorageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupStorage `json:"items"`
}
