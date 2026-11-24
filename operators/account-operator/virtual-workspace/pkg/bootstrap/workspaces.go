/*
Copyright 2026 The kcp Authors.

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

package bootstrap

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// The APIExport lands in <prefix>:<leaf>, both configurable so a deployment
// can bring its own tree. The default is root:access:controllers.
const (
	// DefaultWorkspacePrefix is the parent path the leaf is created under.
	DefaultWorkspacePrefix = "root:access"

	// DefaultControllersWorkspace is the leaf workspace this component owns.
	DefaultControllersWorkspace = "controllers"
)

// JoinWorkspacePath composes a prefix and leaf into an absolute path,
// tolerating a prefix given with kubectl-ws style leading colons.
func JoinWorkspacePath(prefix, leaf string) (string, error) {
	if leaf == "" {
		return "", fmt.Errorf("controllers workspace name must not be empty")
	}
	if strings.Contains(leaf, ":") {
		return "", fmt.Errorf("controllers workspace name %q must be a single segment, not a path", leaf)
	}
	trimmed := strings.Trim(prefix, ":")
	if trimmed == "" {
		return "", fmt.Errorf("workspace prefix must not be empty")
	}
	return trimmed + ":" + leaf, nil
}

// DefaultWorkspaceType is the WorkspaceType used for workspaces this
// bootstrap creates. "universal" is the plain, unopinionated type; a
// deployment wanting something else passes --workspace-type.
const DefaultWorkspaceType = "universal"

var workspaceGVR = schema.GroupVersionResource{
	Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces",
}

// ParseWorkspacePath normalises a kcp workspace path, accepting both the
// kubectl-ws style ":root:access:magic" and the plain "root:access:magic".
// It returns the segments, of which the first must be "root".
func ParseWorkspacePath(path string) ([]string, error) {
	trimmed := strings.Trim(path, ":")
	if trimmed == "" {
		return nil, fmt.Errorf("workspace path is empty")
	}

	segments := strings.Split(trimmed, ":")
	if slices.Contains(segments, "") {
		return nil, fmt.Errorf("workspace path %q has an empty segment", path)
	}
	if segments[0] != "root" {
		return nil, fmt.Errorf("workspace path %q must start at root", path)
	}
	return segments, nil
}

// CreateWorkspacePath creates every workspace along path that does not exist
// yet and returns a config addressing the leaf. Requires rights to create
// workspaces from root down.
func CreateWorkspacePath(ctx context.Context, cfg *rest.Config, path, workspaceType string) (*rest.Config, error) {
	segments, err := ParseWorkspacePath(path)
	if err != nil {
		return nil, err
	}
	if workspaceType == "" {
		workspaceType = DefaultWorkspaceType
	}

	logger := klog.FromContext(ctx)

	base, err := baseURL(cfg.Host)
	if err != nil {
		return nil, err
	}

	current := segments[0] // root always exists

	for _, name := range segments[1:] {
		parent := configForCluster(cfg, base, current)

		if err := createWorkspace(ctx, parent, name, workspaceType); err != nil {
			return nil, fmt.Errorf("create workspace %q under %q: %w", name, current, err)
		}
		if err := waitForWorkspaceReady(ctx, parent, name); err != nil {
			return nil, fmt.Errorf("workspace %q under %q did not become ready: %w", name, current, err)
		}

		current += ":" + name
		logger.Info("workspace ready", "path", current)
	}

	return configForCluster(cfg, base, current), nil
}

func baseURL(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("parse kubeconfig host %q: %w", host, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("kubeconfig host %q has no scheme or host", host)
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String(), nil
}

func configForCluster(cfg *rest.Config, base, clusterPath string) *rest.Config {
	out := rest.CopyConfig(cfg)
	out.Host = base + "/clusters/" + clusterPath
	return out
}

func createWorkspace(ctx context.Context, parent *rest.Config, name, workspaceType string) error {
	logger := klog.FromContext(ctx)

	client, err := dynamic.NewForConfig(parent)
	if err != nil {
		return fmt.Errorf("build dynamic client: %w", err)
	}

	ws := &unstructured.Unstructured{}
	ws.SetGroupVersionKind(schema.GroupVersionKind{
		Group: workspaceGVR.Group, Version: workspaceGVR.Version, Kind: "Workspace",
	})
	ws.SetName(name)
	if err := unstructured.SetNestedMap(ws.Object, map[string]any{"name": workspaceType}, "spec", "type"); err != nil {
		return fmt.Errorf("set workspace type: %w", err)
	}

	if _, err := client.Resource(workspaceGVR).Create(ctx, ws, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(2).Info("workspace already exists", "name", name)
			return nil
		}
		return err
	}

	logger.Info("created workspace", "name", name, "type", workspaceType)
	return nil
}

func waitForWorkspaceReady(ctx context.Context, parent *rest.Config, name string) error {
	logger := klog.FromContext(ctx)

	client, err := dynamic.NewForConfig(parent)
	if err != nil {
		return fmt.Errorf("build dynamic client: %w", err)
	}

	return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		ws, err := client.Resource(workspaceGVR).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		phase, _, _ := unstructured.NestedString(ws.Object, "status", "phase")
		if phase == "Ready" {
			return true, nil
		}
		logger.V(2).Info("waiting for workspace", "name", name, "phase", phase)
		return false, nil
	})
}
