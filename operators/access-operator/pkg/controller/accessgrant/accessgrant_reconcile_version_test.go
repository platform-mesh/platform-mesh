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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	pmaccessproto "go.platform-mesh.io/protocol/access"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVersion_CacheHit_SkipsProbe(t *testing.T) {
	probed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probed = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	cache := newVersionCache()
	cache.set(srv.URL, pmaccessproto.V1)
	v := &versionReconciler{verCache: cache}

	picked, _, done, err := v.reconcile(testCtx(t),srv.Client(), fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if done {
		t.Errorf("expected done=false on cache hit")
	}
	if picked != pmaccessproto.V1 {
		t.Errorf("picked = %q, want %q", picked, pmaccessproto.V1)
	}
	if probed {
		t.Errorf("/versions must not be probed on cache hit")
	}
}

func TestVersion_ProbeSucceeds_PicksAndCaches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(pmaccessproto.VersionsPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pmaccessproto.VersionsResponse{Versions: []string{pmaccessproto.V1}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	cache := newVersionCache()
	v := &versionReconciler{verCache: cache}

	picked, _, done, err := v.reconcile(testCtx(t),srv.Client(), fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if done || picked != pmaccessproto.V1 {
		t.Errorf("picked=%q done=%v, want %q false", picked, done, pmaccessproto.V1)
	}
	if got, ok := cache.get(srv.URL); !ok || got != pmaccessproto.V1 {
		t.Errorf("cache[%s] = (%q,%v), want (%q,true)", srv.URL, got, ok, pmaccessproto.V1)
	}
}

func TestVersion_ProbeFails_SetsUnreachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(pmaccessproto.VersionsPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	v := &versionReconciler{verCache: newVersionCache()}

	_, res, done, err := v.reconcile(testCtx(t),srv.Client(), fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !done || res.RequeueAfter != retryUnreachable {
		t.Errorf("expected done=true RequeueAfter=%v, got done=%v res=%+v", retryUnreachable, done, res)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason {
		t.Errorf("Connected condition = %+v, want False/AccessURLUnreachable", cond)
	}
}

func TestVersion_NoOverlap_SetsUnsupported(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(pmaccessproto.VersionsPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pmaccessproto.VersionsResponse{Versions: []string{"v999"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	v := &versionReconciler{verCache: newVersionCache()}

	_, res, done, err := v.reconcile(testCtx(t),srv.Client(), fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !done || res.RequeueAfter != retryRejected {
		t.Errorf("expected done=true RequeueAfter=%v, got done=%v res=%+v", retryRejected, done, res)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Reason != pmaccessv1alpha1.AccessGrantUnsupportedProtocolVersionReason {
		t.Errorf("Connected condition = %+v, want UnsupportedProtocolVersion", cond)
	}
}

func TestVersionCache_Invalidate(t *testing.T) {
	c := newVersionCache()
	c.set("u", pmaccessproto.V1)
	if _, ok := c.get("u"); !ok {
		t.Fatalf("set+get: want hit")
	}
	c.invalidate("u")
	if _, ok := c.get("u"); ok {
		t.Errorf("invalidate: want miss")
	}
}
