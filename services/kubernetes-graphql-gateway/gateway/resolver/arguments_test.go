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

package resolver_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
)

func TestGetStrArg(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]any
		error error
	}{
		{
			name: "invalid_type_ERROR",
			args: map[string]any{
				"arg1": false,
			},
			error: errors.New("invalid type for argument: arg1"),
		},
		{
			name: "empty_value_ERROR",
			args: map[string]any{
				"arg1": "",
			},
			error: errors.New("empty value for argument: arg1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.GetArg[string](tt.args, "arg1", true)
			if tt.error != nil {
				assert.EqualError(t, err, tt.error.Error())
			}
		})
	}
}

func TestGetBoolArg(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]any
		error error
	}{
		{
			name:  "missing_required_argument_ERROR",
			args:  map[string]any{},
			error: errors.New("missing required argument: arg1"),
		},
		{
			name: "invalid_type_ERROR",
			args: map[string]any{
				"arg1": "MUST_BE_BOOL",
			},
			error: errors.New("invalid type for argument: arg1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.GetArg[bool](tt.args, "arg1", true)
			if tt.error != nil {
				assert.EqualError(t, err, tt.error.Error())
			}
		})
	}
}
