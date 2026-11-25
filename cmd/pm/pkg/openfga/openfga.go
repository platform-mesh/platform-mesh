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

// Package openfga runs an OpenFGA server inside the process, with its
// tuples and models in a SQLite file. In a cluster OpenFGA is a Deployment
// with a Postgres database.
package openfga

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/validator"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/server"
	"github.com/openfga/openfga/pkg/storage/migrate"
	"github.com/openfga/openfga/pkg/storage/sqlcommon"
	"github.com/openfga/openfga/pkg/storage/sqlite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Options configure the server.
type Options struct {
	// Port is the localhost port of the gRPC API.
	Port int
	// Dir keeps the database.
	Dir string
}

// Server is a running OpenFGA.
type Server struct {
	// Addr is the host:port of the gRPC API, without TLS, as the
	// components dial it.
	Addr string
	// Client talks to it.
	Client openfgav1.OpenFGAServiceClient
}

// Start migrates the database and serves OpenFGA until ctx is done.
func Start(ctx context.Context, o Options) (*Server, error) {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	uri := "file:" + filepath.Join(o.Dir, "openfga.db")
	if err := migrate.RunMigrations(migrate.MigrationConfig{Engine: "sqlite", URI: uri, Timeout: time.Minute, PingTimeout: 5 * time.Second}); err != nil {
		return nil, fmt.Errorf("migrating the database: %w", err)
	}
	datastore, err := sqlite.New(uri, sqlcommon.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("opening the database: %w", err)
	}
	fga, err := server.NewServerWithOpts(server.WithDatastore(datastore), server.WithContext(ctx))
	if err != nil {
		datastore.Close()
		return nil, err
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fga.Close()
		datastore.Close()
		return nil, fmt.Errorf("--openfga-port: %w", err)
	}
	// As "openfga run" chains them: OpenFGA relies on the validator to
	// reject malformed requests.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpc_recovery.UnaryServerInterceptor(), validator.UnaryServerInterceptor()),
		grpc.ChainStreamInterceptor(grpc_recovery.StreamServerInterceptor(), validator.StreamServerInterceptor()),
	)
	openfgav1.RegisterOpenFGAServiceServer(grpcServer, fga)
	go func() { _ = grpcServer.Serve(ln) }()
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		fga.Close()
		datastore.Close()
	}()

	addr = ln.Addr().String()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Server{Addr: addr, Client: openfgav1.NewOpenFGAServiceClient(conn)}, nil
}
