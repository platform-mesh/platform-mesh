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

package gateway

import (
	"context"
	"sync"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/http"

	"k8s.io/klog/v2"
)

type Server struct {
	HTTPServer *http.Server
	Gateway    *gateway.Service
}

func NewServer(c *Config) (Server, error) {
	return Server{
		HTTPServer: c.HTTPServer,
		Gateway:    c.Gateway,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	logger.Info("Starting Gateway Server")

	wg := sync.WaitGroup{}
	wg.Go(func() {
		if err := s.Gateway.Run(ctx); err != nil {
			logger.Error(err, "Gateway encountered an error")
		}
	})

	wg.Go(func() {
		if err := s.HTTPServer.Run(ctx); err != nil {
			logger.Error(err, "HTTP server encountered an error")
		}
	})

	wg.Wait()
	return nil
}
