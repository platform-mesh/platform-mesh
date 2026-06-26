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

package schemahandler_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"
	proto "go.platform-mesh.io/kubernetes-graphql-gateway/sdk"
)

func TestGRPCHandler_ReadWriteDelete(t *testing.T) {
	h := schemahandler.NewGRPCHandler()
	ctx := t.Context()

	// Read non-existent
	_, err := h.Read(ctx, "cluster1")
	require.ErrorIs(t, err, schemahandler.ErrNotExist)

	// Write and read back
	require.NoError(t, h.Write(ctx, []byte("v1"), "cluster1"))
	data, err := h.Read(ctx, "cluster1")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), data)

	// Update and read back
	require.NoError(t, h.Write(ctx, []byte("v2"), "cluster1"))
	data, err = h.Read(ctx, "cluster1")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), data)

	// Delete and verify gone
	require.NoError(t, h.Delete(ctx, "cluster1"))
	_, err = h.Read(ctx, "cluster1")
	assert.ErrorIs(t, err, schemahandler.ErrNotExist)
}

// mockStream collects responses and signals receipt via a channel
type mockStream struct {
	grpc.ServerStreamingServer[proto.SubscribeResponse]
	ctx      context.Context
	received chan *proto.SubscribeResponse
}

func newMockStream(ctx context.Context) *mockStream {
	return &mockStream{
		ctx:      ctx,
		received: make(chan *proto.SubscribeResponse, 10),
	}
}

func (m *mockStream) Context() context.Context { return m.ctx }

func (m *mockStream) Send(resp *proto.SubscribeResponse) error {
	m.received <- resp
	return nil
}

func (m *mockStream) expect(t *testing.T) *proto.SubscribeResponse {
	t.Helper()
	select {
	case resp := <-m.received:
		return resp
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
		return nil
	}
}

func TestGRPCHandler_Subscribe(t *testing.T) {
	t.Run("sends existing schemas on connect", func(t *testing.T) {
		h := schemahandler.NewGRPCHandler()
		ctx := t.Context()

		err := h.Write(ctx, []byte("existing"), "cluster1")
		require.NoError(t, err)

		stream := newMockStream(ctx)
		go func() {
			err := h.Subscribe(&proto.SubscribeRequest{}, stream)
			require.NoError(t, err)
		}()

		resp := stream.expect(t)
		assert.Equal(t, "cluster1", resp.ClusterName)
		assert.Equal(t, proto.SubscribeResponse_CREATED, resp.EventType)
	})

	t.Run("receives live updates", func(t *testing.T) {
		h := schemahandler.NewGRPCHandler()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream := newMockStream(ctx)
		done := make(chan struct{})
		go func() {
			err := h.Subscribe(&proto.SubscribeRequest{}, stream)
			require.NoError(t, err)
			close(done)
		}()

		err := h.Write(ctx, []byte("new"), "cluster2")
		require.NoError(t, err)

		resp := stream.expect(t)
		assert.Equal(t, "cluster2", resp.ClusterName)

		cancel()
		<-done
	})
}
