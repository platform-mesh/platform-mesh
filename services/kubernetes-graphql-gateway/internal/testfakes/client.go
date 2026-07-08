package testfakes

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ListItems returns a client List function returning objs filtered by GVK.
func ListItems(
	objs ...unstructured.Unstructured,
) func(context.Context, ctrlruntimeclient.ObjectList, ...ctrlruntimeclient.ListOption) error {
	byGVK := map[schema.GroupVersionKind][]unstructured.Unstructured{}
	for _, v := range objs {
		gvk := v.GroupVersionKind()
		byGVK[gvk] = append(byGVK[gvk], v)
	}

	return func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
		ul := list.(*unstructured.UnstructuredList)
		ul.SetResourceVersion("100")

		gvk := ul.GetObjectKind().GroupVersionKind()
		gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")
		ul.Items = byGVK[gvk]
		return nil
	}
}

// FakeClient implements client.WithWatch with controllable List and Watch behavior.
type FakeClient struct {
	ctrlruntimeclient.WithWatch
	watchCalls int32
	listCalls  int32
	watchFn    func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error)
	listFn     func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error
}

func NewClient(
	listFn func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error,
	watchFn func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error),
) *FakeClient {
	return &FakeClient{
		listFn:  listFn,
		watchFn: watchFn,
	}
}

func (f *FakeClient) Watch(ctx context.Context, obj ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) (watch.Interface, error) {
	atomic.AddInt32(&f.watchCalls, 1)
	if f.watchFn != nil {
		return f.watchFn(ctx, obj, opts...)
	}
	return NewWatcher(), nil
}

func (f *FakeClient) List(ctx context.Context, obj ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
	atomic.AddInt32(&f.listCalls, 1)
	if f.listFn != nil {
		return f.listFn(ctx, obj, opts...)
	}
	return nil
}

func (f *FakeClient) ListCalls() int32 {
	return atomic.LoadInt32(&f.listCalls)
}

func (f *FakeClient) WatchCalls() int32 {
	return atomic.LoadInt32(&f.watchCalls)
}

// FakeWatcher implements watch.Interface for testing.
type FakeWatcher struct {
	events chan watch.Event
	done   chan struct{}
	once   sync.Once
}

func NewWatcher() *FakeWatcher {
	return &FakeWatcher{
		events: make(chan watch.Event, 10),
		done:   make(chan struct{}),
	}
}

func (f *FakeWatcher) ResultChan() <-chan watch.Event {
	return f.events
}

func (f *FakeWatcher) Stop() {
	f.once.Do(func() {
		close(f.done)
	})
}

// WriteChan returns the producer-side Event channel.
func (f *FakeWatcher) WriteChan() chan<- watch.Event {
	return f.events
}
