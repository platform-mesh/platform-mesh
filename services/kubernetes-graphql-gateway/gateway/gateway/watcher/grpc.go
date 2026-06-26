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

package watcher

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.platform-mesh.io/kubernetes-graphql-gateway/sdk"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GRPCWatcher watches for schema changes via gRPC streaming from a listener.
// It implements the Watcher interface.
type GRPCWatcher struct {
	conn      *grpc.ClientConn
	client    sdk.SchemaHandlerClient
	handler   SchemaEventHandler
	connected *atomic.Bool
}

// GRPCWatcherConfig holds configuration for the gRPC watcher.
type GRPCWatcherConfig struct {
	// Address is the gRPC server address (e.g., "localhost:50051")
	Address string
	// MaxRecvMsgSize is the maximum message size in bytes the client can receive.
	MaxRecvMsgSize int
}

// NewGRPCWatcher creates a new gRPC watcher that connects to the given address
// and notifies the handler when schemas change.
func NewGRPCWatcher(config GRPCWatcherConfig, handler SchemaEventHandler, connected *atomic.Bool) (*GRPCWatcher, error) {
	// TODO: Add proper TLS configuration for production
	conn, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(config.MaxRecvMsgSize)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	client := sdk.NewSchemaHandlerClient(conn)

	return &GRPCWatcher{
		conn:      conn,
		client:    client,
		handler:   handler,
		connected: connected,
	}, nil
}

// Close closes the underlying gRPC client connection.
func (w *GRPCWatcher) Close() error {
	if w.conn != nil {
		return w.conn.Close()
	}
	return nil
}

// Run starts the gRPC watcher and blocks until the context is cancelled.
// It subscribes to schema updates from the listener and automatically
// reconnects on stream errors.
func (w *GRPCWatcher) Run(ctx context.Context) error {
	logger := log.FromContext(ctx)
	defer func() {
		if err := w.Close(); err != nil {
			logger.Error(err, "Failed to close gRPC connection")
		}
	}()

	// Uses exponential backoff with jitter to retry subscribe whenever
	// the stream breaks. Stops when ctx is cancelled.
	backoff := wait.Backoff{
		Duration: 800 * time.Millisecond,
		Cap:      30 * time.Second,
		Steps:    10,
		Factor:   2.0,
		Jitter:   1.0,
	}
	delay := backoff.DelayWithReset(&clock.RealClock{}, 2*time.Minute)

	return delay.Until(ctx, true, true, func(ctx context.Context) (bool, error) {
		if err := w.subscribe(ctx); err != nil {
			logger.Error(err, "gRPC stream error, reconnecting")
		}
		return false, nil
	})
}

// subscribe establishes a gRPC stream and processes messages until
// the stream breaks or the context is cancelled.
func (w *GRPCWatcher) subscribe(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// WaitForReady makes Subscribe block until the connection is established
	// instead of failing immediately when the server is not yet reachable.
	stream, err := w.client.Subscribe(ctx, &sdk.SubscribeRequest{}, grpc.WaitForReady(true))
	if err != nil {
		return fmt.Errorf("failed to subscribe to schema updates: %w", err)
	}

	logger.Info("Connected to gRPC schema handler, waiting for updates")
	w.connected.Store(true)
	defer w.connected.Store(false)

	for {
		res, err := stream.Recv()
		if err == io.EOF {
			return fmt.Errorf("stream closed by server")
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("error receiving from stream: %w", err)
		}

		switch res.EventType {
		case sdk.SubscribeResponse_CREATED, sdk.SubscribeResponse_UPDATED:
			logger.V(4).Info("Received schema update",
				"cluster", res.ClusterName,
				"event", res.EventType.String(),
			)
			w.handler.OnSchemaChanged(ctx, res.ClusterName, res.Schema)

		case sdk.SubscribeResponse_REMOVED:
			logger.V(4).Info("Received schema deletion",
				"cluster", res.ClusterName,
			)
			w.handler.OnSchemaDeleted(ctx, res.ClusterName)
		}
	}
}
