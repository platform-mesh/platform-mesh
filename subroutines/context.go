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
	"errors"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type contextKey struct{}

// WithClient stores a client.Client in the context.
func WithClient(ctx context.Context, cl ctrlruntimeclient.Client) context.Context {
	return context.WithValue(ctx, contextKey{}, cl)
}

// ClientFromContext retrieves the client.Client from the context.
func ClientFromContext(ctx context.Context) (ctrlruntimeclient.Client, error) {
	cl, ok := ctx.Value(contextKey{}).(ctrlruntimeclient.Client)
	if !ok || cl == nil {
		return nil, errors.New("no client in context")
	}
	return cl, nil
}

// MustClientFromContext retrieves the client.Client from the context.
// It panics if no client is stored in the context.
func MustClientFromContext(ctx context.Context) ctrlruntimeclient.Client {
	cl, err := ClientFromContext(ctx)
	if err != nil {
		panic(err)
	}
	return cl
}
