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

package sentry

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func ShouldBeProcessed(err error) bool {
	if sentryErr, ok := AsSentryError(err); ok {
		err = sentryErr.GetReason()
	}

	return !isTerminatingNSError(err)
}

func isTerminatingNSError(err error) bool {
	status := apierrors.APIStatus(nil)

	if errors.As(err, &status) && status.Status().Details != nil {
		for _, cause := range status.Status().Details.Causes {
			if cause.Type == corev1.NamespaceTerminatingCause {
				return true
			}
		}
	}

	return false
}
