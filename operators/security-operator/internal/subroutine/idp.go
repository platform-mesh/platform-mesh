package subroutine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/rs/zerolog/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/platform-mesh/golang-commons/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
)

func NewIDPSubroutine(orgsClient client.Client, mgr mcmanager.Manager, cfg config.Config) *IDPSubroutine {
	return &IDPSubroutine{
		orgsClient:             orgsClient,
		mgr:                    mgr,
		additionalRedirectURLs: cfg.IDP.AdditionalRedirectURLs,
		baseDomain:             cfg.BaseDomain,
	}
}

var _ lifecyclesubroutine.Subroutine = &IDPSubroutine{}

type IDPSubroutine struct {
	orgsClient             client.Client
	mgr                    mcmanager.Manager
	additionalRedirectURLs []string
	baseDomain             string
}

func (w *IDPSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (w *IDPSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return nil
}

func (w *IDPSubroutine) GetName() string { return "IDPSubroutine" }

func (w *IDPSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace name"), true, false)
	}

	cl, err := w.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get cluster from context %w", err), true, true)
	}

	var account accountv1alpha1.Account
	err = w.orgsClient.Get(ctx, types.NamespacedName{Name: workspaceName}, &account)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get account resource %w", err), true, true)
	}

	if account.Spec.Type != accountv1alpha1.AccountTypeOrg {
		log.Debug().Str("workspace", workspaceName).Msg("account is not of type organization, skipping idp creation")
		return ctrl.Result{}, nil
	}

	clientConfig := v1alpha1.IdentityProviderClientConfig{
		ClientName:             workspaceName,
		ClientType:             v1alpha1.IdentityProviderClientTypeConfidential,
		RedirectURIs:           append(w.additionalRedirectURLs, fmt.Sprintf("https://%s.%s/*", workspaceName, w.baseDomain)),
		PostLogoutRedirectURIs: []string{fmt.Sprintf("https://%s.%s/logout*", workspaceName, w.baseDomain)},
		SecretRef: corev1.SecretReference{
			Name:      fmt.Sprintf("portal-client-secret-%s-%s", workspaceName, workspaceName),
			Namespace: "default",
		},
	}

	idp := &v1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: workspaceName}}
	_, err = controllerutil.CreateOrUpdate(ctx, cl.GetClient(), idp, func() error {
		clientIdx := slices.IndexFunc(idp.Spec.Clients, func(c v1alpha1.IdentityProviderClientConfig) bool {
			return c.ClientName == clientConfig.ClientName
		})
		if clientIdx != -1 {
			idp.Spec.Clients[clientIdx].RedirectURIs = clientConfig.RedirectURIs
			idp.Spec.Clients[clientIdx].ClientType = clientConfig.ClientType
			idp.Spec.Clients[clientIdx].SecretRef = clientConfig.SecretRef
			idp.Spec.Clients[clientIdx].PostLogoutRedirectURIs = clientConfig.PostLogoutRedirectURIs
			return nil
		}

		idp.Spec.Clients = append(idp.Spec.Clients, clientConfig)
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create idp resource %w", err), true, true)
	}

	log.Info().Str("workspace", workspaceName).Msg("idp configuration resource is created")

	if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: workspaceName}, idp); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get idp resource %w", err), true, true)
	}
	if !meta.IsStatusConditionTrue(idp.GetConditions(), "Ready") {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("idp resource is not ready yet"), true, false)
	}

	log.Info().Str("workspace", workspaceName).Msg("idp resource is ready")
	return ctrl.Result{}, nil
}

func getWorkspaceName(lc *kcpv1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}
