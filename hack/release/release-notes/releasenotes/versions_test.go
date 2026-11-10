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

package releasenotes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestThirdPartyRows(t *testing.T) {
	t.Parallel()

	from := map[string]string{
		"kcp/image":        "v0.33.4",
		"traefik/chart":    "42.0.0",
		"openfga/image":    "v1.22.1",
		"cert-manager/old": "v1",
	}
	to := map[string]string{
		"kcp/image":           "v0.33.6",
		"traefik/chart":       "42.0.0",
		"openfga/image":       "v1.22.1",
		"api-syncagent/chart": "0.6.1",
	}
	want := []VersionRow{
		{Name: "api-syncagent/chart", From: Absent, To: "0.6.1", Changed: true},
		{Name: "cert-manager/old", From: "v1", To: removed, Changed: true},
		{Name: "kcp/image", From: "v0.33.4", To: "v0.33.6", Changed: true},
	}
	assert.Equal(t, want, thirdPartyRows(from, to), "sorted, unchanged pins dropped")
	assert.Nil(t, thirdPartyRows(from, from))
}

func TestShippedComponents(t *testing.T) {
	t.Parallel()

	comps := []Component{
		{Name: "account-operator", Path: "operators/account-operator", Image: "account-operator"},
		{Name: "apis", Path: "apis", Library: true},
		{Name: "provider-operator", Path: "operators/provider-operator"},
		{Name: "root", Path: "."},
	}
	assert.Equal(t, []string{"account-operator"}, shippedComponents(comps), "libraries and components without an image are not shipped")
}
