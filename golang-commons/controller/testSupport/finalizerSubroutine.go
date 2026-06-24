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

package testSupport

import (
	"context"
	"time"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/errors"

	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const SubroutineFinalizer = "finalizer"

type FinalizerSubroutine struct {
	Client       ctrlruntimeclient.Client
	Err          error
	RequeueAfter time.Duration
}

func (c FinalizerSubroutine) Process(_ context.Context, runtimeObj runtimeobject.RuntimeObject) (controllerruntime.Result, errors.OperatorError) {
	instance := runtimeObj.(*TestApiObject)
	instance.Status.Some = "other string"
	return controllerruntime.Result{}, nil
}

func (c FinalizerSubroutine) Finalize(_ context.Context, _ runtimeobject.RuntimeObject) (controllerruntime.Result, errors.OperatorError) {
	if c.Err != nil {
		return controllerruntime.Result{}, errors.NewOperatorError(c.Err, true, true)
	}
	if c.RequeueAfter > 0 {
		return controllerruntime.Result{RequeueAfter: c.RequeueAfter}, nil
	}

	return controllerruntime.Result{}, nil
}

func (c FinalizerSubroutine) GetName() string {
	return "changeStatus"
}

func (c FinalizerSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{
		SubroutineFinalizer,
	}
}
