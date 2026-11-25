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
// Package none implements the identity provider integration for installations
// that have no user directory to query, e.g. a plain OIDC issuer without an
// admin API. Users are known only by the identifier that is found in their
// tokens and in OpenFGA tuples, which is the email address.
package none

import (
	"context"
	"sort"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/errors"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/fga/store"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/idm"
	idmmw "go.platform-mesh.io/iam-service/pkg/middleware/idm"
)

const (
	userPrefix       = "user:"
	rolePrefix       = "role:"
	assigneeRelation = "assignee"
	readPageSize     = 100
)

var (
	_ idm.UserDirectory        = (*Directory)(nil)
	_ idmmw.IDMTenantRetriever = TenantRetriever{}
)

// Directory is a idm.UserDirectory that is not backed by an identity provider.
type Directory struct {
	fga    openfgav1.OpenFGAServiceClient
	stores store.StoreHelper
}

// New returns a Directory. The OpenFGA client and store helper are used to
// derive the known users of an organization from its role assignments.
func New(fga openfgav1.OpenFGAServiceClient, stores store.StoreHelper) *Directory {
	return &Directory{fga: fga, stores: stores}
}

// UserByMail returns a user that consists of nothing but the given email
// address. Without a directory every well-formed identifier is taken as an
// existing user, there is nothing to check it against. As a consequence no
// invitation is ever needed for a user: roles are assigned to the identifier
// directly and take effect once somebody logs in with it.
func (d *Directory) UserByMail(_ context.Context, email string) (*graph.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}

	return newUser(email), nil
}

// GetUsers returns the users that can be derived without a directory: every
// user that has a role assigned in the OpenFGA store of the request's
// organization, and the caller.
func (d *Directory) GetUsers(ctx context.Context) ([]*graph.User, error) {
	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kcp user context")
	}

	storeID, err := d.stores.GetStoreID(ctx, d.fga, kctx.OrganizationName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get store ID for organization %s", kctx.OrganizationName)
	}

	emails := map[string]struct{}{}
	if webToken, err := pmcontext.GetWebTokenFromContext(ctx); err == nil && webToken.Mail != "" {
		emails[webToken.Mail] = struct{}{}
	}

	// OpenFGA cannot filter tuples by object type alone, so all tuples of the
	// store are read and filtered here.
	continuationToken := ""
	for {
		res, err := d.fga.Read(ctx, &openfgav1.ReadRequest{
			StoreId:           storeID,
			PageSize:          wrapperspb.Int32(readPageSize),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to read role assignments of organization %s", kctx.OrganizationName)
		}

		for _, tuple := range res.GetTuples() {
			if email, ok := assignee(tuple.GetKey()); ok {
				emails[email] = struct{}{}
			}
		}

		continuationToken = res.GetContinuationToken()
		if continuationToken == "" {
			break
		}
	}

	users := make([]*graph.User, 0, len(emails))
	for email := range emails {
		users = append(users, newUser(email))
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })

	return users, nil
}

// EnrichUserRoles sets the user id of every user to its email address, which
// is the only thing known about a user. Names stay empty.
func (d *Directory) EnrichUserRoles(_ context.Context, userRoles []*graph.UserRoles) error {
	for _, userRole := range userRoles {
		if userRole == nil || userRole.User == nil {
			continue
		}
		if userRole.User.UserID == "" {
			userRole.User.UserID = userRole.User.Email
		}
	}

	return nil
}

// assignee returns the user of a role assignment tuple (user:<email> assignee
// role:<...>). Tuples of another shape, and the wildcard user, are skipped.
func assignee(key *openfgav1.TupleKey) (string, bool) {
	if key.GetRelation() != assigneeRelation || !strings.HasPrefix(key.GetObject(), rolePrefix) {
		return "", false
	}

	email, ok := strings.CutPrefix(key.GetUser(), userPrefix)
	if !ok || email == "" || email == "*" || strings.Contains(email, "#") {
		return "", false
	}

	return email, true
}

func newUser(email string) *graph.User {
	return &graph.User{UserID: email, Email: email}
}

// TenantRetriever is the idm.IDMTenantRetriever for installations without an
// identity provider directory. There are no tenants (realms) to tell apart, so
// any issuer is accepted and the tenant is empty.
type TenantRetriever struct{}

// GetIDMTenant implements idm.IDMTenantRetriever.
func (TenantRetriever) GetIDMTenant(string) (string, error) {
	return "", nil
}
