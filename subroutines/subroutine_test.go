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

// Compile-time interface assertions.
type testSubroutine struct{}

func (t *testSubroutine) GetName() string { return "test" }
func (t *testSubroutine) Process(context.Context, ctrlruntimeclient.Object) (Result, error) {
	return OK(), nil
}
func (t *testSubroutine) Finalize(context.Context, ctrlruntimeclient.Object) (Result, error) {
	return OK(), nil
}
func (t *testSubroutine) Finalizers(ctrlruntimeclient.Object) []string { return nil }
func (t *testSubroutine) Initialize(context.Context, ctrlruntimeclient.Object) (Result, error) {
	return OK(), nil
}
func (t *testSubroutine) Terminate(context.Context, ctrlruntimeclient.Object) (Result, error) {
	return OK(), nil
}

var (
	_ Subroutine  = &testSubroutine{}
	_ Processor   = &testSubroutine{}
	_ Finalizer   = &testSubroutine{}
	_ Initializer = &testSubroutine{}
	_ Terminator  = &testSubroutine{}
)
