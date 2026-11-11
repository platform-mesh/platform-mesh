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
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmcli "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// helmClient installs charts into one namespace of the cluster behind a
// kubeconfig file, through the Helm SDK: no helm binary is needed.
type helmClient struct {
	kubeconfig string
	namespace  string
	settings   *helmcli.EnvSettings
	registry   *registry.Client
}

func newHelmClient(kubeconfig, namespace string) (*helmClient, error) {
	rc, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating the helm registry client: %w", err)
	}
	return &helmClient{kubeconfig: kubeconfig, namespace: namespace, settings: helmcli.New(), registry: rc}, nil
}

// config is created per call: the kubeconfig file does not exist before the
// cluster does.
func (h *helmClient) config() (*action.Configuration, error) {
	flags := genericclioptions.NewConfigFlags(false)
	flags.KubeConfig = &h.kubeconfig
	flags.Namespace = &h.namespace

	cfg := new(action.Configuration)
	if err := cfg.Init(flags, h.namespace, "secret", func(string, ...any) {}); err != nil {
		return nil, fmt.Errorf("configuring helm: %w", err)
	}
	cfg.RegistryClient = h.registry
	return cfg, nil
}

// latestVersion returns the highest final release of the chart in an OCI
// repository, or the highest prerelease if there is no final one.
func (h *helmClient) latestVersion(ref string) (string, error) {
	if !registry.IsOCI(ref) {
		return "", fmt.Errorf("%s is not an oci:// reference", ref)
	}
	// Tags returns the semantic versions only, highest first.
	tags, err := h.registry.Tags(strings.TrimPrefix(ref, "oci://"))
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no versions found in %s", ref)
	}
	for _, tag := range tags {
		if v, err := semver.NewVersion(tag); err == nil && v.Prerelease() == "" {
			return tag, nil
		}
	}
	return tags[0], nil
}

// lastRelease returns the latest revision of a release, or nil if there is none.
func (h *helmClient) lastRelease(name string) (*release.Release, error) {
	cfg, err := h.config()
	if err != nil {
		return nil, err
	}
	rel, err := cfg.Releases.Last(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil
	}
	return rel, err
}

// installOrUpgrade installs the chart as the release, or upgrades the release
// if it exists. Values are the complete set: nothing is reused from the
// previous revision. With wait it returns once the resources are ready.
func (h *helmClient) installOrUpgrade(ctx context.Context, name, ref, version string, values map[string]any, wait bool, timeout time.Duration) error {
	cfg, err := h.config()
	if err != nil {
		return err
	}

	install := action.NewInstall(cfg)
	install.Version = version
	chrt, err := h.load(install, ref)
	if err != nil {
		return err
	}

	last, err := h.lastRelease(name)
	if err != nil {
		return err
	}
	if last == nil {
		install.ReleaseName = name
		install.Namespace = h.namespace
		install.CreateNamespace = true
		install.Wait = wait
		install.Timeout = timeout
		if _, err := install.RunWithContext(ctx, chrt, values); err != nil {
			return fmt.Errorf("installing %s: %w", name, err)
		}
		return nil
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = h.namespace
	upgrade.Wait = wait
	upgrade.Timeout = timeout
	if _, err := upgrade.RunWithContext(ctx, name, chrt, values); err != nil {
		return fmt.Errorf("upgrading %s: %w", name, err)
	}
	return nil
}

// load pulls the chart from an OCI reference, or reads it from a local path.
func (h *helmClient) load(install *action.Install, ref string) (*chart.Chart, error) {
	path, err := install.LocateChart(ref, h.settings)
	if err != nil {
		return nil, fmt.Errorf("locating chart %s %s: %w", ref, install.Version, err)
	}
	chrt, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading chart %s: %w", ref, err)
	}
	return chrt, nil
}
