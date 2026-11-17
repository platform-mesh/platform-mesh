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

// Package idpdirectory defines the provider-neutral, tenant-scoped user directory protocol.
package idpdirectory

import (
	"context"
	"errors"
	"fmt"
	"net/mail"

	"go.platform-mesh.io/golang-commons/identity"
)

const (
	QueryPath    = "/identity-directory/v1/query"
	MaxBatchSize = 100
	MaxPageSize  = 100
)

var (
	ErrNotFound     = errors.New("identity not found")
	ErrAmbiguous    = errors.New("identity lookup is ambiguous")
	ErrUnauthorized = errors.New("directory access denied")
	ErrUnavailable  = errors.New("identity directory unavailable")
)

type Operation string

const (
	List            Operation = "List"
	LookupPrincipal Operation = "LookupPrincipal"
	LookupEmail     Operation = "LookupEmail"
	BatchLookup     Operation = "BatchLookup"
)

// Tenant identifies a KCP object, not a caller-selected provider administration scope.
type Tenant struct {
	LogicalCluster string `json:"logicalCluster"`
	Name           string `json:"name"`
	UID            string `json:"uid"`
}

type Query struct {
	Tenant     Tenant               `json:"tenant"`
	Operation  Operation            `json:"operation"`
	Principals []identity.Principal `json:"principals,omitempty"`
	Email      string               `json:"email,omitempty"`
	PageSize   int                  `json:"pageSize,omitempty"`
	PageToken  string               `json:"pageToken,omitempty"`
}

type User struct {
	Principal     identity.Principal `json:"principal"`
	Email         string             `json:"email,omitempty"`
	EmailVerified bool               `json:"emailVerified"`
	GivenName     string             `json:"givenName,omitempty"`
	FamilyName    string             `json:"familyName,omitempty"`
}

type Result struct {
	Users         []User `json:"users"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// Backend must resolve the tenant's immutable KCP identity before accessing a provider.
// LookupEmail returns ErrAmbiguous rather than choosing between multiple matches.
type Backend interface {
	Query(context.Context, Query) (Result, error)
}

type Authorizer func(ctx context.Context, workloadURI string, tenant Tenant) error

func (q Query) Validate() error {
	if q.Tenant.LogicalCluster == "" || q.Tenant.Name == "" || q.Tenant.UID == "" {
		return errors.New("tenant logicalCluster, name and uid are required")
	}
	if len(q.Tenant.LogicalCluster) > 253 || len(q.Tenant.Name) > 253 || len(q.Tenant.UID) > 253 {
		return errors.New("tenant identity exceeds maximum length")
	}
	if len(q.PageToken) > 4096 {
		return errors.New("page token exceeds maximum length")
	}
	if q.PageSize < 0 || q.PageSize > MaxPageSize {
		return fmt.Errorf("page size must be between 0 and %d", MaxPageSize)
	}
	if len(q.Principals) > MaxBatchSize {
		return fmt.Errorf("batch exceeds %d principals", MaxBatchSize)
	}
	seen := make(map[identity.Principal]struct{}, len(q.Principals))
	for _, principal := range q.Principals {
		if _, err := principal.Username(); err != nil {
			return fmt.Errorf("invalid principal: %w", err)
		}
		if _, ok := seen[principal]; ok {
			return errors.New("duplicate principal")
		}
		seen[principal] = struct{}{}
	}
	switch q.Operation {
	case List:
		if len(q.Principals) != 0 || q.Email != "" {
			return errors.New("list does not accept principal or email filters")
		}
	case LookupPrincipal, BatchLookup:
		if len(q.Principals) == 0 || (q.Operation == LookupPrincipal && len(q.Principals) != 1) {
			return errors.New("principal lookup requires the appropriate number of principals")
		}
		if q.Email != "" || q.PageSize != 0 || q.PageToken != "" {
			return errors.New("principal lookup does not accept email or pagination")
		}
	case LookupEmail:
		address, err := mail.ParseAddress(q.Email)
		if err != nil || address.Address != q.Email {
			return errors.New("email lookup requires an exact mailbox address")
		}
		if len(q.Principals) != 0 || q.PageSize != 0 || q.PageToken != "" {
			return errors.New("email lookup does not accept principals or pagination")
		}
	default:
		return fmt.Errorf("unsupported directory operation %q", q.Operation)
	}
	return nil
}

func (r Result) validate(q Query) error {
	limit := MaxPageSize
	if q.Operation == List && q.PageSize != 0 {
		limit = q.PageSize
	}
	if q.Operation == LookupEmail || q.Operation == LookupPrincipal {
		limit = 1
		if len(r.Users) != 1 {
			return errors.New("single identity lookup must return a user or an explicit error")
		}
	}
	if q.Operation == BatchLookup {
		limit = len(q.Principals)
	}
	if len(r.Users) > limit {
		return errors.New("directory returned too many users")
	}
	if len(r.NextPageToken) > 4096 || (q.Operation != List && r.NextPageToken != "") {
		return errors.New("directory returned invalid pagination")
	}
	seen := make(map[identity.Principal]struct{}, len(r.Users))
	for _, user := range r.Users {
		if _, err := user.Principal.Username(); err != nil {
			return fmt.Errorf("directory returned an invalid principal: %w", err)
		}
		if _, ok := seen[user.Principal]; ok {
			return errors.New("directory returned duplicate principals")
		}
		seen[user.Principal] = struct{}{}
		if q.Operation == LookupEmail && user.Email != q.Email {
			return errors.New("directory returned a different email")
		}
		if q.Operation == LookupPrincipal || q.Operation == BatchLookup {
			if !containsPrincipal(q.Principals, user.Principal) {
				return errors.New("directory returned an unrequested principal")
			}
		}
	}
	return nil
}

func containsPrincipal(principals []identity.Principal, principal identity.Principal) bool {
	for _, candidate := range principals {
		if candidate == principal {
			return true
		}
	}
	return false
}
