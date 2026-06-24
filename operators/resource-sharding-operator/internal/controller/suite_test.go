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

//nolint:goconst
package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	pmshardingv1alpha1 "go.platform-mesh.io/apis/sharding/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	testTimeout  = 90 * time.Second
	testInterval = 250 * time.Millisecond
)

type ResourceShardingSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	env       *envtest.Environment
	k8sClient ctrlruntimeclient.Client
	mgr       manager.Manager
	scheme    *k8sruntime.Scheme
	mgrErr    chan error
}

func TestResourceShardingSuite(t *testing.T) {
	suite.Run(t, new(ResourceShardingSuite))
}

func (s *ResourceShardingSuite) SetupSuite() {
	// Wire controller-runtime's logger so reconciler and dynamic-controller
	// output land in the test log. Without this the runtime swallows everything
	// and CI failures are opaque.
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	s.scheme = k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s.scheme))
	utilruntime.Must(pmshardingv1alpha1.AddToScheme(s.scheme))

	assetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if assetsDir == "" {
		assetsDir = filepath.Join("..", "..", "bin", "k8s", fmt.Sprintf("1.29.0-%s-%s", runtime.GOOS, runtime.GOARCH))
	}

	s.env = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd"),
		},
		BinaryAssetsDirectory: assetsDir,
	}

	cfg, err := s.env.Start()
	s.Require().NoError(err)

	s.k8sClient, err = ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: s.scheme})
	s.Require().NoError(err)

	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.mgr, err = ctrl.NewManager(cfg, manager.Options{
		Scheme: s.scheme,
	})
	s.Require().NoError(err)

	err = SetupWithManager(s.mgr)
	s.Require().NoError(err)

	s.mgrErr = make(chan error, 1)
	go func() { s.mgrErr <- s.mgr.Start(s.ctx) }()
}

func (s *ResourceShardingSuite) TearDownSuite() {
	s.cancel()
	if err := <-s.mgrErr; err != nil && err != context.Canceled {
		s.NoError(err)
	}
	err := s.env.Stop()
	s.Require().NoError(err)
}

// TestStartDynamicController_CancelledContext verifies that StartDynamicController
// returns an error when the context is already cancelled before the cache can sync.
// Uses an empty ShardLabelKey to also exercise the default-labelKey assignment branch.
func (s *ResourceShardingSuite) TestStartDynamicController_CancelledContext() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dc-cancelled",
			UID:  types.UID("uid-dc-cancelled"),
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{Group: "", Version: "v1", Resource: "configmaps"},
			// Intentionally empty ShardLabelKey to exercise the default-assignment branch.
			ShardLabelKey: "",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "shard-a"}},
		},
	}
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := StartDynamicController(cancelledCtx, s.mgr, rs, gvr)
	s.Require().Error(err, "StartDynamicController with cancelled context should return an error")
}

// TestStartDynamicController_InvalidLabelKey verifies that StartDynamicController
// returns an error when the ShardLabelKey contains invalid characters (making the
// label selector unparseable).
func (s *ResourceShardingSuite) TestStartDynamicController_InvalidLabelKey() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dc-invalid-key",
			UID:  types.UID("uid-dc-invalid"),
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target:        pmshardingv1alpha1.TargetResource{Group: "", Version: "v1", Resource: "configmaps"},
			ShardLabelKey: "invalid label key with spaces",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "shard-a"}},
		},
	}
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	_, err := StartDynamicController(s.ctx, s.mgr, rs, gvr)
	s.Require().Error(err, "StartDynamicController with invalid label key should return an error")
	s.Contains(err.Error(), "parsing label selector")
}

// TestStartDynamicController_UnknownGVR verifies that StartDynamicController
// returns an error when the GVR cannot be resolved to a GVK.
func (s *ResourceShardingSuite) TestStartDynamicController_UnknownGVR() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dc-unknown-gvr",
			UID:  types.UID("uid-dc-unknown"),
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target:        pmshardingv1alpha1.TargetResource{Group: "nonexistent.example.io", Version: "v1", Resource: "fakes"},
			ShardLabelKey: "test.unknown.io/shard",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "shard-a"}},
		},
	}
	gvr := schema.GroupVersionResource{Group: "nonexistent.example.io", Version: "v1", Resource: "fakes"}

	_, err := StartDynamicController(s.ctx, s.mgr, rs, gvr)
	s.Require().Error(err, "StartDynamicController with unknown GVR should return an error")
	s.Contains(err.Error(), "resolving GVR")
}
