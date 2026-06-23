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

package transformer

import (
	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

type UserTransformer struct {
	serviceCfg *config.JWTConfig
}

func NewUserTransformer(serviceCfg *config.JWTConfig) *UserTransformer {
	return &UserTransformer{
		serviceCfg: serviceCfg,
	}
}

func (t *UserTransformer) Transform(user *graph.User) *graph.User {
	if user == nil {
		return nil
	}

	switch t.serviceCfg.UserIDClaim {
	case "email":
		user.UserID = user.Email
	default:
		// no-op
	}

	return user
}
