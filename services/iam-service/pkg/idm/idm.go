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
// Package idm describes what iam-service needs from an identity provider's
// user directory, independent of the product behind it.
package idm

import (
	"context"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

const (
	// ProviderKeycloak looks users up in Keycloak. This is the default.
	ProviderKeycloak = "keycloak"
	// ProviderNone runs without a user directory: users are known only by the
	// identifier found in tokens and OpenFGA tuples.
	ProviderNone = "none"
)

// UserDirectory is the user directory of the identity provider.
type UserDirectory interface {
	// UserByMail returns the user with the given email address, or nil if the
	// directory does not know such a user.
	UserByMail(ctx context.Context, email string) (*graph.User, error)

	// GetUsers returns all users known for the organization of the request.
	GetUsers(ctx context.Context) ([]*graph.User, error)

	// EnrichUserRoles completes the users of the given role assignments
	// in-place with what the directory knows about them (id, names).
	EnrichUserRoles(ctx context.Context, userRoles []*graph.UserRoles) error
}
