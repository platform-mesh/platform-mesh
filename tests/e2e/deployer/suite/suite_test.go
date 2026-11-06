//go:build e2e

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

package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlug(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
	}{
		"strips the Test prefix":            {in: "TestDistributedClusters", want: "distributedclusters"},
		"replaces the subtest separator":    {in: "TestModule/with_registry", want: "module-with-registry"},
		"trims leading and trailing dashes": {in: "Test_module_", want: "module"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, slug(tc.in))
		})
	}
}
