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

package builder

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/controllerruntime"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/multicluster"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"
)

type Builder struct {
	operatorName            string
	controllerName          string
	withConditionManagement bool
	withSpreadingReconciles bool
	withReadOnly            bool
	terminator              string
	initializer             string
	rateLimiterOptions      *[]ratelimiter.Option
	subroutines             []subroutine.Subroutine
	log                     *logger.Logger
}

func NewBuilder(operatorName, controllerName string, subroutines []subroutine.Subroutine, log *logger.Logger) *Builder {
	return &Builder{
		operatorName:            operatorName,
		controllerName:          controllerName,
		log:                     log,
		withConditionManagement: false,
		subroutines:             subroutines,
	}
}

func (b *Builder) WithConditionManagement() *Builder {
	b.withConditionManagement = true
	return b
}

func (b *Builder) WithSpreadingReconciles() *Builder {
	b.withSpreadingReconciles = true
	return b
}

func (b *Builder) WithReadOnly() *Builder {
	b.withReadOnly = true
	return b
}

func (b *Builder) WithStaticThenExponentialRateLimiter(opts ...ratelimiter.Option) *Builder {
	b.rateLimiterOptions = &opts
	return b
}

func (b *Builder) WithTerminator(terminator string) *Builder {
	b.terminator = terminator
	return b
}

func (b *Builder) WithInitializer(initializer string) *Builder {
	b.initializer = initializer
	return b
}

func (b *Builder) BuildControllerRuntime(cl client.Client) *controllerruntime.LifecycleManager {
	lm := controllerruntime.NewLifecycleManager(b.subroutines, b.operatorName, b.controllerName, cl, b.log)
	if b.withConditionManagement {
		lm.WithConditionManagement()
	}
	if b.withSpreadingReconciles {
		lm.WithSpreadingReconciles()
	}
	if b.withReadOnly {
		lm.WithReadOnly()
	}
	if b.rateLimiterOptions != nil {
		lm.WithStaticThenExponentialRateLimiter((*b.rateLimiterOptions)...)
	}
	return lm
}

func (b *Builder) BuildMultiCluster(mgr mcmanager.Manager) *multicluster.LifecycleManager {
	lm := multicluster.NewLifecycleManager(b.subroutines, b.operatorName, b.controllerName, mgr, b.log)
	if b.withConditionManagement {
		lm.WithConditionManagement()
	}
	if b.withSpreadingReconciles {
		lm.WithSpreadingReconciles()
	}
	if b.withReadOnly {
		lm.WithReadOnly()
	}
	if b.rateLimiterOptions != nil {
		lm.WithStaticThenExponentialRateLimiter((*b.rateLimiterOptions)...)
	}
	if b.terminator != "" {
		lm.WithTerminator(b.terminator)
	}
	if b.initializer != "" {
		lm.WithInitializer(b.initializer)
	}
	return lm
}
