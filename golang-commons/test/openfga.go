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

package test

import (
	"context"
	"net"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/bufconn"

	"go.platform-mesh.io/golang-commons/fga/helpers"
)

type config struct {
	storeName          string
	tuples             []*openfgav1.TupleKey
	authorizationModel string
}

type TestServerOption func(*config)

func WithStoreName(storeName string) TestServerOption {
	return func(c *config) {
		c.storeName = storeName
	}
}

func WithAuthorizationModel(authorizationModel string) TestServerOption {
	return func(c *config) {
		c.authorizationModel = authorizationModel
	}
}

func WithTuples(tuples []*openfgav1.TupleKey) TestServerOption {
	return func(c *config) {
		c.tuples = tuples
	}
}

type OpenFGATestServer struct {
	grpcServer    *grpc.Server
	openfgaServer *server.Server
	lis           *bufconn.Listener
}

func NewOpenFGATestServer(ctx context.Context, opts ...TestServerOption) (*OpenFGATestServer, error) {

	testServer := &OpenFGATestServer{}

	cfg := &config{
		storeName:          "test",
		authorizationModel: `type user`,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	testServer.openfgaServer = server.MustNewServerWithOpts(server.WithDatastore(memory.New()))

	testServer.grpcServer = grpc.NewServer()
	openfgav1.RegisterOpenFGAServiceServer(testServer.grpcServer, testServer.openfgaServer)

	store, err := testServer.openfgaServer.CreateStore(ctx, &openfgav1.CreateStoreRequest{
		Name: cfg.storeName,
	})
	if err != nil {
		return nil, err
	}

	parsedModel, err := language.TransformDSLToProto(cfg.authorizationModel)
	if err != nil {
		return nil, err
	}

	authzModel, err := testServer.openfgaServer.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         store.Id,
		TypeDefinitions: parsedModel.TypeDefinitions,
		SchemaVersion:   parsedModel.SchemaVersion,
		Conditions:      parsedModel.Conditions,
	})
	if err != nil {
		return nil, err
	}

	for _, tuple := range cfg.tuples {
		_, err := testServer.openfgaServer.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              store.Id,
			AuthorizationModelId: authzModel.AuthorizationModelId,
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{tuple},
			},
		})
		if helpers.IsDuplicateWriteError(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
	}

	return testServer, nil
}

func (o *OpenFGATestServer) Start() (openfgav1.OpenFGAServiceClient, error) {
	o.lis = bufconn.Listen(101024 * 1024)

	go func() {
		if err := o.grpcServer.Serve(o.lis); err != nil {
			panic(err)
		}
	}()

	resolver.SetDefaultScheme("passthrough")
	conn, err := grpc.NewClient("",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) { return o.lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return openfgav1.NewOpenFGAServiceClient(conn), nil
}

func (o *OpenFGATestServer) Stop() {
	o.grpcServer.Stop()
	o.openfgaServer.Close()
}
