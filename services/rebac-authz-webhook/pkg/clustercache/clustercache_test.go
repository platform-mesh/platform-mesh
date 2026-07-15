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

package clustercache_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/mocks"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const testCluster = multicluster.ClusterName("test-cluster")

// fastBackoff makes the resolver retry almost immediately so tests don't wait
// on the production cadence.
var fastBackoff = wait.Backoff{Duration: time.Millisecond, Factor: 1.0, Steps: 100, Cap: time.Millisecond}

// waitStopped blocks until the resolver's call counter stops climbing, so a
// late Get call can't race the testify mock's AssertExpectations at cleanup.
func waitStopped(t *testing.T, gets *atomic.Int32) {
	t.Helper()
	assert.Eventually(t, func() bool {
		before := gets.Load()
		time.Sleep(40 * time.Millisecond)
		return gets.Load() == before
	}, 2*time.Second, 20*time.Millisecond, "resolver did not stop after cancel")
}

type fixture struct {
	cc clustercache.Provider
}

// spec describes one resolution scenario for newFixture.
type spec struct {
	path       string       // kcp.io/path annotation on the LogicalCluster
	owner      string       // spec.owner.cluster; "" => omitted
	storeID    string       // Store status.storeId; "" => Store returns without storeId
	withStore  bool         // wire the orgs Store lookup
	failFirstN int32        // fail the LogicalCluster Get this many times first
	lcGetErr   error        // permanent LogicalCluster Get error (never resolves)
	backoff    wait.Backoff // resolver cadence; zero value => fastBackoff
}

// newFixture wires the mocks the resolver touches and Engages one cluster: the
// LogicalCluster Get fails its first failFirstN calls, then returns an object at
// path/owner; withStore adds the orgs Store lookup returning storeID.
func newFixture(t *testing.T, s spec) fixture {
	t.Helper()

	cl := mocks.NewCluster(t)
	k8sClient := mocks.NewClient(t)
	mgr := mocks.NewManager(t)

	cl.EXPECT().GetClient().Return(k8sClient)
	if s.storeID != "" {
		// GetConfig is only reached on the full success path (after storeId).
		cl.EXPECT().GetConfig().Return(&rest.Config{Host: "https://example.com"})
	}

	var gets atomic.Int32
	k8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			defer gets.Add(1) // increment last so waitStopped's barrier orders the obj writes below
			if s.lcGetErr != nil {
				return s.lcGetErr
			}
			if gets.Load() < s.failFirstN {
				return errors.New("logicalclusters permission claim not accepted")
			}
			lc := obj.(*unstructured.Unstructured)
			lc.SetAnnotations(map[string]string{"kcp.io/path": s.path})
			if s.owner != "" {
				lc.Object["spec"] = map[string]any{"owner": map[string]any{"cluster": s.owner}}
			}
			return nil
		})

	if s.withStore {
		orgsCluster := mocks.NewCluster(t)
		orgsClient := mocks.NewClient(t)
		mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(orgsCluster, nil)
		orgsCluster.EXPECT().GetClient().Return(orgsClient)
		orgsClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, _ ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
				status := map[string]any{}
				if s.storeID != "" {
					status["storeId"] = s.storeID
				}
				obj.(*unstructured.Unstructured).Object = map[string]any{"status": status}
				return nil
			})
	}

	backoff := s.backoff
	if backoff == (wait.Backoff{}) {
		backoff = fastBackoff
	}
	cc, err := clustercache.New(mgr, clustercache.WithBackoff(backoff))
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() { cancel(); waitStopped(t, &gets) })
	assert.NoError(t, cc.Engage(ctx, testCluster, cl))

	return fixture{cc: cc}
}

func (f fixture) cachedEventually(t *testing.T) (clustercache.ClusterInfo, bool) {
	t.Helper()
	assert.Eventually(t, func() bool {
		_, ok := f.cc.Get(testCluster)
		return ok
	}, 3*time.Second, time.Millisecond, "expected cluster to be cached")
	return f.cc.Get(testCluster)
}

func TestNew(t *testing.T) {
	cc, err := clustercache.New(mocks.NewManager(t))
	assert.NoError(t, err)
	assert.NotNil(t, cc)
}

func TestClusterCache_Get_NotFound(t *testing.T) {
	cc, err := clustercache.New(mocks.NewManager(t))
	assert.NoError(t, err)
	info, found := cc.Get(multicluster.ClusterName("non-existing"))
	assert.False(t, found)
	assert.Empty(t, info.StoreID)
}

// TestClusterCache_Engage covers the resolution outcomes: a workspace is cached
// only when the LogicalCluster and Store fully resolve; every incomplete or
// failing case must leave the cache empty (Engage stays non-blocking and never
// caches a bad workspace). failFirstN also proves the resolver keeps retrying
// past a transient failure and past Backoff.Steps.
func TestClusterCache_Engage(t *testing.T) {
	tests := []struct {
		name       string
		spec       spec
		wantCached bool
		wantAccot  string
		wantParent string
	}{
		{
			name:       "caches once LogicalCluster and Store resolve",
			spec:       spec{path: "root:orgs:myorg:ws:child", owner: "parent-cluster-id", storeID: "myorg-store-id", withStore: true},
			wantCached: true, wantAccot: "child", wantParent: "parent-cluster-id",
		},
		{
			name:       "recovers after transient LogicalCluster failures",
			spec:       spec{path: "root:orgs:myorg:ws:child", owner: "parent-cluster-id", storeID: "myorg-store-id", withStore: true, failFirstN: 2},
			wantCached: true, wantAccot: "child", wantParent: "parent-cluster-id",
		},
		{
			name:       "keeps retrying past Backoff.Steps",
			spec:       spec{path: "root:orgs:myorg:ws:child", owner: "parent-cluster-id", storeID: "myorg-store-id", withStore: true, failFirstN: 20, backoff: wait.Backoff{Duration: time.Millisecond, Factor: 1.0, Steps: 3, Cap: time.Millisecond}},
			wantCached: true, wantAccot: "child", wantParent: "parent-cluster-id",
		},
		{name: "skips non-org path", spec: spec{path: "root:platform-mesh-system"}},
		{name: "does not cache when owner missing", spec: spec{path: "root:orgs:myorg"}},
		{name: "does not cache on LogicalCluster get failure", spec: spec{path: "root:orgs:myorg", lcGetErr: errors.New("connection refused")}},
		{name: "does not cache when storeId missing", spec: spec{path: "root:orgs:myorg", owner: "parent-cluster", withStore: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.spec)

			if tt.wantCached {
				info, _ := f.cachedEventually(t)
				assert.Equal(t, tt.wantAccot, info.AccountName)
				assert.Equal(t, tt.wantParent, info.ParentClusterID)
				assert.NotNil(t, info.RESTMapper)
				return
			}
			assert.Never(t, func() bool {
				_, ok := f.cc.Get(testCluster)
				return ok
			}, 50*time.Millisecond, 5*time.Millisecond, "must not cache a bad workspace")
		})
	}
}

// alwaysFail builds a cluster whose LogicalCluster Get never succeeds, counting
// calls. Used to observe resolver retry behavior (dedup, cancellation).
func alwaysFail(t *testing.T) (*mocks.Cluster, clustercache.Provider, *atomic.Int32) {
	t.Helper()
	cl := mocks.NewCluster(t)
	k8sClient := mocks.NewClient(t)
	cl.EXPECT().GetClient().Return(k8sClient)

	var gets atomic.Int32
	k8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ ctrlruntimeclient.ObjectKey, _ ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			gets.Add(1)
			return errors.New("still failing")
		})

	slow := wait.Backoff{Duration: 20 * time.Millisecond, Factor: 1.0, Steps: 1, Cap: 20 * time.Millisecond}
	cc, err := clustercache.New(mocks.NewManager(t), clustercache.WithBackoff(slow))
	assert.NoError(t, err)
	return cl, cc, &gets
}

// TestClusterCache_Engage_ReentrantDedup verifies repeated Engage of the same
// cluster instance does not stack resolver goroutines: an always-failing
// resolver on a 20ms cadence ticks ~5 times over 100ms; five stacked resolvers
// would tick ~25.
func TestClusterCache_Engage_ReentrantDedup(t *testing.T) {
	cl, cc, gets := alwaysFail(t)

	ctx, cancel := context.WithCancel(t.Context())
	for range 5 {
		assert.NoError(t, cc.Engage(ctx, testCluster, cl))
	}

	time.Sleep(100 * time.Millisecond)
	assert.Less(t, gets.Load(), int32(15), "re-entrant Engage must not stack resolvers")

	cancel()
	waitStopped(t, gets)
}

// TestClusterCache_Engage_ContextCancellationStopsResolver verifies the resolver
// stops making calls once its context is cancelled.
func TestClusterCache_Engage_ContextCancellationStopsResolver(t *testing.T) {
	cl, cc, gets := alwaysFail(t)

	ctx, cancel := context.WithCancel(t.Context())
	assert.NoError(t, cc.Engage(ctx, testCluster, cl))

	assert.Eventually(t, func() bool { return gets.Load() >= 1 }, time.Second, time.Millisecond)
	cancel()
	waitStopped(t, gets)
}
