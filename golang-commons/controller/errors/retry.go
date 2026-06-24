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

package errors

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

func IsRetriable(err error) (bool, ctrl.Result) {
	// This covers ServerTimeout, Timeout and TooManyRequests
	delay, ok := apierrors.SuggestsClientDelay(err)
	if ok {
		return true, ctrl.Result{RequeueAfter: time.Duration(delay) * time.Second}
	}

	if apierrors.IsInternalError(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsConflict(err) {
		return true, ctrl.Result{}
	}

	return false, ctrl.Result{}
}
