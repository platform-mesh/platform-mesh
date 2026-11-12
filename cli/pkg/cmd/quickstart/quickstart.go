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

// Package quickstart implements "pmctl quickstart": a local Platform Mesh
// installation in a kind cluster, for demos and tests.
package quickstart

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
)

const (
	// namespace everything is installed into.
	namespace = "platform-mesh-system"

	// gatewayPort is the HTTPS port Platform Mesh is reached on, on the
	// host and inside the cluster. The kind profile of the operator chart
	// has it built in, so it is not configurable.
	gatewayPort = 8443
	// gatewayNodePort is the NodePort of the gateway Service; kind maps
	// 127.0.0.1:gatewayPort to it.
	gatewayNodePort = 31000

	runtimeAuto   = "auto"
	runtimeDocker = "docker"
)

var long = `Create a local Platform Mesh installation for demos and tests.

quickstart creates a kind cluster on the local container runtime (docker,
podman or nerdctl) and installs the given Platform Mesh release into it, the
way a user installs it: the platform-mesh-prerequisites and
platform-mesh-operator charts with the kind profile. The operator then rolls
out the release's OCM component.

Running it again against an existing cluster upgrades the installation to the
given version, and applies that version's kind profile.

With --dev the installation is built from a checkout of the platform-mesh
repository (--git-root, default: the one of the current directory), whatever
is in its working tree: the images, the charts and the OCM components are
built with the checkout's own release tooling, published to a registry
container next to the cluster, and installed from there. Over an installed
release this is the upgrade test of that tree. It needs docker (no other
container runtime builds the images yet), go and task, and container addresses
that are routable from the host (Linux, OrbStack). The kubeconfig of the
cluster is written to a file of its own;
~/.kube/config is not touched.`

var example = `  # Install the latest release
  pmctl quickstart

  # Install a specific release
  pmctl quickstart --version=v26.10.2

  # Upgrade the installation to the current checkout
  pmctl quickstart --dev

  # Use podman
  pmctl quickstart --container-runtime=podman

  # Remove the installation
  pmctl quickstart delete`

// ClusterOptions identifies the kind cluster; create and delete share them.
type ClusterOptions struct {
	// Name of the kind cluster.
	Name string
	// Kubeconfig is the file the kubeconfig of the cluster is written to.
	Kubeconfig string
	// ContainerRuntime is auto, docker, podman or nerdctl.
	ContainerRuntime string
}

// AddFlags registers the cluster flags.
func (o *ClusterOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Name, "name", o.Name, "Name of the kind cluster.")
	cmd.Flags().StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "File the kubeconfig of the cluster is written to (default \"<name>.kubeconfig\").")
	cmd.Flags().StringVar(&o.ContainerRuntime, "container-runtime", o.ContainerRuntime, "Container runtime kind uses. One of: auto|docker|podman|nerdctl.")
}

// Complete fills in the derived defaults.
func (o *ClusterOptions) Complete() {
	if o.Kubeconfig == "" {
		o.Kubeconfig = o.Name + ".kubeconfig"
	}
}

// Validate checks the cluster options.
func (o *ClusterOptions) Validate() error {
	if o.Name == "" {
		return errors.New("--name must not be empty")
	}
	switch o.ContainerRuntime {
	case runtimeAuto, runtimeDocker, "podman", "nerdctl":
		return nil
	default:
		return fmt.Errorf("--container-runtime must be one of auto|docker|podman|nerdctl, got %q", o.ContainerRuntime)
	}
}

// caFile is where the CA certificate of the installation is written to.
func (o *ClusterOptions) caFile() string {
	return o.Name + "-ca.crt"
}

// Options holds the flags and dependencies of "pmctl quickstart".
type Options struct {
	genericiooptions.IOStreams
	ClusterOptions

	// Version is the Platform Mesh release, vYY.MM.P; empty means latest.
	// With Dev it is the version of the build, once that is known.
	Version string
	// Dev builds the installation from the checkout at GitRoot instead of
	// installing a release.
	Dev bool
	// GitRoot is the platform-mesh checkout Dev builds.
	GitRoot string
	// ParallelBuilds is how many images Dev builds at a time.
	ParallelBuilds int
	// NodeImage overrides the kind node image.
	NodeImage string
	// BaseDomain is the DNS name Platform Mesh is served under. It and
	// its subdomains must resolve to 127.0.0.1 on the host.
	BaseDomain string
	// ChartRepository holds the Platform Mesh Helm charts: an oci://
	// reference, or a local directory of charts.
	ChartRepository string
	// OCMRepository is the OCI registry the operator reads the Platform
	// Mesh OCM components from.
	OCMRepository string
	// RegistrationAllowed lets users sign up in the portal.
	RegistrationAllowed bool
	// Timeout bounds the whole run.
	Timeout time.Duration
	// StallTimeout is how long a HelmRelease may keep failing before the
	// run gives up; zero waits for Timeout.
	StallTimeout time.Duration

	progress progress
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,
		ClusterOptions: ClusterOptions{
			Name:             "pm-quickstart",
			ContainerRuntime: runtimeAuto,
		},
		BaseDomain:          "pm.127-0-0-1.sslip.io",
		ChartRepository:     "oci://ghcr.io/platform-mesh/helm-charts",
		OCMRepository:       "ghcr.io/platform-mesh",
		RegistrationAllowed: true,
		ParallelBuilds:      2,
		Timeout:             45 * time.Minute,
		StallTimeout:        10 * time.Minute,
	}
}

// NewQuickstartCommand returns the "pmctl quickstart" command.
func NewQuickstartCommand(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)
	cmd := &cobra.Command{
		Use:     "quickstart",
		Short:   "Create a local Platform Mesh installation in a kind cluster",
		Long:    long,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	o.AddFlags(cmd)
	cmd.Flags().StringVar(&o.Version, "version", o.Version, "Platform Mesh release to install, e.g. v26.10.2 (default: the latest release).")
	cmd.Flags().BoolVar(&o.Dev, "dev", o.Dev, "Build and install the working tree of the checkout at --git-root instead of a release. Needs docker.")
	cmd.Flags().StringVar(&o.GitRoot, "git-root", o.GitRoot, "platform-mesh checkout that --dev builds (default: the repository of the current directory).")
	cmd.Flags().IntVar(&o.ParallelBuilds, "parallel-build", o.ParallelBuilds, "Number of images --dev builds at a time.")
	cmd.Flags().StringVar(&o.NodeImage, "node-image", o.NodeImage, "kind node image (default: the one of the built-in kind version).")
	cmd.Flags().StringVar(&o.BaseDomain, "base-domain", o.BaseDomain, "DNS name Platform Mesh is served under. It and its subdomains must resolve to 127.0.0.1.")
	cmd.Flags().StringVar(&o.ChartRepository, "chart-repository", o.ChartRepository, "Repository of the Platform Mesh Helm charts: an oci:// reference or a local directory.")
	cmd.Flags().StringVar(&o.OCMRepository, "ocm-repository", o.OCMRepository, "OCI registry the Platform Mesh OCM components are read from.")
	cmd.Flags().BoolVar(&o.RegistrationAllowed, "registration-allowed", o.RegistrationAllowed, "Let users sign up in the portal.")
	cmd.Flags().DurationVar(&o.Timeout, "timeout", o.Timeout, "Time to wait for the installation to become ready.")
	cmd.Flags().DurationVar(&o.StallTimeout, "stall-timeout", o.StallTimeout, "Give up when a HelmRelease has been failing for this long; 0 waits for --timeout.")

	cmd.MarkFlagsMutuallyExclusive("dev", "version")

	cmd.AddCommand(NewDeleteCommand(streams))
	return cmd
}

// Complete fills in the fields that are derived from the command line.
func (o *Options) Complete(_ *cobra.Command, _ []string) error {
	o.ClusterOptions.Complete()
	// Releases are tagged vYY.MM.P in git; charts and components carry
	// the version without the prefix.
	o.Version = strings.TrimPrefix(o.Version, "v")
	o.ChartRepository = strings.TrimSuffix(o.ChartRepository, "/")

	if o.Dev {
		dir := o.GitRoot
		if dir == "" {
			dir = "."
		}
		root, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return fmt.Errorf("--dev needs a platform-mesh checkout, and %q is not in a git repository (see --git-root)", dir)
		}
		o.GitRoot = strings.TrimSpace(string(root))

		// The build is docker only, and the registry it publishes to has
		// to sit next to the kind node. So kind does not get to pick
		// another runtime (KIND_EXPERIMENTAL_PROVIDER, or docker not
		// running); Validate fails when docker is not usable.
		if o.ContainerRuntime == runtimeAuto {
			o.ContainerRuntime = runtimeDocker
		}
	}
	return nil
}

// Validate checks the options.
func (o *Options) Validate() error {
	if err := o.ClusterOptions.Validate(); err != nil {
		return err
	}
	if o.BaseDomain == "" {
		return errors.New("--base-domain must not be empty")
	}
	if o.ChartRepository == "" {
		return errors.New("--chart-repository must not be empty")
	}
	if o.OCMRepository == "" {
		return errors.New("--ocm-repository must not be empty")
	}
	if o.StallTimeout < 0 {
		return errors.New("--stall-timeout must not be negative")
	}
	if o.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if o.ParallelBuilds < 1 {
		return errors.New("--parallel-build must be at least 1")
	}
	if !o.Dev {
		if o.GitRoot != "" {
			return errors.New("--git-root is only used with --dev")
		}
		return nil
	}
	if o.Version != "" {
		return errors.New("--dev builds the checkout and --version installs a release: use one of them")
	}
	if o.ContainerRuntime != runtimeDocker {
		return fmt.Errorf("--dev only works with docker for now, not %s", o.ContainerRuntime)
	}
	if _, err := os.Stat(filepath.Join(o.GitRoot, "ocm", "components.yaml")); err != nil {
		return fmt.Errorf("%s is not a platform-mesh checkout: %w", o.GitRoot, err)
	}
	for _, tool := range []string{"docker", "go", "task"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("--dev needs %s: %w", tool, err)
		}
	}
	return requireDocker()
}

// requireDocker fails unless the docker CLI is docker and reaches a docker
// daemon. --dev builds with "docker buildx" and runs its registry with
// docker; another runtime behind the name (podman-docker) or a stopped daemon
// would only fail later, and less clearly.
func requireDocker() error {
	out, err := exec.Command("docker", "version", "--format", "{{.Client.Version}} {{.Server.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("--dev needs a running docker daemon: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// podman answers for docker when podman-docker is installed.
	if out, err := exec.Command("docker", "--version").Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "podman") {
		return fmt.Errorf("--dev only works with docker for now, and the docker command is %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Run creates the cluster and installs Platform Mesh into it.
func (o *Options) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	helm, err := newHelmClient(o.Kubeconfig, namespace)
	if err != nil {
		return err
	}
	if o.Version == "" && !o.Dev {
		if o.Version, err = helm.latestVersion(o.chartRef(operatorChart)); err != nil {
			return fmt.Errorf("finding the latest release (use --version to name one): %w", err)
		}
	}

	if err := o.createCluster(); err != nil {
		return err
	}

	src := o.release()
	if o.Dev {
		if src, err = o.buildSnapshot(ctx); err != nil {
			return err
		}
		o.Version = src.version
	}

	_, _ = fmt.Fprintf(o.ErrOut, "Installing Platform Mesh %s ...\n", o.Version)
	if err := o.install(ctx, helm, src); err != nil {
		return err
	}
	if err := o.waitReady(ctx); err != nil {
		return err
	}

	o.printSummary(src)
	return nil
}

// url is the address of the portal.
func (o *Options) url() string {
	return "https://" + net.JoinHostPort(o.BaseDomain, strconv.Itoa(gatewayPort)) + "/"
}

// logf prints a debug line, shown with -v=1 or higher.
func (o *Options) logf(format string, args ...any) {
	if !klog.V(1).Enabled() {
		return
	}
	_, _ = fmt.Fprintf(o.ErrOut, ">>> "+format+"\n", args...)
}

func (o *Options) printSummary(src *source) {
	details := ""
	for _, line := range src.details {
		details += "  " + line + "\n"
	}
	_, _ = fmt.Fprintf(o.Out, `
Platform Mesh %[1]s is ready.

  Portal          %[2]s
  Keycloak admin  %[2]skeycloak/admin/
  kcp API         https://kcp.api.%[3]s:%[4]d

  Kubeconfig      %[5]s
  CA certificate  %[6]s
%[9]s
Next steps:

  export KUBECONFIG=%[5]s
  kubectl -n %[7]s get platformmeshes

The certificates are issued by a CA created for this installation: trust
%[6]s, or accept the browser warning.

Remove the installation with:

  pmctl quickstart delete --name %[8]s
`, o.Version, o.url(), o.BaseDomain, gatewayPort, o.Kubeconfig, o.caFile(), namespace, o.Name, details)
}
