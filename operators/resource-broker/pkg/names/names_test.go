package names_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/resource-broker/pkg/names"
)

func TestHash(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{
			name:  "no parts",
			parts: nil,
			want:  "e3b0c44298fc1c14",
		},
		{
			name:  "single part",
			parts: []string{"root:orgs:provider"},
			want:  names.Hash("root:orgs:provider"),
		},
		{
			name:  "two parts",
			parts: []string{"root:orgs:provider", "example-export"},
			want:  names.Hash("root:orgs:provider", "example-export"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names.Hash(tt.parts...)
			assert.Equal(t, tt.want, got)
			assert.Len(t, got, 16)
			assert.Regexp(t, regexp.MustCompile("^[0-9a-f]{16}$"), got)
		})
	}
}

func TestHashDeterministic(t *testing.T) {
	assert.Equal(t, names.Hash("a", "b"), names.Hash("a", "b"))
}

func TestHashSeparator(t *testing.T) {
	tests := []struct {
		name  string
		left  []string
		right []string
	}{
		{
			name:  "concatenation ambiguity",
			left:  []string{"ab", "c"},
			right: []string{"a", "bc"},
		},
		{
			name:  "empty part matters",
			left:  []string{"a", ""},
			right: []string{"a"},
		},
		{
			name:  "order matters",
			left:  []string{"a", "b"},
			right: []string{"b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEqual(t, names.Hash(tt.left...), names.Hash(tt.right...))
		})
	}
}
