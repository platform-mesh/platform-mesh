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

package fga

import (
	"context"
	"errors"
	"reflect"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/search-service/internal/config"
	"go.platform-mesh.io/search-service/internal/service/search"
)

type fakeClient struct {
	listObjectsRequest *openfgav1.ListObjectsRequest
	listObjectsResult  *openfgav1.ListObjectsResponse
	listObjectsErr     error
}

func (f *fakeClient) BatchCheck(context.Context, *openfgav1.BatchCheckRequest, ...grpc.CallOption) (*openfgav1.BatchCheckResponse, error) {
	return nil, nil
}

func (f *fakeClient) ListObjects(_ context.Context, req *openfgav1.ListObjectsRequest, _ ...grpc.CallOption) (*openfgav1.ListObjectsResponse, error) {
	f.listObjectsRequest = req
	return f.listObjectsResult, f.listObjectsErr
}

func (f *fakeClient) ListStores(context.Context, *openfgav1.ListStoresRequest, ...grpc.CallOption) (*openfgav1.ListStoresResponse, error) {
	return &openfgav1.ListStoresResponse{Stores: []*openfgav1.Store{{Id: "store-acme", Name: "acme"}}}, nil
}

func TestAuthorizerListAccessibleAccounts(t *testing.T) {
	client := &fakeClient{listObjectsResult: &openfgav1.ListObjectsResponse{Objects: []string{
		"custom_account:cluster/team-a",
		"custom_account:cluster/team-a",
		" custom_account:cluster/team-b ",
		"",
	}}}
	cfg := config.NewServiceConfig()
	cfg.OpenFGA.ObjectType = "custom_account"
	cfg.OpenFGA.DefaultRole = "viewer"

	accounts, err := NewAuthorizer(client, *cfg).ListAccessibleAccounts(context.Background(), "acme", "john.doe@example.com", "")
	if err != nil {
		t.Fatalf("ListAccessibleAccounts returned error: %v", err)
	}
	if !reflect.DeepEqual(accounts, []string{
		"custom_account:cluster/team-a",
		"custom_account:cluster/team-b",
	}) {
		t.Fatalf("unexpected accounts: %v", accounts)
	}
	if got := client.listObjectsRequest; got.GetStoreId() != "store-acme" || got.GetType() != "custom_account" || got.GetRelation() != "viewer" || got.GetUser() != "user:john.doe@example.com" {
		t.Fatalf("unexpected ListObjects request: %+v", got)
	}
}

func TestAuthorizerListAccessibleAccountsUsesRequestedRelation(t *testing.T) {
	client := &fakeClient{listObjectsResult: &openfgav1.ListObjectsResponse{}}
	cfg := config.NewServiceConfig()
	cfg.OpenFGA.DefaultRole = "member"

	_, err := NewAuthorizer(client, *cfg).ListAccessibleAccounts(context.Background(), "acme", "john.doe@example.com", " owner ")
	if err != nil {
		t.Fatalf("ListAccessibleAccounts returned error: %v", err)
	}
	if got := client.listObjectsRequest.GetRelation(); got != "owner" {
		t.Fatalf("expected requested relation owner, got %q", got)
	}
}

func TestAuthorizerListAccessibleAccountsClassifiesMissingRelation(t *testing.T) {
	client := &fakeClient{listObjectsErr: status.Error(
		codes.Code(openfgav1.ErrorCode_relation_not_found),
		"relation 'account:owner' not found",
	)}
	cfg := config.NewServiceConfig()

	_, err := NewAuthorizer(client, *cfg).ListAccessibleAccounts(context.Background(), "acme", "john.doe@example.com", "owner")
	if !errors.Is(err, search.ErrFGARelationNotFound) {
		t.Fatalf("expected ErrFGARelationNotFound, got %v", err)
	}
}

func TestIsRelationNotFoundOnlyMatchesRelationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "relation not found",
			err:  status.Error(codes.Code(openfgav1.ErrorCode_relation_not_found), "relation not found"),
			want: true,
		},
		{
			name: "other OpenFGA validation error",
			err:  status.Error(codes.Code(openfgav1.ErrorCode_type_not_found), "type not found"),
		},
		{name: "ordinary invalid argument", err: status.Error(codes.InvalidArgument, "invalid")},
		{name: "plain error", err: errors.New("failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRelationNotFound(tt.err); got != tt.want {
				t.Fatalf("isRelationNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildBatchCheckItemResourceObjectFormat(t *testing.T) {
	hit := search.OpenSearchHit{Source: map[string]any{
		"fga_object": "core_platform-mesh_io_component:cluster1/ns1/comp1",
		"permissions": []any{
			map[string]any{
				"user":     "core_platform-mesh_io_account:sap/workspaces",
				"relation": "parent",
				"object":   "core_platform_mesh_io_namespace:cluster1/ns1",
			},
		},
	}}

	testlogger := testlogger.New().HideLogOutput()

	item, missing := buildBatchCheckItem(testlogger.Logger, "alice@example.com", "get", 0, hit)
	if missing {
		t.Fatalf("expected context to be valid")
	}
	if item.TupleKey.Relation != "get" {
		t.Fatalf("unexpected relation: %s", item.TupleKey.Relation)
	}
	expected := "core_platform-mesh_io_component:cluster1/ns1/comp1"
	if item.TupleKey.Object != expected {
		t.Fatalf("unexpected object: %s", item.TupleKey.Object)
	}
	if len(item.ContextualTuples.TupleKeys) == 0 {
		t.Fatalf("expected contextual tuples")
	}
}

func TestBuildBatchCheckItemDropsMissingAuthContext(t *testing.T) {
	hit := search.OpenSearchHit{Source: map[string]any{
		// missing fga_object
		"kind": "Component",
	}}

	testlogger := testlogger.New().HideLogOutput()

	_, missing := buildBatchCheckItem(testlogger.Logger, "alice@example.com", "get", 0, hit)
	if !missing {
		t.Fatalf("expected missing auth context")
	}
}

func TestChunkRanges(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		chunkSize int
		want      [][2]int
	}{
		{
			name:      "empty",
			total:     0,
			chunkSize: 100,
			want:      nil,
		},
		{
			name:      "single chunk exact",
			total:     100,
			chunkSize: 100,
			want:      [][2]int{{0, 100}},
		},
		{
			name:      "single chunk partial",
			total:     50,
			chunkSize: 100,
			want:      [][2]int{{0, 50}},
		},
		{
			name:      "multiple chunks",
			total:     250,
			chunkSize: 100,
			want:      [][2]int{{0, 100}, {100, 200}, {200, 250}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkRanges(tt.total, tt.chunkSize)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("chunkRanges(%d, %d) = %#v, want %#v", tt.total, tt.chunkSize, got, tt.want)
			}
		})
	}
}

func TestFormatUser(t *testing.T) {
	tests := []struct {
		user string
		want string
	}{
		{"alice", "alice"},
		{"john.doe@example.com", "john.doe@example.com"},
		{"system:serviceaccount:default:auth", "system.serviceaccount.default.auth"},
	}
	for _, tt := range tests {
		if got := formatUser(tt.user); got != tt.want {
			t.Errorf("formatUser(%q) = %q, want %q", tt.user, got, tt.want)
		}
	}
}

func TestBuildAuthorizationContextFromDocumentMetadata(t *testing.T) {
	source := map[string]any{
		"fga_object": "core_platform-mesh_io_component:cluster-x/ns-y/comp-z",
		"permissions": []any{
			map[string]any{
				"user":     "core_platform_mesh_io_account:sap/workspaces",
				"relation": "parent",
				"object":   "core_platform_mesh_io_namespace:cluster-x/ns-y",
			},
		},
	}

	ctx, ok := buildAuthorizationContext(nil, source)
	if !ok {
		t.Fatalf("expected valid context")
	}

	if ctx.object != source["fga_object"] {
		t.Errorf("expected object %q, got %q", source["fga_object"], ctx.object)
	}
}

func TestBuildAuthorizationContextFromDocumentMetadataNoPermissions(t *testing.T) {
	source := map[string]any{
		"fga_object": "core_platform_mesh_io_workspace:cluster-x/work-y",
	}

	testlogger := testlogger.New().HideLogOutput()

	ctx, ok := buildAuthorizationContext(testlogger.Logger, source)
	if !ok {
		t.Fatalf("expected valid context")
	}

	if ctx.object != "core_platform_mesh_io_workspace:cluster-x/work-y" {
		t.Errorf("unexpected object: %s", ctx.object)
	}
	if len(ctx.contextualTuples) != 0 {
		t.Errorf("expected 0 tuples, got %d", len(ctx.contextualTuples))
	}
}
