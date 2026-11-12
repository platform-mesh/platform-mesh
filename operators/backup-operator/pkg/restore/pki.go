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
	"time"

	"go.platform-mesh.io/backup-operator/pkg/pki"
)

// secrets writes back the secrets of the kcp certificates. They are done when they still match
// the backup after the settle time, else they are written again.
func (r *Restore) secrets(ctx context.Context) (bool, string, error) {
	for attempt := 1; attempt <= 10; attempt++ {
		if err := pki.RestoreSecrets(ctx, r.cl, r.opts.Namespace, r.bundle); err != nil {
			return false, "", err
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(r.opts.SettleTime):
		}
		diff, err := pki.Settled(ctx, r.cl, r.opts.Namespace, r.bundle)
		if err != nil {
			return false, "", err
		}
		if diff == "" {
			r.opts.Log("  %d secrets restored", len(r.bundle.Secrets))
			return true, "", nil
		}
		r.opts.Log("  %s, writing the secrets again", diff)
	}
	return false, "", fatalError{"cert-manager keeps changing the restored secrets"}
}

// certificates creates the missing kcp Certificates, Issuers and Kubeconfigs.
func (r *Restore) certificates(ctx context.Context) (bool, string, error) {
	created, err := pki.RestoreCertificates(ctx, r.cl, r.opts.Namespace, r.bundle)
	if err != nil {
		return false, "", fmt.Errorf("restoring certificates: %w", err)
	}
	total := len(r.bundle.Certificates) + len(r.bundle.Issuers) + len(r.bundle.Kubeconfigs)
	r.opts.Log("  %d of %d certificates, issuers and kubeconfigs created, the others exist", created, total)
	return true, "", nil
}
