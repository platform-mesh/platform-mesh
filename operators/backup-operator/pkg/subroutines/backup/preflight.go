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

package backup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/druid"
	"go.platform-mesh.io/backup-operator/pkg/kcp"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Preflight checks the storage, the etcd clusters and kcp once, before anything is written.
type Preflight struct {
	cl   ctrlruntimeclient.Client
	dest *Destination
}

func (p *Preflight) GetName() string { return "Preflight" }

func (p *Preflight) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	b := obj.(*pmbackupv1alpha1.Backup)
	if b.Status.Archive != "" {
		return subroutines.OK(), nil
	}

	target, err := p.dest.For(ctx, b)
	if errors.As(err, &invalidStorage{}) {
		return fail(b, err.Error())
	}
	if err != nil {
		return subroutines.OK(), err
	}

	etcds := &druidv1alpha1.EtcdList{}
	if err := p.cl.List(ctx, etcds, ctrlruntimeclient.InNamespace(b.Namespace)); err != nil {
		return subroutines.OK(), fmt.Errorf("listing etcds: %w", err)
	}
	if len(etcds.Items) == 0 {
		return fail(b, "no etcd-druid Etcd found in namespace "+b.Namespace)
	}
	sort.Slice(etcds.Items, func(i, j int) bool { return etcds.Items[i].Name < etcds.Items[j].Name })

	rootShards, err := kcp.List(ctx, p.cl, b.Namespace, "RootShard")
	if err != nil {
		return subroutines.OK(), err
	}
	if len(rootShards) == 0 {
		return fail(b, "no kcp RootShard found in namespace "+b.Namespace)
	}
	for _, rs := range rootShards {
		if phase, _, _ := unstructured.NestedString(rs.Object, "status", "phase"); phase != "Running" {
			return fail(b, fmt.Sprintf("RootShard %s is not running (phase %q)", rs.GetName(), phase))
		}
	}
	shards, err := kcp.List(ctx, p.cl, b.Namespace, "Shard")
	if err != nil {
		return subroutines.OK(), err
	}
	shardEtcds, err := etcdsOfShards(etcds.Items, append(rootShards, shards...))
	if err != nil {
		return fail(b, err.Error())
	}

	// The data of an etcd without store that no shard uses can be rebuilt, like the cache server's.
	var included []druidv1alpha1.Etcd
	var skipped []string
	for i := range etcds.Items {
		e := &etcds.Items[i]
		if err := storage.CheckEtcdStore(e); err != nil {
			if shard, ok := shardEtcds[e.Name]; ok {
				return fail(b, fmt.Sprintf("%s, it is the etcd of %s", err, shard))
			}
			skipped = append(skipped, e.Name)
			continue
		}
		if !druid.IsReady(e) {
			return fail(b, fmt.Sprintf("etcd %s is not ready", e.Name))
		}
		included = append(included, *e)
	}
	if err := storage.CheckEtcdStorePrefixes(included); err != nil {
		return fail(b, err.Error())
	}

	key := manifest.ArchiveKey(target.Prefix, b.Namespace, b.Name)
	existing, err := target.Store.Get(ctx, key)
	switch {
	case err == nil:
		_ = existing.Close()
		return fail(b, "a backup already exists at "+key)
	case !errors.Is(err, storage.ErrNotFound):
		return subroutines.OK(), fmt.Errorf("checking backup archive: %w", err)
	}

	b.Status.Archive = key
	b.Status.Etcds = nil
	for _, e := range included {
		b.Status.Etcds = append(b.Status.Etcds, pmbackupv1alpha1.EtcdSnapshot{Name: e.Name, Task: TaskName(b.Name, e.Name)})
	}
	b.Status.Message = "preflight passed"
	if len(skipped) > 0 {
		b.Status.Message += ", etcds without backup store skipped: " + strings.Join(skipped, ", ")
	}
	return subroutines.OK(), nil
}

// etcdsOfShards maps each Etcd that a RootShard or Shard uses to that shard. kcp reaches the Etcd
// through its client service.
func etcdsOfShards(etcds []druidv1alpha1.Etcd, shards []unstructured.Unstructured) (map[string]string, error) {
	byService := map[string]string{}
	for _, e := range etcds {
		byService[druidv1alpha1.GetClientServiceName(e.ObjectMeta)] = e.Name
	}
	out := map[string]string{}
	for _, s := range shards {
		shard := s.GetKind() + " " + s.GetName()
		endpoints, _, _ := unstructured.NestedStringSlice(s.Object, "spec", "etcd", "endpoints")
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("%s has no etcd endpoints", shard)
		}
		for _, ep := range endpoints {
			u, err := url.Parse(ep)
			if err != nil || u.Hostname() == "" {
				return nil, fmt.Errorf("%s: etcd endpoint %q is not a URL", shard, ep)
			}
			host := strings.Split(u.Hostname(), ".")
			name, ok := byService[host[0]]
			if !ok || (len(host) > 1 && host[1] != s.GetNamespace()) {
				return nil, fmt.Errorf("%s uses etcd %s, it is not an etcd-druid Etcd in namespace %s", shard, ep, s.GetNamespace())
			}
			out[name] = shard
		}
	}
	return out, nil
}
