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

package identityprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kcp-dev/logicalcluster/v3"
)

type Runtime struct {
	Classes             ctrlruntimeclient.Client
	Private             ctrlruntimeclient.Client
	Organizations       ctrlruntimeclient.Client
	OrganizationCluster logicalcluster.Name
	WelcomeCluster      logicalcluster.Name
	ClientForCluster    func(context.Context, logicalcluster.Name) (ctrlruntimeclient.Client, error)
	ClassName           string
	RuntimeNamespace    string
	RequestTimeout      time.Duration
	ResyncInterval      time.Duration
}

func (r *Runtime) ObserveClass(ctx context.Context) (*pmidentityv1alpha1.IdentityProviderClass, *LoadedConfiguration, error) {
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := r.Classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: r.ClassName}, class); err != nil {
		return nil, nil, err
	}
	if class.Spec.ControllerName != directory.KeycloakControllerName {
		return nil, nil, errors.New("configured provider class is owned by a different controller")
	}
	before := class.DeepCopy()
	errs := pmidentityv1alpha1.ValidateProviderClass(class)
	if len(errs) != 0 {
		return nil, nil, errs.ToAggregate()
	}
	loaded, err := LoadConfiguration(ctx, r.Classes, r.Private, class, r.RuntimeNamespace, r.RequestTimeout)
	if err == nil {
		if class.Spec.AuthenticationPolicy.IssuerMode != pmidentityv1alpha1.IssuerModeDedicated {
			err = errors.New("the default provider requires dedicated issuers")
		} else {
			err = loaded.Backend.request(ctx, loaded.Backend.admin, http.MethodGet,
				loaded.Backend.config.AdminURL+"/admin/realms/master", nil, nil)
		}
	}
	class.Status.ObservedGeneration = class.Generation
	if err != nil {
		setReady(&class.Status.Conditions, class.Generation, false, "ProviderUnavailable", "Provider configuration or authenticated administration is unavailable")
		if loaded != nil {
			loaded.Close()
		}
		if !equality.Semantic.DeepEqual(before.Status, class.Status) {
			err = errors.Join(err, r.Classes.Status().Update(ctx, class))
		}
		return nil, nil, err
	}
	class.Status.ContractVersion = pmidentityv1alpha1.ContractVersion
	class.Status.ObservedConfigurationRevision = loaded.Revision
	class.Status.Capabilities = []pmidentityv1alpha1.IdentityCapability{
		pmidentityv1alpha1.IdentityCapabilityOIDC,
		pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
		pmidentityv1alpha1.IdentityCapabilityClientManagement,
		pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
		pmidentityv1alpha1.IdentityCapabilitySelfRegistration,
		pmidentityv1alpha1.IdentityCapabilityLogout,
	}
	if class.Spec.Directory != nil {
		class.Status.Capabilities = append(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityDirectory)
	}
	if r.Organizations != nil && r.OrganizationCluster.IsValid() &&
		loaded.Backend.config.SMTP["host"] != "" && loaded.Backend.config.SMTP["from"] != "" {
		class.Status.Capabilities = append(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityInvitations)
	}
	setReady(&class.Status.Conditions, class.Generation, true, "Configured", "Default provider configuration and administration are available")
	if !equality.Semantic.DeepEqual(before.Status, class.Status) {
		if err := r.Classes.Status().Update(ctx, class); err != nil {
			loaded.Close()
			return nil, nil, err
		}
	}
	return class, loaded, nil
}

func (r *Runtime) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.RequestTimeout)
	defer cancel()
	cluster := logicalcluster.Name(req.ClusterName)
	if !cluster.IsValid() {
		return ctrl.Result{}, errors.New("provider reconciliation requires an immutable logical cluster ID")
	}
	cl, err := r.ClientForCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	if err := cl.Get(ctx, req.NamespacedName, tenant); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}
	if tenant.Spec.ProviderClassRef != r.ClassName {
		return ctrl.Result{}, nil
	}
	if err := validateTenantCluster(tenant.Spec.Purpose, cluster, r.WelcomeCluster); err != nil {
		return ctrl.Result{}, err
	}
	var class *pmidentityv1alpha1.IdentityProviderClass
	var loaded *LoadedConfiguration
	if !tenant.DeletionTimestamp.IsZero() {
		class = &pmidentityv1alpha1.IdentityProviderClass{}
		err = r.Classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: r.ClassName}, class)
		if err == nil {
			loaded, err = LoadConfiguration(ctx, r.Classes, r.Private, class, r.RuntimeNamespace, r.RequestTimeout)
		}
	} else {
		class, loaded, err = r.ObserveClass(ctx)
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve selected identity provider class: %w", err)
	}
	defer loaded.Close()
	engine := &Reconciler{
		Tenants: cl, Private: r.Private, Cluster: cluster,
		RuntimeNamespace: r.RuntimeNamespace, OutputNamespace: loaded.OutputNamespace,
		ProviderIdentity: loaded.Identity, Backend: loaded.Backend,
	}
	if err := engine.Reconcile(ctx, class, tenant); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}

func (r *Runtime) SetupWithManager(mgr mcmanager.Manager) error {
	if r.Classes == nil || r.Private == nil || r.ClientForCluster == nil || r.ClassName == "" ||
		r.RuntimeNamespace == "" || !r.WelcomeCluster.IsValid() || r.RequestTimeout <= 0 || r.ResyncInterval <= 0 {
		return errors.New("provider runtime configuration and clients are incomplete")
	}
	specOrDeletion := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
				!e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp())
		},
	}
	if err := mcbuilder.ControllerManagedBy(mgr).Named("identity-tenant-keycloak").
		For(&pmidentityv1alpha1.IdentityTenant{}).WithEventFilter(specOrDeletion).Complete(r); err != nil {
		return err
	}
	if r.Organizations != nil {
		if !r.OrganizationCluster.IsValid() {
			return errors.New("invitation reconciliation requires the trusted organizations logical cluster")
		}
		if err := mcbuilder.ControllerManagedBy(mgr).Named("identity-invitation-keycloak").
			For(&pmidentityv1alpha1.Invitation{}).WithEventFilter(specOrDeletion).
			Complete(mcreconcile.Func(r.ReconcileInvitation)); err != nil {
			return err
		}
	}
	return mgr.GetLocalManager().Add(&classObserver{runtime: r})
}

func validateTenantCluster(purpose pmidentityv1alpha1.IdentityTenantPurpose, cluster, welcomeCluster logicalcluster.Name) error {
	if !cluster.IsValid() || !welcomeCluster.IsValid() {
		return errors.New("identity tenant and welcome workspace require immutable logical cluster IDs")
	}
	switch purpose {
	case pmidentityv1alpha1.IdentityTenantPurposeWelcome:
		if cluster != welcomeCluster {
			return errors.New("welcome identity tenants must be hosted in the trusted welcome workspace")
		}
	case pmidentityv1alpha1.IdentityTenantPurposeOrganization:
		if cluster == welcomeCluster {
			return errors.New("organization identity tenants cannot be hosted in the trusted welcome workspace")
		}
	default:
		return fmt.Errorf("unsupported identity tenant purpose %q", purpose)
	}
	return nil
}

type classObserver struct{ runtime *Runtime }

func (o *classObserver) NeedLeaderElection() bool { return true }

func (o *classObserver) Start(ctx context.Context) error {
	ticker := time.NewTicker(o.runtime.ResyncInterval)
	defer ticker.Stop()
	for {
		bounded, cancel := context.WithTimeout(ctx, o.runtime.RequestTimeout)
		_, config, err := o.runtime.ObserveClass(bounded)
		cancel()
		if config != nil {
			config.Close()
		}
		if err != nil && ctx.Err() == nil {
			ctrl.LoggerFrom(ctx).Error(err, "identity provider class observation failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
