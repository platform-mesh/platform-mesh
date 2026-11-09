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

// Package kcp provides the kcp instances shared by the integration tests,
// run in Docker via kcp-testcontainers.
//
//	instance, err := kcp.Default(t.Context())
//	path, err := instance.CreateWorkspace(t.Context(), "root:something-")
//	cl, err := instance.Client(path.String(), client.Options{Scheme: scheme.New(t)})
package kcp

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"

	"k8s.io/client-go/rest"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
)

// tag is the kcp release under test (KCP_VERSION).
var tag = cmp.Or(os.Getenv("KCP_VERSION"), kcptc.DefaultVersion)

// Image is the kcp image the integration tests run.
var Image = kcptc.DefaultImage + ":" + tag

const (
	startupTimeout = 5 * time.Minute

	frontProxyPort = "6443"
	shardPort      = "6444"
)

// Default returns the instance all integration tests share. A package that
// cannot share it adds a function like this one with another name.
func Default(ctx context.Context) (*kcptc.Instance, error) {
	return shared(ctx, "platform-mesh")
}

// Config returns the unscoped admin config of instance, verifying TLS
// against the URL's host rather than the front-proxy alias, for clients that
// follow the URLs kcp advertises (virtual workspaces live on the shards).
func Config(instance *kcptc.Instance) *rest.Config {
	cfg := instance.Config()
	if i := strings.Index(cfg.Host, "/clusters/"); i >= 0 {
		cfg.Host = cfg.Host[:i]
	}
	cfg.ServerName = ""
	return cfg
}

// RESTConfig returns Config scoped to the workspace at path.
func RESTConfig(instance *kcptc.Instance, path logicalcluster.Path) *rest.Config {
	cfg := Config(instance)
	cfg.Host += path.RequestPath()
	return cfg
}

// shared starts or joins the shared kcp named name. The operators run on the
// host and follow the URLs kcp advertises, so the front-proxy and the shards
// are published on fixed host ports and the shards advertise those instead
// of their Docker network aliases.
func shared(ctx context.Context, name string) (*kcptc.Instance, error) {
	host, err := daemonHost(ctx)
	if err != nil {
		return nil, err
	}
	ports, err := freePorts(3)
	if err != nil {
		return nil, err
	}
	frontProxy, shardA, shardB := ports[0], ports[1], ports[2]
	external := "https://" + net.JoinHostPort(host, strconv.Itoa(frontProxy))

	logDir, err := logDir(name)
	if err != nil {
		return nil, err
	}

	instance, err := kcptc.Shared(ctx, name, kcptc.Options{
		Image:          Image,
		StartupTimeout: startupTimeout,
		Cache: []testcontainers.ContainerCustomizer{
			logTo(logDir, kcptc.AliasCache),
		},
		FrontProxy: []testcontainers.ContainerCustomizer{
			publish(frontProxy, frontProxyPort),
			logTo(logDir, kcptc.AliasFrontProxy),
		},
		ShardA: []testcontainers.ContainerCustomizer{
			publish(shardA, shardPort),
			advertise(external, "https://"+net.JoinHostPort(host, strconv.Itoa(shardA))),
			logTo(logDir, kcptc.ShardA),
		},
		ShardB: []testcontainers.ContainerCustomizer{
			publish(shardB, shardPort),
			advertise(external, "https://"+net.JoinHostPort(host, strconv.Itoa(shardB))),
			logTo(logDir, kcptc.ShardB),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("starting kcp %q (%s) in Docker: %w", name, Image, err)
	}
	return instance, nil
}

// publish binds containerPort to a fixed hostPort; ExposedPorts only maps to
// random host ports and the port must be known before the shard starts.
func publish(hostPort int, containerPort string) testcontainers.CustomizeRequestOption {
	return testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
		hc.PortBindings = network.PortMap{
			network.MustParsePort(containerPort + "/tcp"): {{HostPort: strconv.Itoa(hostPort)}},
		}
	})
}

// advertise points a shard's --shard-external-url (workspace URLs, via the
// front-proxy) and --shard-virtual-workspace-url (APIExportEndpointSlice
// URLs, served by the shard itself) at the host side.
func advertise(externalURL, virtualWorkspaceURL string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		cmd := make([]string, 0, len(req.Cmd)+1)
		for _, arg := range req.Cmd {
			switch {
			case strings.HasPrefix(arg, "--shard-external-url="):
				arg = "--shard-external-url=" + externalURL
			case strings.HasPrefix(arg, "--shard-virtual-workspace-url="):
				continue
			}
			cmd = append(cmd, arg)
		}
		req.Cmd = append(cmd, "--shard-virtual-workspace-url="+virtualWorkspaceURL)
		return nil
	}
}

// logTo streams the container's output to <dir>/<component>.log.
func logTo(dir, component string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		f, err := os.Create(filepath.Join(dir, component+".log"))
		if err != nil {
			return fmt.Errorf("creating log file for %s: %w", component, err)
		}
		req.LogConsumerCfg = &testcontainers.LogConsumerConfig{Consumers: []testcontainers.LogConsumer{fileLog{f}}}
		return nil
	}
}

type fileLog struct{ f *os.File }

func (l fileLog) Accept(entry testcontainers.Log) {
	_, _ = l.f.Write(entry.Content)
}

// daemonHost is the host Docker publishes container ports on.
func daemonHost(ctx context.Context) (string, error) {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return "", fmt.Errorf("connecting to Docker: %w", err)
	}
	host, err := provider.DaemonHost(ctx)
	if err != nil {
		_ = provider.Close()
		return "", fmt.Errorf("resolving the Docker host: %w", err)
	}
	return host, provider.Close()
}

// freePorts picks n free TCP ports. They are released before Docker binds
// them; if something else grabs one in between the run fails and is retried.
func freePorts(n int) ([]int, error) {
	listeners := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	ports := make([]int, 0, n)
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("picking a free port: %w", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

// logDir is <repo>/tests/.test/<instance>.
func logDir(instance string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := findModuleRoot(cwd)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "tests", ".test", instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating log dir %s: %w", dir, err)
	}
	return dir, nil
}

func findModuleRoot(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
	}
}
