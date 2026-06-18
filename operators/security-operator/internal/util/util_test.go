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
