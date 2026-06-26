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

package watcher_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"go.platform-mesh.io/kubernetes-graphql-gateway/defaults"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/watcher"
	proto "go.platform-mesh.io/kubernetes-graphql-gateway/sdk"
)

// fakeHandler records schema events from the watcher.
type fakeHandler struct {
	mu       sync.Mutex
	changed  map[string][]byte
	changeCh chan string
}

func newFakeHandler() *fakeHandler {
	return &fakeHandler{
		changed:  make(map[string][]byte),
		changeCh: make(chan string, 10),
	}
}

func (h *fakeHandler) OnSchemaChanged(_ context.Context, cluster string, schema []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.changed[cluster] = schema
	h.changeCh <- cluster
}

func (h *fakeHandler) OnSchemaDeleted(_ context.Context, cluster string) {
	h.mu.Lock()
	defer h.mu.Unlock()
}

func TestGRPCWatcher_ConnectsAndReceives(t *testing.T) {
	// Start a fake gRPC schema server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	fake := &fakeSchemaServer{subscribeCh: make(chan struct{}, 10)}
	srv := grpc.NewServer()
	proto.RegisterSchemaHandlerServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.GracefulStop() })

	handler := newFakeHandler()
	var connected atomic.Bool

	gw, err := watcher.NewGRPCWatcher(watcher.GRPCWatcherConfig{
		Address:        lis.Addr().String(),
		MaxRecvMsgSize: defaults.DefaultGRPCMaxMsgSize,
	}, handler, &connected)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = gw.Run(ctx) }()

	// Wait for subscription to be established
	select {
	case <-fake.subscribeCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for subscribe")
	}
	assert.True(t, connected.Load())

	// Send a schema update and verify it arrives
	require.NoError(t, fake.send(&proto.SubscribeResponse{
		ClusterName: "test-cluster",
		Schema:      []byte("schema-data"),
		EventType:   proto.SubscribeResponse_CREATED,
	}))

	select {
	case cluster := <-handler.changeCh:
		assert.Equal(t, "test-cluster", cluster)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for schema change")
	}
}

// fakeSchemaServer implements the Subscribe RPC for testing.
type fakeSchemaServer struct {
	proto.UnimplementedSchemaHandlerServer
	mu          sync.Mutex
	subscribers []proto.SchemaHandler_SubscribeServer
	subscribeCh chan struct{}
}

func (s *fakeSchemaServer) Subscribe(_ *proto.SubscribeRequest, stream proto.SchemaHandler_SubscribeServer) error {
	s.mu.Lock()
	s.subscribers = append(s.subscribers, stream)
	s.mu.Unlock()

	s.subscribeCh <- struct{}{}
	<-stream.Context().Done()
	return nil
}

func (s *fakeSchemaServer) send(resp *proto.SubscribeResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subscribers {
		if err := sub.Send(resp); err != nil {
			return err
		}
	}
	return nil
}
