package subroutine

import (
	"context"
	"fmt"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alphav1 "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/security-operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type workspaceAuthSubroutine struct {
	client        client.Client
	runtimeClient client.Client
	cfg           config.Config
}

func NewWorkspaceAuthConfigurationSubroutine(client, runtimeClient client.Client, cfg config.Config) *workspaceAuthSubroutine {
	return &workspaceAuthSubroutine{
		client:        client,
		runtimeClient: runtimeClient,
		cfg:           cfg,
	}
}

var _ lifecyclesubroutine.Subroutine = &workspaceAuthSubroutine{}

func (r *workspaceAuthSubroutine) GetName() string { return "workspaceAuthConfiguration" }

func (r *workspaceAuthSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{}
}

func (r *workspaceAuthSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	return reconcile.Result{}, nil
}

func (r *workspaceAuthSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace path"), true, false)
	}

	var domainCASecret corev1.Secret
	if r.cfg.DomainCALookup {
		err := r.runtimeClient.Get(ctx, client.ObjectKey{Name: "domain-certificate-ca", Namespace: "platform-mesh-system"}, &domainCASecret)
		if err != nil {
			return reconcile.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get domain CA secret: %w", err), true, false)
		}
	}

	obj := &kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration{ObjectMeta: metav1.ObjectMeta{Name: workspaceName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.client, obj, func() error {
		obj.Spec = kcptenancyv1alphav1.WorkspaceAuthenticationConfigurationSpec{
			JWT: []kcptenancyv1alphav1.JWTAuthenticator{
				{
					Issuer: kcptenancyv1alphav1.Issuer{
						URL:                 fmt.Sprintf("https://%s/keycloak/realms/%s", r.cfg.BaseDomain, workspaceName),
						AudienceMatchPolicy: kcptenancyv1alphav1.AudienceMatchPolicyMatchAny,
						Audiences:           []string{workspaceName},
					},
					ClaimMappings: kcptenancyv1alphav1.ClaimMappings{
						Groups: kcptenancyv1alphav1.PrefixedClaimOrExpression{
							Claim:  r.cfg.GroupClaim,
							Prefix: ptr.To(""),
						},
						Username: kcptenancyv1alphav1.PrefixedClaimOrExpression{
							Claim:  r.cfg.UserClaim,
							Prefix: ptr.To(""),
						},
					},
				},
			},
		}

		if r.cfg.DomainCALookup {
			obj.Spec.JWT[0].Issuer.CertificateAuthority = string(domainCASecret.Data["tls.crt"])
		}

		return nil
	})
	if err != nil {
		return reconcile.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create WorkspaceAuthConfiguration resource: %w", err), true, true)
	}

	return ctrl.Result{}, nil
}
