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

package accessgrant

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	pmaccessproto "go.platform-mesh.io/protocol/access"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// versionCache memoizes the picked handshake version per AccessURL.
// It is concurrency-safe and owns no logic beyond get/set/invalidate.
type versionCache struct {
	mu    sync.Mutex
	store map[string]string
}

func newVersionCache() *versionCache {
	return &versionCache{store: map[string]string{}}
}

func (c *versionCache) get(baseURL string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	version, ok := c.store[baseURL]
	return version, ok
}

func (c *versionCache) set(baseURL, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[baseURL] = version
}

// invalidate drops the cached version for baseURL. Safe to pass as a
// bare func(string) callback to dependents that only need to signal
// a dial failure without knowing about the cache itself.
func (c *versionCache) invalidate(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, baseURL)
}

// versionReconciler returns a mutually supported handshake version
// for a given AccessURL, probing /versions on cache miss.
type versionReconciler struct {
	verCache *versionCache
}

// reconcile returns the picked version. On probe failure it sets
// Connected=False/AccessURLUnreachable; on no overlap it sets
// Connected=False/UnsupportedProtocolVersion. Either stops the chain.
func (v *versionReconciler) reconcile(ctx context.Context, httpClient *http.Client, grant *pmaccessv1alpha1.AccessGrant) (string, ctrl.Result, bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	if cached, ok := v.verCache.get(grant.Spec.AccessURL); ok {
		return cached, ctrl.Result{}, false, nil
	}
	picked, serverVersions, err := negotiateVersion(ctx, httpClient, grant.Spec.AccessURL)
	if err != nil {
		log.Info().Err(err).Str("url", grant.Spec.AccessURL).Msg("version probe failed")
		setConnected(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason,
			fmt.Sprintf("probe %s/versions: %v", grant.Spec.AccessURL, err))
		return "", ctrl.Result{RequeueAfter: retryUnreachable}, true, nil
	}
	if picked == "" {
		setConnected(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantUnsupportedProtocolVersionReason,
			fmt.Sprintf("no mutually supported handshake version: provider offers %v, operator supports %v",
				serverVersions, pmaccessproto.SupportedVersions))
		return "", ctrl.Result{RequeueAfter: retryRejected}, true, nil
	}
	v.verCache.set(grant.Spec.AccessURL, picked)
	return picked, ctrl.Result{}, false, nil
}
