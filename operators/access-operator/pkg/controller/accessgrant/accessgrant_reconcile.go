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
	"os"
	"time"

	"go.platform-mesh.io/access-operator/pkg/config"
	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// Retry cadences for the different non-terminal non-happy paths.
	retryUnreachable  = 30 * time.Second
	retryRejected     = 60 * time.Second
	retry202          = 10 * time.Second
	retryOther        = 30 * time.Second
	retrySATokenEmpty = 2 * time.Second

	// httpTimeout is the POST timeout per handshake attempt.
	httpTimeout = 30 * time.Second
)

// Reconciler drives the /access handshake for an AccessGrant.
type Reconciler struct {
	mgr        mcmanager.Manager
	httpClient *http.Client

	validate  *validateReconciler
	saToken   *saTokenReconciler
	version   *versionReconciler
	handshake *handshakeReconciler
}

// NewReconciler builds a Reconciler with production defaults. The
// options exposed via With* are for tests.
func NewReconciler(mgr mcmanager.Manager, cfg *config.OperatorConfig) *Reconciler {
	verCache := newVersionCache()
	return &Reconciler{
		mgr:        mgr,
		httpClient: &http.Client{Timeout: httpTimeout},
		validate:   &validateReconciler{},
		saToken:    &saTokenReconciler{mintTokenForServiceAccount: defaultSATokenMinter},
		version:    &versionReconciler{verCache: verCache},
		handshake: &handshakeReconciler{
			kcpExternalURL: cfg.KCP.ExternalUrl,
			readCABundle: func() ([]byte, error) {
				if cfg.KCP.CABundlePath == "" {
					return nil, nil
				}
				return os.ReadFile(cfg.KCP.CABundlePath)
			},
			invalidateVer: verCache.invalidate,
		},
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctx = mccontext.WithCluster(ctx, req.ClusterName)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("controller", "AccessGrant")
	ctx = logger.SetLoggerInContext(ctx, log)

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve cluster from context: %w", err)
	}
	cl := cluster.GetClient()

	grant := &pmaccessv1alpha1.AccessGrant{}
	if err := cl.Get(ctx, req.NamespacedName, grant); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get AccessGrant: %w", err)
	}
	if !grant.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Already connected — nothing to do until spec changes. A spec
	// change (e.g. fresh access token) bumps Generation and we re-run
	// the handshake.
	if isConnected(grant) && grant.Status.ObservedGeneration == grant.Generation {
		return ctrl.Result{}, nil
	}

	result, dialErr := r.doReconcile(ctx, cl, grant, string(req.ClusterName))
	grant.Status.ObservedGeneration = grant.Generation

	if err := cl.Status().Update(ctx, grant); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update AccessGrant status: %w", err)
	}
	return result, dialErr
}

// doReconcile composes the phase reconcilers. Each phase mutates the
// grant's status conditions in-place and may short-circuit the chain
// by returning done=true with a requeue cadence. Only the handshake
// phase returns the final Result directly.
func (r *Reconciler) doReconcile(ctx context.Context, cl ctrlruntimeclient.Client, grant *pmaccessv1alpha1.AccessGrant, clusterName string) (ctrl.Result, error) {
	parsed, res, done, err := r.validate.reconcile(ctx, cl, grant)
	if done || err != nil {
		return res, err
	}

	saToken, res, done, err := r.saToken.reconcile(ctx, cl, grant)
	if done || err != nil {
		return res, err
	}

	httpClient, err := httpClientFor(r.httpClient, grant.Spec.CABundle)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build HTTP client for /access: %w", err)
	}

	version, res, done, err := r.version.reconcile(ctx, httpClient, grant)
	if done || err != nil {
		return res, err
	}

	return r.handshake.reconcile(ctx, httpClient, grant, clusterName, version, parsed, saToken)
}

// isConnected reports whether the AccessGrant currently holds a
// Connected=True condition.
func isConnected(grant *pmaccessv1alpha1.AccessGrant) bool {
	c := apimeta.FindStatusCondition(grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	return c != nil && c.Status == metav1.ConditionTrue
}

func setValid(grant *pmaccessv1alpha1.AccessGrant, status metav1.ConditionStatus, reason, msg string) {
	setCondition(grant, pmaccessv1alpha1.AccessGrantConditionValid, status, reason, msg)
}

func setConnected(grant *pmaccessv1alpha1.AccessGrant, status metav1.ConditionStatus, reason, msg string) {
	setCondition(grant, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted, status, reason, msg)
}

func clearConnected(grant *pmaccessv1alpha1.AccessGrant) {
	apimeta.RemoveStatusCondition(&grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
}

func setCondition(grant *pmaccessv1alpha1.AccessGrant, condType string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&grant.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: grant.Generation,
	})
}
