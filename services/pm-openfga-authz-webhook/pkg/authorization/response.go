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

package authorization

import (
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
)

func Errored(err error) Response {
	return Response{
		SubjectAccessReview: authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed:         false,
				Reason:          err.Error(),
				EvaluationError: err.Error(),
			},
		},
	}
}

func NoOpinion() Response {
	return Response{
		SubjectAccessReview: authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: false,
				Reason:  "NoOpinion",
			},
		},
	}
}

// Aborted returns a response that is neither allowed nor denied,
// but signals the union chain to stop evaluating further handlers.
func Aborted() Response {
	return Response{
		SubjectAccessReview: authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: false,
				Reason:  "NoOpinion",
			},
		},
		Abort: true,
	}
}

func Allowed() Response {
	return Response{
		SubjectAccessReview: authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
				Denied:  false,
			},
		},
	}
}

func Denied() Response {
	return Response{
		SubjectAccessReview: authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: false,
				Denied:  true,
			},
		},
	}
}

// Retry makes the apiserver retry the request after a given duration
func Retry(after time.Duration) Response {
	// note: while setting a SubjectAccessReview won't have any effect because
	// the webhook is implemented in a way where it will not write
	// SubjectAccessReview to the HTTP response's body if RetryAfter is set, we
	// are not supposed to set one anyway because its presence in the HTTP
	// response body would take priority over the Retry-After header and render
	// it ineffective. So don't try to set one here in any case.
	return Response{
		RetryAfter: after,
	}
}
