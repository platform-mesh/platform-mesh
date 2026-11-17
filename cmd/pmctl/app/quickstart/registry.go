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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	registryImage = "registry:3"
	// registryPort is the port the registry listens on in its container.
	registryPort = "5000"
)

// localRegistry is the OCI registry a snapshot is published to: a container
// next to the kind node on kind's network.
type localRegistry struct {
	// url is the address recorded in everything published, the one the
	// cluster pulls from: http://<container IP>:5000. The http:// tells
	// the ocm CLI, the ocm-k8s-toolkit and the operator that the registry
	// speaks plain HTTP.
	url string
	// pushHost is the registry on the loopback interface of the host.
	// docker pushes there: it treats 127.0.0.0/8 as insecure without any
	// daemon configuration.
	pushHost string
}

// host is url without the scheme.
func (r localRegistry) host() string {
	return strings.TrimPrefix(r.url, "http://")
}

func registryName(cluster string) string {
	return cluster + "-registry"
}

// kindNetwork is the container network kind puts its nodes on.
func kindNetwork() string {
	if n := os.Getenv("KIND_EXPERIMENTAL_DOCKER_NETWORK"); n != "" {
		return n
	}
	return "kind"
}

// ensureRegistry starts the registry container unless it runs, and returns
// its addresses. The content lives in a volume, so it survives the container.
func (s *snapshot) ensureRegistry(ctx context.Context) (localRegistry, error) {
	name := registryName(s.o.Name)
	network := kindNetwork()

	address, err := s.registryAddress(ctx, name, network)
	if err != nil {
		_ = s.run(ctx, "", nil, "docker", "rm", "-f", name)
		// Pinned, since the address is recorded in what is published and
		// docker hands out a new one whenever the container is recreated.
		// A network that cannot pin it gets a dynamic address.
		pinned := false
		if ip, err := s.registryIP(ctx, network); err == nil {
			pinned = s.startRegistry(ctx, name, network, "--ip", ip) == nil
		}
		if !pinned {
			_ = s.run(ctx, "", nil, "docker", "rm", "-f", name)
			if err := s.startRegistry(ctx, name, network); err != nil {
				return localRegistry{}, fmt.Errorf("starting the registry: %w", err)
			}
		}
		if address, err = s.registryAddress(ctx, name, network); err != nil {
			return localRegistry{}, err
		}
	}

	if err := waitHTTP(ctx, "http://"+address.pushHost+"/v2/", 30*time.Second); err != nil {
		return localRegistry{}, fmt.Errorf("the registry does not answer: %w", err)
	}
	// The ocm and helm CLIs publish to the address the cluster uses, so
	// it must be routable from this machine too.
	if err := waitHTTP(ctx, address.url+"/v2/", 10*time.Second); err != nil {
		return localRegistry{}, fmt.Errorf("the registry at %s is not reachable from this machine: container addresses must be routable from the host, as they are on Linux and with OrbStack (Docker Desktop does not route to containers): %w", address.url, err)
	}
	return address, nil
}

func (s *snapshot) startRegistry(ctx context.Context, name, network string, extra ...string) error {
	args := slices.Concat(
		[]string{"run", "-d", "--restart=always", "--name", name, "--network", network},
		extra,
		[]string{"-v", name + "-data:/var/lib/registry", "-p", "127.0.0.1::" + registryPort, registryImage},
	)
	return s.run(ctx, "", nil, "docker", args...)
}

// registryAddress reads the addresses of the running registry container.
func (s *snapshot) registryAddress(ctx context.Context, name, network string) (localRegistry, error) {
	format := fmt.Sprintf(`{{.State.Running}} {{(index .NetworkSettings.Networks %q).IPAddress}} {{(index (index .NetworkSettings.Ports "%s/tcp") 0).HostPort}}`, network, registryPort)
	out, err := s.output(ctx, "", nil, "docker", "inspect", "-f", format, name)
	if err != nil {
		return localRegistry{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 3 || fields[0] != "true" {
		return localRegistry{}, fmt.Errorf("registry container %s is not running", name)
	}
	return localRegistry{
		url:      "http://" + fields[1] + ":" + registryPort,
		pushHost: "127.0.0.1:" + fields[2],
	}, nil
}

// registryIP picks the address to pin the registry to: the second-to-last
// host address of the network's IPv4 subnet, away from the addresses docker
// hands out from the start of it.
func (s *snapshot) registryIP(ctx context.Context, network string) (string, error) {
	out, err := s.output(ctx, "", nil, "docker", "network", "inspect", network, "-f", "{{range .IPAM.Config}}{{.Subnet}} {{end}}")
	if err != nil {
		return "", err
	}
	for _, subnet := range strings.Fields(out) {
		prefix, err := netip.ParsePrefix(subnet)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 29 {
			continue
		}
		base := prefix.Masked().Addr().As4()
		n := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
		// The last address is the broadcast address, the one before the last host.
		n += uint32(1)<<(32-prefix.Bits()) - 3
		return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}).String(), nil
	}
	return "", errors.New("network " + network + " has no IPv4 subnet")
}

// waitHTTP waits until a GET of url is answered with 200.
func waitHTTP(ctx context.Context, url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	var last error
	err := wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			return false, nil //nolint:nilerr // retried, the error is kept
		}
		_ = resp.Body.Close()
		last = fmt.Errorf("%s: %s", url, resp.Status)
		return resp.StatusCode == http.StatusOK, nil
	})
	if err != nil && last != nil {
		return last
	}
	return err
}

// configureNodes makes containerd on every node of the cluster pull from the
// registry over plain HTTP, with a hosts.toml for its address. The certs.d
// directory is containerd's default in kind nodes and is read on every pull,
// so nothing is restarted.
func (o *Options) configureNodes(registry localRegistry) error {
	provider, err := newKindProvider(o.ContainerRuntime)
	if err != nil {
		return err
	}
	nodes, err := provider.ListNodes(o.Name)
	if err != nil {
		return fmt.Errorf("listing the nodes of kind cluster %q: %w", o.Name, err)
	}
	dir := "/etc/containerd/certs.d/" + registry.host()
	hosts := fmt.Sprintf("server = %[1]q\n\n[host.%[1]q]\n  capabilities = [\"pull\", \"resolve\"]\n  skip_verify = true\n", registry.url)
	for _, node := range nodes {
		if err := node.Command("mkdir", "-p", dir).Run(); err != nil {
			return fmt.Errorf("configuring the registry on node %s: %w", node, err)
		}
		if err := node.Command("cp", "/dev/stdin", dir+"/hosts.toml").SetStdin(strings.NewReader(hosts)).Run(); err != nil {
			return fmt.Errorf("configuring the registry on node %s: %w", node, err)
		}
	}
	return nil
}

// removeRegistry removes the registry container and its volume. Neither
// existing is not an error.
func removeRegistry(ctx context.Context, cluster string) {
	name := registryName(cluster)
	_ = runQuiet(ctx, "docker", "rm", "-f", name)
	_ = runQuiet(ctx, "docker", "volume", "rm", "-f", name+"-data")
}
