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

// Package velero manages the Velero backup server lifecycle as an internal
// dependency of the backup-operator. Adopters must NOT install Velero separately;
// the operator owns the Velero Deployment, node-agent DaemonSet, and CRDs.
package velero

import (
	"context"
	"fmt"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	"go.platform-mesh.io/golang-commons/logger"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// bslName is the fixed name of the BackupStorageLocation managed by the operator.
const bslName = "default"

// EnsureBSL creates or updates the Velero BackupStorageLocation in the given namespace
// to match the provided S3 endpoint, bucket, and credentials secret.
// It is called by both capture and restore subroutines before triggering Velero operations.
func EnsureBSL(ctx context.Context, cl ctrlruntimeclient.Client, namespace, endpoint, bucket, credentialsSecret string) error {
	log := logger.LoadLoggerFromContext(ctx)

	// Default region for minio; real AWS deployments should configure via flag.
	region := "minio"

	desired := &velerov1.BackupStorageLocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bslName,
			Namespace: namespace,
		},
		Spec: velerov1.BackupStorageLocationSpec{
			Provider: "aws",
			// Credential points Velero at the user-supplied secret so it can
			// authenticate to minio (or real S3). Velero expects the secret to
			// have a key named "cloud" containing an AWS credentials file.
			Credential: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: credentialsSecret},
				Key:                  "cloud",
			},
			StorageType: velerov1.StorageType{
				ObjectStorage: &velerov1.ObjectStorageLocation{
					Bucket: bucket,
				},
			},
			Config: map[string]string{
				"s3Url":                 endpoint,
				"region":                region,
				"s3ForcePathStyle":      "true",
				"insecureSkipTLSVerify": "true",
			},
		},
	}

	var existing velerov1.BackupStorageLocation
	err := cl.Get(ctx, types.NamespacedName{Name: bslName, Namespace: namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if createErr := cl.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating BackupStorageLocation: %w", createErr)
		}
		log.Info().Str("namespace", namespace).Str("bucket", bucket).Msg("created Velero BackupStorageLocation")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting BackupStorageLocation: %w", err)
	}

	// Only patch if spec changed to avoid spurious audit events on every restart.
	if existing.Spec.Config["s3Url"] == endpoint &&
		existing.Spec.ObjectStorage != nil &&
		existing.Spec.ObjectStorage.Bucket == bucket {
		return nil
	}

	patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if patchErr := cl.Patch(ctx, &existing, patch); patchErr != nil {
		return fmt.Errorf("updating BackupStorageLocation: %w", patchErr)
	}
	log.Info().Str("namespace", namespace).Str("bucket", bucket).Msg("updated Velero BackupStorageLocation")
	return nil
}

// BSLName returns the fixed name used for the managed BackupStorageLocation.
func BSLName() string { return bslName }
