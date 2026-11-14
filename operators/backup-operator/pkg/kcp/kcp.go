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

// Package kcp knows which objects in the runtime cluster belong to the kcp installation of kcp-operator.
package kcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.platform-mesh.io/backup-operator/pkg/manifest"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Group is the API group of kcp-operator.
const Group = "operator.kcp.io"

// Kinds are the kcp-operator kinds that own the kcp workloads and certificates.
var Kinds = []string{"RootShard", "Shard", "CacheServer", "FrontProxy"}

// IsOwned is true when the object is owned by a RootShard, Shard, CacheServer or FrontProxy.
func IsOwned(obj metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if isKcpRef(ref) {
			return true
		}
	}
	return false
}

func isKcpRef(ref metav1.OwnerReference) bool {
	if !strings.HasPrefix(ref.APIVersion, Group+"/") {
		return false
	}
	for _, k := range Kinds {
		if ref.Kind == k {
			return true
		}
	}
	return false
}

// OwnerUID returns the uid of the owner, or "" when it does not exist.
func OwnerUID(ctx context.Context, cl ctrlruntimeclient.Client, namespace string, ref metav1.OwnerReference) (types.UID, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return "", err
	}
	owner := &unstructured.Unstructured{}
	owner.SetGroupVersionKind(gv.WithKind(ref.Kind))
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, owner); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting owner %s %s: %w", ref.Kind, ref.Name, err)
	}
	return owner.GetUID(), nil
}

// Deployments returns the kcp Deployments (shards, proxies, front-proxy, cache server), sorted by name.
func Deployments(ctx context.Context, cl ctrlruntimeclient.Client, namespace string) ([]appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	if err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	var out []appsv1.Deployment
	for _, d := range list.Items {
		if IsOwned(&d) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Clients returns the Deployments that are not part of kcp but mount a *-kubeconfig secret, sorted by name.
// These are the Platform Mesh controllers that keep connections to kcp.
func Clients(ctx context.Context, cl ctrlruntimeclient.Client, namespace string) ([]appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	if err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	var out []appsv1.Deployment
	for _, d := range list.Items {
		if IsOwned(&d) {
			continue
		}
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.Secret != nil && strings.HasSuffix(v.Secret.SecretName, "-kubeconfig") {
				out = append(out, d)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// List returns the kcp-operator objects of one kind, for example all Shards.
func List(ctx context.Context, cl ctrlruntimeclient.Client, namespace, kind string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: Group, Version: "v1alpha1", Kind: kind + "List"})
	if err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing %s: %w", kind, err)
	}
	return list.Items, nil
}

// Topology lists the kcp-operator objects. A restore needs the same ones.
func Topology(ctx context.Context, cl ctrlruntimeclient.Client, namespace string) (manifest.Topology, error) {
	var t manifest.Topology
	for kind, dst := range map[string]*[]string{
		"RootShard":   &t.RootShards,
		"Shard":       &t.Shards,
		"CacheServer": &t.CacheServers,
		"FrontProxy":  &t.FrontProxies,
	} {
		items, err := List(ctx, cl, namespace, kind)
		if err != nil {
			return t, err
		}
		names := []string{}
		for _, i := range items {
			names = append(names, i.GetName())
		}
		*dst = names
	}
	return t, nil
}
