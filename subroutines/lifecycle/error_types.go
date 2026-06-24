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

package lifecycle

import (
	"context"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Action represents the type of operation a subroutine performs.
type Action string

const (
	ActionProcess    Action = "process"
	ActionFinalize   Action = "finalize"
	ActionInitialize Action = "initialize"
	ActionTerminate  Action = "terminate"
)

// String returns the string representation of the action.
func (a Action) String() string {
	return string(a)
}

// IsFinalize returns true if the action is a finalize or terminate operation.
func (a Action) IsFinalize() bool {
	return a == ActionFinalize || a == ActionTerminate
}

// ErrorReporter is called when a subroutine returns an error.
type ErrorReporter interface {
	Report(ctx context.Context, err error, info ErrorInfo)
}

// ErrorInfo provides context about the subroutine that failed.
type ErrorInfo struct {
	Subroutine string
	Object     ctrlruntimeclient.Object
	Action     Action
}
