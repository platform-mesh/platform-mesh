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

package listener

import (
	"context"
	"fmt"

	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/clusteraccess"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/resource"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type Server struct {
	Config *Config

	Controllers
}

type Controllers struct {
	// Resource reconciler is used when we are operating in kubernetes mode
	Resource *resource.Reconciler
	// ClusterAccess reconciler watches ClusterAccess CRD resources
	ClusterAccess *clusteraccess.ClusterAccessReconciler
}

func NewServer(ctx context.Context, c *Config) (*Server, error) {
	logger := klog.FromContext(ctx)
	logger.Info("Setting up Listener Server controllers")

	s := &Server{
		Config: c,
	}

	opts := controller.TypedOptions[mcreconcile.Request]{}
	var err error

	if c.Options.EnableResourceController {
		logger.Info("Setting up Resource controller")
		s.Resource, err = resource.New(
			ctx,
			s.Config.Manager,
			opts,
			s.Config.SchemaHandler,
			c.Options.AnchorResource,
			c.Options.ResourceGVR,
			c.Options.AdditonalPathAnnotationKey,
			c.Options.ClusterMetadataFunc,
			c.Options.ClusterURLResolverFunc,
		)
		if err != nil {
			return nil, fmt.Errorf("error setting up Namespace Controller: %w", err)
		}
		if err := s.Resource.SetupWithManager(s.Config.Manager, c.ResourceControllerForOptions...); err != nil {
			return nil, fmt.Errorf("error setting up Namespace controller with manager: %w", err)
		}
	}

	if c.Options.EnableClusterAccessController {
		logger.Info("Setting up ClusterAccess controller")
		s.ClusterAccess, err = clusteraccess.NewClusterAccessReconciler(
			ctx,
			s.Config.Manager,
			opts,
			s.Config.SchemaHandler,
		)
		if err != nil {
			return nil, fmt.Errorf("error setting up ClusterAccess controller: %w", err)
		}
		if err := s.ClusterAccess.SetupWithManager(s.Config.Manager, c.ClusterAccessControllerForOptions...); err != nil {
			return nil, fmt.Errorf("error setting up ClusterAccess controller with manager: %w", err)
		}
	}

	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	logger.Info("Starting Listener")

	// Gracefully stop the gRPC server when the context is cancelled
	go func() {
		<-ctx.Done()
		s.Config.GracefulStop()
	}()

	return s.Config.Manager.Start(ctx)
}
