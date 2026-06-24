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

package subroutines

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestClientContext(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		ctx := WithClient(context.Background(), cl)
		got, err := ClientFromContext(ctx)
		require.NoError(t, err)
		assert.Equal(t, cl, got)
	})

	t.Run("error on empty context", func(t *testing.T) {
		_, err := ClientFromContext(context.Background())
		assert.Error(t, err)
	})

	t.Run("MustClientFromContext panics on empty", func(t *testing.T) {
		assert.Panics(t, func() {
			MustClientFromContext(context.Background())
		})
	})

	t.Run("MustClientFromContext succeeds", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		ctx := WithClient(context.Background(), cl)
		assert.NotPanics(t, func() {
			got := MustClientFromContext(ctx)
			assert.Equal(t, cl, got)
		})
	})
}
