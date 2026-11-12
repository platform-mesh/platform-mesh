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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/kcp"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/pki"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Archive collects the kcp PKI and streams it with the etcd snapshots into one archive object.
// The object only exists when the upload is complete.
type Archive struct {
	cl     ctrlruntimeclient.Client
	stores storage.Factory
	open   Open
}

func (a *Archive) GetName() string { return "Archive" }

func (a *Archive) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	b := obj.(*pmbackupv1alpha1.Backup)

	bundle, err := pki.Collect(ctx, a.cl, b.Namespace)
	if err != nil {
		return fail(b, err.Error())
	}
	plain, err := json.Marshal(bundle)
	if err != nil {
		return subroutines.OK(), err
	}
	topology, err := kcp.Topology(ctx, a.cl, b.Namespace)
	if err != nil {
		return subroutines.OK(), err
	}
	opened, err := a.open(ctx, b)
	if err != nil {
		return subroutines.OK(), err
	}

	mf := &manifest.Manifest{
		Version:   manifest.Version,
		Backup:    b.Name,
		Namespace: b.Namespace,
		CreatedAt: b.Status.StartTime.UTC(),
		Topology:  topology,
		PKI: manifest.PKI{
			Size:         int64(len(plain)),
			Secrets:      len(bundle.Secrets),
			Certificates: len(bundle.Certificates) + len(bundle.Issuers),
		},
	}
	pr, pw := io.Pipe()
	written := make(chan error, 1)
	go func() {
		err := a.write(ctx, b, archive.NewWriter(pw, opened.Key), mf, plain)
		_ = pw.CloseWithError(err)
		written <- err
	}()
	err = opened.Store.Put(ctx, b.Status.Archive, pr, -1)
	_ = pr.CloseWithError(fmt.Errorf("upload stopped: %w", err))
	if werr := <-written; werr != nil {
		return subroutines.OK(), werr
	}
	if err != nil {
		return subroutines.OK(), fmt.Errorf("uploading archive: %w", err)
	}

	now := metav1.Now()
	b.Status.Phase = pmbackupv1alpha1.PhaseCompleted
	b.Status.CompletionTime = &now
	b.Status.Secrets = mf.PKI.Secrets
	b.Status.Certificates = mf.PKI.Certificates
	b.Status.Message = fmt.Sprintf("backup of %d etcd clusters and %d secrets completed", len(mf.Etcds), mf.PKI.Secrets)
	return subroutines.OK(), nil
}

func (a *Archive) write(ctx context.Context, b *pmbackupv1alpha1.Backup, w *archive.Writer, mf *manifest.Manifest, pkiData []byte) error {
	sum, err := w.Add(manifest.PKIEntry, bytes.NewReader(pkiData), int64(len(pkiData)))
	if err != nil {
		return err
	}
	mf.PKI.SHA256 = sum

	for _, s := range b.Status.Etcds {
		etcd := &druidv1alpha1.Etcd{}
		if err := a.cl.Get(ctx, types.NamespacedName{Namespace: b.Namespace, Name: s.Name}, etcd); err != nil {
			return err
		}
		src, prefix, err := a.stores.ForEtcd(ctx, a.cl, etcd)
		if err != nil {
			return err
		}
		e := manifest.Etcd{Name: s.Name, StorePrefix: prefix, Snapshot: path.Base(s.Snapshot), Size: s.Size}
		r, err := src.Get(ctx, s.Snapshot)
		if err != nil {
			return fmt.Errorf("reading snapshot of %s: %w", s.Name, err)
		}
		e.SHA256, err = w.Add(e.Entry(), r, s.Size)
		_ = r.Close()
		if err != nil {
			return err
		}
		mf.Etcds = append(mf.Etcds, e)
	}
	return w.Close(mf)
}
