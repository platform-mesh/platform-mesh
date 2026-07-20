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

package idp

import (
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/util"
)

// needsOrganizationsEnabled reports whether any upstream provider defines email
// domains. It mirrors the org-creation path in reconcileUpstreamIdentityProvider (which
// keys off email domains alone) so the realm-level Organizations flag can never
// drift from whether organizations are actually created.
func needsOrganizationsEnabled(providers []pmcorev1alpha1.UpstreamIdentityProvider) bool {
	for i := range providers {
		routing := providers[i].EmailDomainRouting
		if routing != nil && len(util.NormalizeEmailDomains(routing.Domains)) > 0 {
			return true
		}
	}
	return false
}
