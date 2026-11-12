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

// Package restore puts a backup archive back into a Platform Mesh with the same topology.
// The steps are: preflight, stop, secrets, certificates, etcd, start.
package restore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/kcp"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/pki"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Part is one part of a backup that can be restored.
type Part string

const (
	PartSecrets      Part = "secrets"
	PartCertificates Part = "certificates"
	PartEtcd         Part = "etcd"
)

// AllParts are restored when no part is given.
var AllParts = []Part{PartSecrets, PartCertificates, PartEtcd}

// ParseParts checks the names and returns the parts in restore order.
func ParseParts(names []string) ([]Part, error) {
	if len(names) == 0 {
		return AllParts, nil
	}
	for _, n := range names {
		if !slices.Contains(AllParts, Part(n)) {
			return nil, fmt.Errorf("unknown part %q, use %v", n, AllParts)
		}
	}
	var out []Part
	for _, p := range AllParts {
		if slices.Contains(names, string(p)) {
			out = append(out, p)
		}
	}
	return out, nil
}

// ReplicasAnnotation keeps the replicas of a Deployment or Etcd that the restore scaled to zero.
// A restore that runs again after a failure uses it to scale back.
const ReplicasAnnotation = "backup.platform-mesh.io/replicas"

type Options struct {
	Namespace            string
	KcpOperator          string
	PlatformMeshOperator string
	Parts                []Part
	// SettleTime is how long restored secrets must stay unchanged. Default 10s.
	SettleTime time.Duration
	// Timeout of each step. Default 30m.
	Timeout time.Duration
	// Wait is called between two checks of a step. Default: sleep 5s.
	Wait func(ctx context.Context) error
	Log  func(format string, args ...any)
}

type Restore struct {
	cl     ctrlruntimeclient.Client
	stores storage.Factory
	x      *archive.Extracted
	opts   Options
	bundle *pki.Bundle
	// id names the copy of the druid store from before the restore.
	id        string
	etcdSteps map[string]etcdStep
}

func New(cl ctrlruntimeclient.Client, stores storage.Factory, x *archive.Extracted, opts Options) *Restore {
	if opts.SettleTime <= 0 {
		opts.SettleTime = 10 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}
	if opts.Wait == nil {
		opts.Wait = func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	if len(opts.Parts) == 0 {
		opts.Parts = AllParts
	}
	return &Restore{cl: cl, stores: stores, x: x, opts: opts, id: time.Now().UTC().Format("20060102-150405")}
}

func (r *Restore) has(p Part) bool { return slices.Contains(r.opts.Parts, p) }

// Run restores the parts. Nothing is changed when the preflight fails.
func (r *Restore) Run(ctx context.Context) error {
	if err := r.Preflight(ctx); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	r.opts.Log("preflight passed, backup %s from %s", r.x.Manifest.Backup, r.x.Manifest.CreatedAt.Format(time.RFC3339))

	steps := []struct {
		name string
		run  bool
		fn   func(context.Context) (bool, string, error)
	}{
		{"stop platform mesh", true, r.stop},
		{"restore secrets", r.has(PartSecrets), r.secrets},
		{"restore certificates", r.has(PartCertificates), r.certificates},
		{"restore etcd", r.has(PartEtcd), r.etcd},
		{"start kcp", true, r.startKcp},
		{"start platform mesh", true, r.startOthers},
	}
	for _, s := range steps {
		if !s.run {
			continue
		}
		r.opts.Log("%s", s.name)
		if err := r.until(ctx, s.name, s.fn); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	r.opts.Log("restore of %v completed", r.opts.Parts)
	return nil
}

// fatalError stops the restore, trying again does not help.
type fatalError struct{ msg string }

func (f fatalError) Error() string { return f.msg }

// until calls fn until it is done. Other errors than fatalError are logged and tried again.
func (r *Restore) until(ctx context.Context, name string, fn func(context.Context) (bool, string, error)) error {
	ctx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	last := ""
	for {
		done, msg, err := fn(ctx)
		var fe fatalError
		if errors.As(err, &fe) {
			return err
		}
		if err != nil {
			msg = "error, trying again: " + err.Error()
		}
		if done {
			return nil
		}
		if msg != last {
			r.opts.Log("  %s", msg)
			last = msg
		}
		if err := r.opts.Wait(ctx); err != nil {
			return fmt.Errorf("timeout, last state: %s", last)
		}
	}
}

// Preflight checks the archive against the cluster: namespace, kcp topology and etcd stores.
func (r *Restore) Preflight(ctx context.Context) error {
	m := r.x.Manifest
	if m.Namespace != r.opts.Namespace {
		return fmt.Errorf("backup is from namespace %s", m.Namespace)
	}
	if r.has(PartSecrets) || r.has(PartCertificates) {
		data, err := r.x.ReadAll(manifest.PKIEntry)
		if err != nil {
			return err
		}
		r.bundle = &pki.Bundle{}
		if err := json.Unmarshal(data, r.bundle); err != nil {
			return fmt.Errorf("parsing pki: %w", err)
		}
	}
	current, err := kcp.Topology(ctx, r.cl, r.opts.Namespace)
	if err != nil {
		return err
	}
	if msg := compareTopology(m.Topology, current); msg != "" {
		return errors.New("kcp topology is not the same as in the backup: " + msg)
	}
	if err := r.cl.Get(ctx, types.NamespacedName{Namespace: r.opts.Namespace, Name: r.opts.KcpOperator}, &appsv1.Deployment{}); err != nil {
		return fmt.Errorf("kcp-operator deployment %s: %w", r.opts.KcpOperator, err)
	}
	if r.has(PartEtcd) {
		var etcds []druidv1alpha1.Etcd
		for _, e := range m.Etcds {
			etcd := &druidv1alpha1.Etcd{}
			err := r.cl.Get(ctx, types.NamespacedName{Namespace: r.opts.Namespace, Name: e.Name}, etcd)
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("etcd %s from the backup does not exist", e.Name)
			}
			if err != nil {
				return err
			}
			if err := storage.CheckEtcdStore(etcd); err != nil {
				return err
			}
			if etcd.Spec.Backup.Store.Prefix != e.StorePrefix {
				return fmt.Errorf("etcd %s: store prefix is %s, the backup has %s", e.Name, etcd.Spec.Backup.Store.Prefix, e.StorePrefix)
			}
			etcds = append(etcds, *etcd)
		}
		if err := storage.CheckEtcdStorePrefixes(etcds); err != nil {
			return err
		}
	}
	return nil
}

func compareTopology(want, got manifest.Topology) string {
	var diffs []string
	for _, c := range []struct {
		kind      string
		want, got []string
	}{
		{"RootShard", want.RootShards, got.RootShards},
		{"Shard", want.Shards, got.Shards},
		{"CacheServer", want.CacheServers, got.CacheServers},
		{"FrontProxy", want.FrontProxies, got.FrontProxies},
	} {
		w, g := slices.Clone(c.want), slices.Clone(c.got)
		slices.Sort(w)
		slices.Sort(g)
		if !slices.Equal(w, g) {
			diffs = append(diffs, fmt.Sprintf("%s: backup has %v, cluster has %v", c.kind, w, g))
		}
	}
	return strings.Join(diffs, ", ")
}
