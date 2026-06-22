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

package subroutine

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func NewWorkspaceInitializer(cfg config.Config, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, creatorRelation, objectType string, kcpHelper iclient.KCPClientGetter) *workspaceInitializer {
	// read file from path
	res, err := os.ReadFile(cfg.CoreModulePath)
	if err != nil {
		panic(err)
	}

	return &workspaceInitializer{
		kcpClientGetter: kcpClientGetter,
		coreModule:      string(res),
		initializerName: cfg.InitializerName(),
		mgr:             mgr,
		cfg:             cfg,
		creatorRelation: creatorRelation,
		objectType:      objectType,
	}
}

var (
	_ subroutines.Initializer = &workspaceInitializer{}
	_ subroutines.Processor   = &workspaceInitializer{}
)

type workspaceInitializer struct {
	mgr             mcmanager.Manager
	kcpClientGetter iclient.KCPClientGetter
	cfg             config.Config
	coreModule      string
	initializerName string

	objectType      string
	creatorRelation string
}

func (w *workspaceInitializer) GetName() string { return "WorkspaceInitializer" }

// Initialize implements subroutines.Initializer.
func (w *workspaceInitializer) Initialize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (w *workspaceInitializer) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return w.reconcile(ctx, obj)
}

func (w *workspaceInitializer) reconcile(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	cluster, err := w.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context: %w", err)
	}

	var ai corev1alpha1.AccountInfo
	if err := cluster.GetClient().Get(ctx, client.ObjectKey{
		Name: "account",
	}, &ai); err != nil && !kerrors.IsNotFound(err) {
		return subroutines.OK(), fmt.Errorf("getting AccountInfo for LogicalCluster: %w", err)
	} else if kerrors.IsNotFound(err) {
		return subroutines.StopWithRequeue(5*time.Second, "AccountInfo not found yet, requeueing"), nil
	}

	orgsClient, err := w.kcpClientGetter.NewClientForLogicalCluster(ctx, "root:orgs")
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
	}
	var acc corev1alpha1.Account
	if err := orgsClient.Get(ctx, client.ObjectKey{
		Name: ai.Spec.Account.Name,
	}, &acc); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account in platform-mesh-system: %w", err)
	}

	store := corev1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{Name: generateStoreName(lc)},
	}

	if acc.Spec.Creator == nil || *acc.Spec.Creator == "" {
		return subroutines.OK(), fmt.Errorf("account creator is nil or empty")
	}
	tuples, err := fga.TuplesForOrganization(fga.TuplesForOrganizationInput{
		BaseTuplesInput: fga.BaseTuplesInput{
			Creator:                *acc.Spec.Creator,
			AccountOriginClusterID: ai.Spec.Account.OriginClusterId,
			AccountName:            ai.Spec.Account.Name,
			CreatorRelation:        w.creatorRelation,
			ObjectType:             w.objectType,
		},
	})
	if err != nil {
		return subroutines.OK(), fmt.Errorf("building tuples for organization: %w", err)
	}
	if w.cfg.AllowMemberTuplesEnabled { // TODO: remove this flag once the feature is tested and stable
		tuples = append(tuples, []corev1alpha1.Tuple{
			{
				Object:   "role:authenticated",
				Relation: "assignee",
				User:     "user:*",
			},
			{
				Object:   fmt.Sprintf("core_platform-mesh_io_account:%s/%s", lc.Spec.Owner.Cluster, lc.Spec.Owner.Name),
				Relation: "member",
				User:     "role:authenticated#assignee",
			},
		}...)
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, orgsClient, &store, func() error {
		store.Spec = corev1alpha1.StoreSpec{
			CoreModule: w.coreModule,
		}
		store.Spec.Tuples = tuples

		return nil
	}); err != nil {
		return subroutines.OK(), fmt.Errorf("unable to create/update store: %w", err)
	} else if result == controllerutil.OperationResultCreated || result == controllerutil.OperationResultUpdated {
		return subroutines.StopWithRequeue(5*time.Second, "store needed to be updated, requeueing"), nil
	}

	// Check if Store applied tuple changes
	for _, t := range tuples {
		if !slices.Contains(store.Status.ManagedTuples, t) {
			return subroutines.StopWithRequeue(5*time.Second, "store does not yet contain all specified tuples, requeueing"), nil
		}
	}

	if store.Status.StoreID == "" {
		// Store is not ready yet, requeue
		return subroutines.StopWithRequeue(5*time.Second, "store id is empty"), nil
	}

	return subroutines.OK(), nil
}

func generateStoreName(lc *kcpcorev1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}
