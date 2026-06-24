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

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/config"
)

func TestOperatorConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantErr   string
	}{
		{name: "default namespace", namespace: "platform-mesh"},
		{name: "custom namespace", namespace: "my-ns"},
		{name: "empty namespace", namespace: "", wantErr: "--namespace must not be empty"},
		{name: "whitespace-only namespace", namespace: "   ", wantErr: "--namespace must not be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewOperatorConfig()
			cfg.Namespace = tc.namespace

			err := cfg.Validate()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
