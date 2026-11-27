/*
Copyright 2026 The kcp Authors.

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

// The OpenAPI model names of the types of this API. The API server keys the
// definitions by them, and only a definition it can find by the name of a
// registered kind gets the x-kubernetes-group-version-kind extension that
// clients reading the OpenAPI document (kubectl, the GraphQL gateway) find
// the resource by.

// OpenAPIModelName returns the OpenAPI model name for this type.
func (SelfClusterAccessReview) OpenAPIModelName() string {
	return "io.platform-mesh.access.v1alpha1.SelfClusterAccessReview"
}

// OpenAPIModelName returns the OpenAPI model name for this type.
func (SelfClusterAccessReviewList) OpenAPIModelName() string {
	return "io.platform-mesh.access.v1alpha1.SelfClusterAccessReviewList"
}

// OpenAPIModelName returns the OpenAPI model name for this type.
func (SelfClusterAccessReviewStatus) OpenAPIModelName() string {
	return "io.platform-mesh.access.v1alpha1.SelfClusterAccessReviewStatus"
}

// OpenAPIModelName returns the OpenAPI model name for this type.
func (AccessEndpointSlice) OpenAPIModelName() string {
	return "io.platform-mesh.access.v1alpha1.AccessEndpointSlice"
}

// OpenAPIModelName returns the OpenAPI model name for this type.
func (Account) OpenAPIModelName() string {
	return "io.platform-mesh.access.v1alpha1.Account"
}
