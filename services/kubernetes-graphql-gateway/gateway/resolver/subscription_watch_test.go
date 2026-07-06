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

package resolver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeWatcher implements watch.Interface for testing.
type fakeWatcher struct {
	events chan watch.Event
	done   chan struct{}
	once   sync.Once
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events: make(chan watch.Event, 10),
		done:   make(chan struct{}),
	}
}

func (f *fakeWatcher) ResultChan() <-chan watch.Event {
	return f.events
}

func (f *fakeWatcher) Stop() {
	f.once.Do(func() {
		close(f.done)
	})
}

// fakeClient implements client.WithWatch with controllable List and Watch behavior.
type fakeClient struct {
	ctrlruntimeclient.WithWatch
	watchCalls int32
	listCalls  int32
	watchFn    func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error)
	listFn     func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error
}

func (f *fakeClient) Watch(ctx context.Context, obj ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
	atomic.AddInt32(&f.watchCalls, 1)
	if f.watchFn != nil {
		return f.watchFn(ctx, obj, opts...)
	}
	return newFakeWatcher(), nil
}

func (f *fakeClient) List(ctx context.Context, obj ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
	atomic.AddInt32(&f.listCalls, 1)
	if f.listFn != nil {
		return f.listFn(ctx, obj, opts...)
	}
	return nil
}

func makeResolveParams(ctx context.Context) graphql.ResolveParams {
	return graphql.ResolveParams{
		Context: ctx,
		Args: map[string]any{
			SubscribeToAllArg: true,
		},
		Info: graphql.ResolveInfo{
			FieldASTs: []*ast.Field{
				{
					SelectionSet: &ast.SelectionSet{
						Selections: []ast.Selection{},
					},
				},
			},
		},
	}
}

func makeUnstructuredObj(name, namespace, rv string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":            name,
				"namespace":       namespace,
				"resourceVersion": rv,
			},
		},
	}
}

func makeStatusEvent(code int32, reason metav1.StatusReason, message string) watch.Event {
	return watch.Event{
		Type: watch.Error,
		Object: &metav1.Status{
			TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
			Status:   metav1.StatusFailure,
			Message:  message,
			Reason:   reason,
			Code:     code,
		},
	}
}

func collectResults(ch chan any, timeout time.Duration) []any {
	var results []any
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case val, ok := <-ch:
			if !ok {
				return results
			}
			results = append(results, val)
		case <-timer.C:
			return results
		}
	}
}

// collectNResults drains ch until a specific item count has been received.
func collectNResults(ch chan any, count int) []any {
	out := make([]any, 0, count)
	timeout := 5 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(out) < count {
		select {
		case v, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, v)
		case <-timer.C:
			return out
		}
	}
	return out
}

func TestRunWatch_WatchErrorGone_TriggersReconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var watchCall int32
	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			call := atomic.AddInt32(&watchCall, 1)
			w := newFakeWatcher()
			if call == 1 {
				go func() {
					w.events <- makeStatusEvent(http.StatusGone, metav1.StatusReasonExpired, "resource version too old")
					close(w.events)
				}()
			} else {
				go func() {
					obj := makeUnstructuredObj("test", "default", "200")
					w.events <- watch.Event{Type: watch.Added, Object: obj}
					<-ctx.Done()
					close(w.events)
				}()
			}
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)
	cancel()

	var foundAdded bool
	for _, r := range results {
		if env, ok := r.(SubscriptionEnvelope); ok && env.Type == EventTypeAdded {
			foundAdded = true
		}
	}
	assert.True(t, foundAdded, "expected ADDED event after reconnection")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&watchCall), int32(2), "expected at least 2 watch calls (reconnect)")
}

func TestRunWatch_WatchError500_TriggersReconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var watchCall int32
	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			call := atomic.AddInt32(&watchCall, 1)
			w := newFakeWatcher()
			if call == 1 {
				go func() {
					w.events <- makeStatusEvent(http.StatusInternalServerError, metav1.StatusReasonInternalError, "internal error")
					close(w.events)
				}()
			} else {
				go func() {
					obj := makeUnstructuredObj("test", "default", "200")
					w.events <- watch.Event{Type: watch.Added, Object: obj}
					<-ctx.Done()
					close(w.events)
				}()
			}
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)
	cancel()

	var foundAdded bool
	for _, r := range results {
		if env, ok := r.(SubscriptionEnvelope); ok && env.Type == EventTypeAdded {
			foundAdded = true
		}
	}
	assert.True(t, foundAdded, "expected ADDED event after reconnection")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&watchCall), int32(2))
}

func TestRunWatch_WatchError403_IsTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			w := newFakeWatcher()
			go func() {
				w.events <- makeStatusEvent(http.StatusForbidden, metav1.StatusReasonForbidden, "forbidden")
				close(w.events)
			}()
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)

	var foundErr bool
	for _, r := range results {
		if _, ok := r.(error); ok {
			foundErr = true
		}
	}
	assert.True(t, foundErr, "expected error sent to client for 403")
	assert.Equal(t, int32(1), atomic.LoadInt32(&fc.watchCalls), "should not retry on 403")
}

func TestRunWatch_ChannelClose_TriggersReconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var watchCall int32
	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			call := atomic.AddInt32(&watchCall, 1)
			w := newFakeWatcher()
			if call == 1 {
				go func() {
					close(w.events)
				}()
			} else {
				go func() {
					obj := makeUnstructuredObj("test", "default", "200")
					w.events <- watch.Event{Type: watch.Added, Object: obj}
					<-ctx.Done()
					close(w.events)
				}()
			}
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)
	cancel()

	var foundAdded bool
	for _, r := range results {
		if env, ok := r.(SubscriptionEnvelope); ok && env.Type == EventTypeAdded {
			foundAdded = true
		}
	}
	assert.True(t, foundAdded, "expected ADDED event after reconnection from channel close")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&watchCall), int32(2))
}

func TestRunWatch_ContextCancellation_StopsRetryLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			w := newFakeWatcher()
			go func() {
				w.events <- makeStatusEvent(http.StatusInternalServerError, metav1.StatusReasonInternalError, "error")
				close(w.events)
			}()
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	// Wait for the channel to close (which happens when runWatch returns)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		t.Fatal("runWatch did not stop after context cancellation")
	case _, ok := <-resultChannel:
		if ok {
			// Drain remaining
			for range resultChannel { //nolint:revive // will be potentially fixed by https://github.com/mgechev/revive/pull/1710
			}
		}
	}
}

func TestRunWatch_WatchCreationFailure_Retries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var watchCall int32
	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("100")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			call := atomic.AddInt32(&watchCall, 1)
			if call == 1 {
				return nil, fmt.Errorf("connection refused")
			}
			w := newFakeWatcher()
			go func() {
				obj := makeUnstructuredObj("test", "default", "200")
				w.events <- watch.Event{Type: watch.Added, Object: obj}
				<-ctx.Done()
				close(w.events)
			}()
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)
	cancel()

	var foundAdded bool
	for _, r := range results {
		if env, ok := r.(SubscriptionEnvelope); ok && env.Type == EventTypeAdded {
			foundAdded = true
		}
	}
	assert.True(t, foundAdded, "expected event after watch creation retry")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&watchCall), int32(2))
}

func TestRunWatch_NormalEvents_DeliveredCorrectly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	obj1 := makeUnstructuredObj("obj1", "default", "100")
	obj2 := makeUnstructuredObj("obj1", "default", "101")

	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			ul := list.(*unstructured.UnstructuredList)
			ul.SetResourceVersion("99")
			ul.Items = nil
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			w := newFakeWatcher()
			go func() {
				w.events <- watch.Event{Type: watch.Added, Object: obj1}
				w.events <- watch.Event{Type: watch.Modified, Object: obj2}
				w.events <- watch.Event{Type: watch.Deleted, Object: obj2}
				time.Sleep(100 * time.Millisecond)
				cancel()
			}()
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)

	require.Len(t, results, 3)

	env0 := results[0].(SubscriptionEnvelope)
	assert.Equal(t, EventTypeAdded, env0.Type)

	env1 := results[1].(SubscriptionEnvelope)
	assert.Equal(t, EventTypeModified, env1.Type)

	env2 := results[2].(SubscriptionEnvelope)
	assert.Equal(t, EventTypeDeleted, env2.Type)
}

func TestRunWatch_410OnWatchCreation_ClearsRVAndRelists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var watchCall int32
	var listCalls int32
	fc := &fakeClient{
		listFn: func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
			call := atomic.AddInt32(&listCalls, 1)
			ul := list.(*unstructured.UnstructuredList)
			if call == 1 {
				ul.SetResourceVersion("100")
				ul.Items = []unstructured.Unstructured{*makeUnstructuredObj("obj1", "default", "100")}
			} else {
				ul.SetResourceVersion("200")
				ul.Items = []unstructured.Unstructured{*makeUnstructuredObj("obj1", "default", "200")}
			}
			return nil
		},
		watchFn: func(_ context.Context, _ ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
			call := atomic.AddInt32(&watchCall, 1)
			if call == 1 {
				return nil, apierrors.NewResourceExpired("resource version too old")
			}
			w := newFakeWatcher()
			go func() {
				<-ctx.Done()
				close(w.events)
			}()
			return w, nil
		},
	}

	svc := &Service{runtimeClient: fc}
	resultChannel := make(chan any, 10)

	go svc.runWatch(makeResolveParams(ctx), schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, resultChannel, false, apiextensionsv1.ClusterScoped)

	results := collectResults(resultChannel, 3*time.Second)
	cancel()

	// Should have received ADDED events from both list calls
	var addedCount int
	for _, r := range results {
		if env, ok := r.(SubscriptionEnvelope); ok && env.Type == EventTypeAdded {
			addedCount++
		}
	}
	assert.GreaterOrEqual(t, addedCount, 2, "expected ADDED events from re-list after 410")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&listCalls), int32(2), "expected re-list after 410")
}

func TestSubscribeResourcesByCategory(t *testing.T) {
	t.Run("merges all input streams", func(t *testing.T) {
		makeObjKind := func(kind, name, namespace, rv string) *unstructured.Unstructured {
			return &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       kind,
					"metadata": map[string]any{
						"name":            name,
						"namespace":       namespace,
						"resourceVersion": rv,
					},
				},
			}
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		firstGroup := "foomanager.io"
		firstVersion := "v1"
		firstKind := "Issuer"

		secondGroup := "foomanager.io"
		secondVersion := "v1"
		secondKind := "CertificateRequest"

		client := fakeClient{
			listFn: listWithItems(),
			watchFn: func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
				gvk := list.GetObjectKind().GroupVersionKind()
				kind := strings.TrimSuffix(gvk.Kind, "List")
				w := newFakeWatcher()

				go func() {
					obj := makeObjKind(kind, "blapper", "system", "baz")
					w.events <- watch.Event{Type: watch.Added, Object: obj}
					<-ctx.Done()
					close(w.events)
				}()
				return w, nil
			},
		}

		svc := New(&client, nil)

		typemap := map[string][]TypeByCategory{
			"cert-manager": {
				{
					Group:   firstGroup,
					Version: firstVersion,
					Kind:    firstKind,
				},
				{
					Group:   secondGroup,
					Version: secondVersion,
					Kind:    secondKind,
				},
			},
		}

		rFn := svc.SubscribeResourcesByCategory(typemap)
		chObj, err := rFn(graphql.ResolveParams{
			Context: ctx,
			Args: map[string]any{
				NameArg: "cert-manager",
			},
		})

		require.NoError(t, err)

		resultChan, ok := chObj.(chan any)
		require.True(t, ok)

		result := collectNResults(resultChan, 2)

		// verify
		require.Len(t, result, 2) // might break with List?

		// assert
		seenKinds := make(map[string]struct{})
		for _, v := range result {
			envelope, ok := v.(SubscriptionEnvelope)
			require.True(t, ok)
			assert.Equal(t, EventTypeAdded, envelope.Type)

			fieldVals, ok := envelope.Object.(map[string]any)
			require.True(t, ok)

			kind := fieldVals["kind"]
			seenKinds[kind.(string)] = struct{}{}
		}

		for _, kind := range []string{firstKind, secondKind} {
			if _, got := seenKinds[kind]; !got {
				t.Errorf("expected kind %q got none in %v", kind, seenKinds)
			}
		}

		cancel()

		select {
		case _, open := <-resultChan:
			assert.False(t, open, "fan-in channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("fan-in channel did not close on context cancel")
		}
	})
}

// listWithItems returns a listFn returning items for the List operation.
func listWithItems(items ...unstructured.Unstructured) func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
	return func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
		ul := list.(*unstructured.UnstructuredList)
		ul.SetResourceVersion("100")
		ul.Items = items
		return nil
	}
}
