package subroutine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kcp-dev/kcp/sdk/apis/cache/initialization"
	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

const (
	PortalClientSecretNamespace = "platform-mesh-system"
)

type removeInitializer struct {
	initializerName   string
	mgr               mcmanager.Manager
	runtimeClient     client.Client
	secretWaitTimeout time.Duration
}

// Finalize implements subroutine.Subroutine.
func (r *removeInitializer) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

// Finalizers implements subroutine.Subroutine.
func (r *removeInitializer) Finalizers(_ runtimeobject.RuntimeObject) []string { return []string{} }

// GetName implements subroutine.Subroutine.
func (r *removeInitializer) GetName() string { return "RemoveInitializer" }

// Process implements subroutine.Subroutine.
func (r *removeInitializer) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	initializer := kcpv1alpha1.LogicalClusterInitializer(r.initializerName)

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get cluster from context: %w", err), true, false)
	}

	if !slices.Contains(lc.Status.Initializers, initializer) {
		log.Info().Msg("Initializer already absent, skipping patch")
		return ctrl.Result{}, nil
	}

	// we need to wait until keycloak crossplane provider creates a portal secret
	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace path"), true, false)
	}

	secretName := fmt.Sprintf("portal-client-secret-%s", workspaceName)
	key := types.NamespacedName{Name: secretName, Namespace: PortalClientSecretNamespace}

	var secret corev1.Secret
	if err := r.runtimeClient.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			age := time.Since(lc.CreationTimestamp.Time)
			if age <= r.secretWaitTimeout {
				log.Info().Str("secret", secretName).Msg("portal secret not ready yet, requeueing")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("portal client secret %s was not created within %s", secretName, r.secretWaitTimeout.String()), true, true)
		}
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get secret %s: %w", secretName, err), true, true)
	}

	patch := client.MergeFrom(lc.DeepCopy())

	lc.Status.Initializers = initialization.EnsureInitializerAbsent(initializer, lc.Status.Initializers)
	if err := cluster.GetClient().Status().Patch(ctx, lc, patch); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to patch out initializers: %w", err), true, true)
	}

	log.Info().Msg(fmt.Sprintf("Removed initializer from LogicalCluster status, name %s,uuid %s", lc.Name, lc.UID))

	return ctrl.Result{}, nil
}

func NewRemoveInitializer(mgr mcmanager.Manager, cfg config.Config, runtimeClient client.Client) *removeInitializer {
	return &removeInitializer{
		initializerName:   cfg.InitializerName,
		mgr:               mgr,
		runtimeClient:     runtimeClient,
		secretWaitTimeout: time.Duration(cfg.SecretWaitingTimeoutInSeconds) * time.Second,
	}
}

var _ subroutine.Subroutine = &removeInitializer{}
