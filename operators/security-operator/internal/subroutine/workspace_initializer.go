package subroutine

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	accountsv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	lifecycleruntimeobject "github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"

	"github.com/platform-mesh/golang-commons/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
)

const initializerName = "root:fga"

func NewWorkspaceInitializer(cl, orgsClient client.Client, restCfg *rest.Config, cfg config.Config) *workspaceInitializer {
	coreModulePath := cfg.CoreModulePath

	// read file from path
	res, err := os.ReadFile(coreModulePath)
	if err != nil {
		panic(err)
	}

	return &workspaceInitializer{
		cl:         cl,
		orgsClient: orgsClient,
		restCfg:    restCfg,
		coreModule: string(res),
	}
}

var _ lifecyclesubroutine.Subroutine = &workspaceInitializer{}

type workspaceInitializer struct {
	cl         client.Client
	orgsClient client.Client
	restCfg    *rest.Config
	coreModule string
}

func (w *workspaceInitializer) Finalize(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	// TODO: implement once finalizing workspaces are a thing
	return ctrl.Result{}, nil
}

func (w *workspaceInitializer) Finalizers() []string { return nil }

func (w *workspaceInitializer) GetName() string { return "WorkspaceInitializer" }

func (w *workspaceInitializer) Process(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	path, ok := lc.ObjectMeta.Annotations["kcp.io/path"]
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get workspace path"), true, false)
	}

	store := v1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{Name: generateStoreName(lc)},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, w.orgsClient, &store, func() error {
		store.Spec = v1alpha1.StoreSpec{
			Tuples: []v1alpha1.Tuple{
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
			},
			CoreModule: w.coreModule,
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to create/update store: %w", err), true, true)
	}

	if store.Status.StoreID == "" {
		// Store is not ready yet, requeue
		return ctrl.Result{Requeue: true}, nil
	}

	// Update accountInfo with storeid
	wsCfg := rest.CopyConfig(w.restCfg)
	wsCfg.Host = strings.Replace(wsCfg.Host, "/services/initializingworkspaces/root:fga", "/clusters/"+path, -1)
	wsClient, err := client.New(wsCfg, client.Options{Scheme: w.cl.Scheme()})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to create client: %w", err), true, true)
	}

	accountInfo := accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, wsClient, &accountInfo, func() error {
		accountInfo.Spec.FGA.Store.Id = store.Status.StoreID
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to create/update accountInfo: %w", err), true, true)
	}

	original := lc.DeepCopy()
	lc.Status.Initializers = slices.DeleteFunc(lc.Status.Initializers, func(s kcpv1alpha1.LogicalClusterInitializer) bool {
		return s == initializerName
	})

	err = w.cl.Status().Patch(ctx, lc, client.MergeFrom(original))
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to patch out initializers: %w", err), true, true)
	}

	return ctrl.Result{}, nil
}

func generateStoreName(lc *kcpv1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}
