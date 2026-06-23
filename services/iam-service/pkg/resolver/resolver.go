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

package resolver

import (
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/iam-service/pkg/resolver/api"
)

//go:generate go run github.com/99designs/gqlgen@v0.17.81 generate

type Resolver struct {
	svc    api.ResolverService
	logger *logger.Logger
}

func New(svc api.ResolverService, logger *logger.Logger) *Resolver {
	return &Resolver{
		svc:    svc,
		logger: logger,
	}
}
