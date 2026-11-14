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

package restore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.platform-mesh.io/backup-operator/pkg/kcp"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// toStop returns platform-mesh-operator and kcp-operator first, so they do not start the others
// again, then the kcp Deployments and the kcp clients.
func (r *Restore) toStop(ctx context.Context) ([]appsv1.Deployment, error) {
	var out []appsv1.Deployment
	seen := map[string]bool{}
	add := func(d appsv1.Deployment) {
		if !seen[d.Name] {
			seen[d.Name] = true
			out = append(out, d)
		}
	}
	for _, name := range []string{r.opts.PlatformMeshOperator, r.opts.KcpOperator} {
		d := &appsv1.Deployment{}
		err := r.cl.Get(ctx, types.NamespacedName{Namespace: r.opts.Namespace, Name: name}, d)
		if apierrors.IsNotFound(err) && name == r.opts.PlatformMeshOperator {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("getting deployment %s: %w", name, err)
		}
		add(*d)
	}
	deps, err := kcp.Deployments(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return nil, err
	}
	clients, err := kcp.Clients(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return nil, err
	}
	for _, d := range append(deps, clients...) {
		add(d)
	}
	return out, nil
}

// stop scales the Deployments to zero and keeps their replicas in an annotation.
func (r *Restore) stop(ctx context.Context) (bool, string, error) {
	deps, err := r.toStop(ctx)
	if err != nil {
		return false, "", err
	}
	running := []string{}
	for i := range deps {
		d := &deps[i]
		if _, ok := d.Annotations[ReplicasAnnotation]; !ok || ptr.Deref(d.Spec.Replicas, 1) != 0 {
			if err := scaleDown(ctx, r.cl, d); err != nil {
				return false, "", err
			}
		}
		if d.Status.Replicas > 0 {
			running = append(running, d.Name)
		}
	}
	if len(running) > 0 {
		return false, fmt.Sprintf("waiting for %d deployments to stop: %s", len(running), strings.Join(running, ", ")), nil
	}
	return true, "", nil
}

func scaleDown(ctx context.Context, cl ctrlruntimeclient.Client, d *appsv1.Deployment) error {
	patch := ctrlruntimeclient.MergeFrom(d.DeepCopy())
	if _, ok := d.Annotations[ReplicasAnnotation]; !ok {
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[ReplicasAnnotation] = strconv.Itoa(int(ptr.Deref(d.Spec.Replicas, 1)))
	}
	d.Spec.Replicas = ptr.To[int32](0)
	if err := cl.Patch(ctx, d, patch); err != nil {
		return fmt.Errorf("stopping deployment %s: %w", d.Name, err)
	}
	return nil
}

// scaleBack sets the replicas from the annotation and removes it.
func scaleBack(ctx context.Context, cl ctrlruntimeclient.Client, d *appsv1.Deployment) error {
	v, ok := d.Annotations[ReplicasAnnotation]
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fatalError{fmt.Sprintf("deployment %s: bad annotation %s=%q", d.Name, ReplicasAnnotation, v)}
	}
	patch := ctrlruntimeclient.MergeFrom(d.DeepCopy())
	delete(d.Annotations, ReplicasAnnotation)
	d.Spec.Replicas = ptr.To(int32(n))
	if err := cl.Patch(ctx, d, patch); err != nil {
		return fmt.Errorf("starting deployment %s: %w", d.Name, err)
	}
	return nil
}

func stopped(ctx context.Context, cl ctrlruntimeclient.Client, namespace string) ([]appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	if err := cl.List(ctx, list, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	var out []appsv1.Deployment
	for _, d := range list.Items {
		if _, ok := d.Annotations[ReplicasAnnotation]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func ready(d *appsv1.Deployment) bool {
	n := ptr.Deref(d.Spec.Replicas, 1)
	return d.Status.ObservedGeneration >= d.Generation && d.Status.ReadyReplicas >= n && d.Status.UpdatedReplicas >= n
}

// startKcp starts the kcp Deployments and kcp-operator, and waits until the shards are running.
func (r *Restore) startKcp(ctx context.Context) (bool, string, error) {
	deps, err := stopped(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return false, "", err
	}
	for i := range deps {
		if kcp.IsOwned(&deps[i]) || deps[i].Name == r.opts.KcpOperator {
			if err := scaleBack(ctx, r.cl, &deps[i]); err != nil {
				return false, "", err
			}
		}
	}

	all, err := kcp.Deployments(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return false, "", err
	}
	op := appsv1.Deployment{}
	if err := r.cl.Get(ctx, types.NamespacedName{Namespace: r.opts.Namespace, Name: r.opts.KcpOperator}, &op); err != nil {
		return false, "", err
	}
	for _, d := range append(all, op) {
		if !ready(&d) {
			return false, "waiting for deployment " + d.Name, nil
		}
	}
	for _, kind := range []string{"RootShard", "Shard"} {
		items, err := kcp.List(ctx, r.cl, r.opts.Namespace, kind)
		if err != nil {
			return false, "", err
		}
		for _, it := range items {
			if phase, _, _ := unstructured.NestedString(it.Object, "status", "phase"); phase != "Running" {
				return false, fmt.Sprintf("waiting for %s %s", kind, it.GetName()), nil
			}
		}
	}
	return true, "", nil
}

// startOthers starts the kcp clients and platform-mesh-operator, after kcp runs again.
func (r *Restore) startOthers(ctx context.Context) (bool, string, error) {
	deps, err := stopped(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return false, "", err
	}
	for i := range deps {
		if err := scaleBack(ctx, r.cl, &deps[i]); err != nil {
			return false, "", err
		}
	}
	toStart, err := r.toStop(ctx)
	if err != nil {
		return false, "", err
	}
	for _, d := range toStart {
		if !ready(&d) {
			return false, "waiting for deployment " + d.Name, nil
		}
	}
	return true, "", nil
}
