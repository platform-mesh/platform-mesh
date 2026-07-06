package testfakes

import (
	"context"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/watch"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

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
