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
	"time"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/golang-commons/traces"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/options"

	genericapiserver "k8s.io/apiserver/pkg/server"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type command struct {
	options *options.Options
}

func NewCommand() *cobra.Command {
	c := &command{
		options: options.NewOptions(),
	}

	cmd := &cobra.Command{
		Use:   "listener",
		Short: "Run the listener server",
		RunE:  c.run,
	}

	c.options.AddFlags(cmd.Flags())
	return cmd
}

func (c *command) run(cmd *cobra.Command, args []string) error {
	if err := logsv1.ValidateAndApply(c.options.Logs, nil); err != nil {
		return err
	}
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log.SetLogger(klog.NewKlogr())

	completed, err := c.options.Complete()
	if err != nil {
		return err
	}
	if err := completed.Validate(); err != nil {
		return err
	}

	ctx := genericapiserver.SetupSignalContext()

	if completed.Common.Tracing.Enabled {
		shutdown, err := traces.InitProvider(ctx, completed.Common.Tracing.Collector)
		if err != nil {
			return fmt.Errorf("error initializing tracing: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(shutdownCtx)
		}()
	}

	config, err := listener.NewConfig(completed)
	if err != nil {
		return err
	}
	server, err := listener.NewServer(ctx, config)
	if err != nil {
		return err
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("error running listener: %w", err)
	}

	<-ctx.Done()
	return nil
}
