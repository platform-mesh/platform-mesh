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

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Subroutine is the base interface that all subroutines must implement.
type Subroutine interface {
	GetName() string
}

// Processor handles the main reconciliation logic for a subroutine.
type Processor interface {
	Subroutine
	Process(ctx context.Context, obj ctrlruntimeclient.Object) (Result, error)
}

// Finalizer handles cleanup when an object is being deleted.
type Finalizer interface {
	Subroutine
	Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (Result, error)
	Finalizers(obj ctrlruntimeclient.Object) []string
}

// Initializer handles one-time initialization when an initializer marker is present in status.
type Initializer interface {
	Subroutine
	Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (Result, error)
}

// Terminator handles ordered teardown when a terminator marker is present in status.
type Terminator interface {
	Subroutine
	Terminate(ctx context.Context, obj ctrlruntimeclient.Object) (Result, error)
}
