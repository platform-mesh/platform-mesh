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

package releaseutils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOCMConstructor(t *testing.T) {
	constructor, err := ParseOCMConstructor([]byte(`components:
  - name: github.com/platform-mesh/helm-charts/marketplace-ui
    version: ${PM_VERSION}
    resources:
      - name: chart
        type: helmChart
        version: ${PM_VERSION}
  - name: github.com/platform-mesh/marketplace-ui
    version: ${PM_VERSION}
    componentReferences:
      - name: image
        componentName: github.com/platform-mesh/images/marketplace-ui
        version: ${APP_VERSION}
`))
	require.NoError(t, err)
	require.Equal(t, OCMConstructor{
		Components: []OCMComponent{
			{
				Name:    "github.com/platform-mesh/helm-charts/marketplace-ui",
				Version: "${PM_VERSION}",
				Resources: []OCMResource{
					{
						Name:    "chart",
						Version: "${PM_VERSION}",
					},
				},
			},
			{
				Name:    "github.com/platform-mesh/marketplace-ui",
				Version: "${PM_VERSION}",
				References: []OCMReference{
					{
						Name:          "image",
						ComponentName: "github.com/platform-mesh/images/marketplace-ui",
						Version:       "${APP_VERSION}",
					},
				},
			},
		},
	}, constructor, "placeholders are kept as written")

	_, err = ParseOCMConstructor([]byte("components: {"))
	require.Error(t, err)
}
