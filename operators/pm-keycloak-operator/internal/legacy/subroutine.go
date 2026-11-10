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

package legacy

import (
	"context"
	"fmt"

	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// converter maps a legacy object to its current counterpart.
// writeBack copies the current object's state back into the legacy one.
type converter func(legacy ctrlruntimeclient.Object) (current ctrlruntimeclient.Object, writeBack func(), err error)

// Subroutine runs a keycloak.platform-mesh.io subroutine on a legacy kind.
type Subroutine struct {
	inner   subroutines.Subroutine
	convert converter
	// legacyFinalizers are the finalizer names the old operator set on legacy
	// objects; they are released together with the current ones.
	legacyFinalizers []string
}

var (
	_ subroutines.Processor = &Subroutine{}
	_ subroutines.Finalizer = &Subroutine{}
)

// GetName implements subroutines.Subroutine.
func (s *Subroutine) GetName() string { return s.inner.GetName() }

// Process implements subroutines.Processor.
func (s *Subroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	processor, ok := s.inner.(subroutines.Processor)
	if !ok {
		return subroutines.OK(), nil
	}
	current, writeBack, err := s.convert(obj)
	if err != nil {
		return subroutines.OK(), err
	}
	res, err := processor.Process(ctx, current)
	writeBack()
	return res, err
}

// Finalize implements subroutines.Finalizer.
func (s *Subroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	finalizer, ok := s.inner.(subroutines.Finalizer)
	if !ok {
		return subroutines.OK(), nil
	}
	current, writeBack, err := s.convert(obj)
	if err != nil {
		return subroutines.OK(), err
	}
	res, err := finalizer.Finalize(ctx, current)
	writeBack()
	return res, err
}

// Finalizers implements subroutines.Finalizer.
// Returns the current finalizers plus the legacy names
// so objects finalized by the old operator are released as well.
func (s *Subroutine) Finalizers(obj ctrlruntimeclient.Object) []string {
	finalizer, ok := s.inner.(subroutines.Finalizer)
	if !ok {
		return nil
	}
	current, _, err := s.convert(obj)
	if err != nil {
		return nil
	}
	return append(finalizer.Finalizers(current), s.legacyFinalizers...)
}

func wrongType(obj ctrlruntimeclient.Object, want string) error {
	return fmt.Errorf("legacy subroutine: expected %s, got %T", want, obj)
}
