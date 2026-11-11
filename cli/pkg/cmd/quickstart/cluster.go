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

package quickstart

import (
	"fmt"
	"slices"
	"time"

	"sigs.k8s.io/kind/pkg/cluster"
	kindcmd "sigs.k8s.io/kind/pkg/cmd"
)

// kindConfig is the cluster: one node, with the gateway NodePort published on
// the loopback interface of the host.
const kindConfig = `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: "127.0.0.1"
  kubeProxyMode: iptables
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: %d
        hostPort: %d
        listenAddress: "127.0.0.1"
        protocol: TCP
`

// newKindProvider returns the kind provider for the container runtime; auto
// leaves the choice to kind (KIND_EXPERIMENTAL_PROVIDER, then whichever of
// docker, podman and nerdctl is available).
func newKindProvider(runtime string) (*cluster.Provider, error) {
	var opt cluster.ProviderOption
	switch runtime {
	case "docker":
		opt = cluster.ProviderWithDocker()
	case "podman":
		opt = cluster.ProviderWithPodman()
	case "nerdctl":
		opt = cluster.ProviderWithNerdctl("")
	default:
		var err error
		if opt, err = cluster.DetectNodeProvider(); err != nil {
			return nil, fmt.Errorf("detecting the container runtime: %w", err)
		}
		if opt == nil {
			return nil, fmt.Errorf("no container runtime found: docker, podman or nerdctl must be installed and running")
		}
	}
	return cluster.NewProvider(opt, cluster.ProviderWithLogger(kindcmd.NewLogger())), nil
}

// createCluster creates the kind cluster unless it exists, and writes its
// kubeconfig to o.Kubeconfig either way.
func (o *Options) createCluster() error {
	provider, err := newKindProvider(o.ContainerRuntime)
	if err != nil {
		return err
	}
	clusters, err := provider.List()
	if err != nil {
		return fmt.Errorf("listing kind clusters: %w", err)
	}
	if slices.Contains(clusters, o.Name) {
		_, _ = fmt.Fprintf(o.ErrOut, "Using existing cluster %q ...\n", o.Name)
		if err := provider.ExportKubeConfig(o.Name, o.Kubeconfig, false); err != nil {
			return fmt.Errorf("exporting the kubeconfig of kind cluster %q: %w", o.Name, err)
		}
		return nil
	}

	opts := []cluster.CreateOption{
		cluster.CreateWithRawConfig(fmt.Appendf(nil, kindConfig, gatewayNodePort, gatewayPort)),
		cluster.CreateWithKubeconfigPath(o.Kubeconfig),
		cluster.CreateWithWaitForReady(5 * time.Minute),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	}
	if o.NodeImage != "" {
		opts = append(opts, cluster.CreateWithNodeImage(o.NodeImage))
	}
	if err := provider.Create(o.Name, opts...); err != nil {
		return fmt.Errorf("creating kind cluster %q: %w", o.Name, err)
	}
	return nil
}
