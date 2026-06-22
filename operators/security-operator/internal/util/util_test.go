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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCapGroupToRelationLength(t *testing.T) {
	tests := []struct {
		name      string
		gvr       schema.GroupVersionResource
		maxLength int
		want      string
	}{
		{
			name:      "group fits within max length returns group unchanged",
			gvr:       schema.GroupVersionResource{Group: "mygroup", Version: "v1", Resource: "things"},
			maxLength: 100,
			want:      "mygroup",
		},
		{
			name:      "empty group defaults to core",
			gvr:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			maxLength: 100,
			want:      "core",
		},
		{
			name:      "long group is trimmed from the front to fit max length",
			gvr:       schema.GroupVersionResource{Group: "long-group-name", Version: "v1", Resource: "resource"},
			maxLength: 20,
			want:      "name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CapGroupToRelationLength(tt.gvr, tt.maxLength))
		})
	}
}
