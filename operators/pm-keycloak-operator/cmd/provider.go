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

package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/app"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/identityprovider"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func newProviderCommand() *cobra.Command {
	var kubeconfig, workspace, organizationWorkspace, className, namespace, endpointSlice string
	var requestTimeout, resync time.Duration
	validation := &providerValidationServer{}
	cmd := &cobra.Command{
		Use: "provider", Short: "Reconcile the staged neutral identity API against Keycloak without legacy writers",
		RunE: func(_ *cobra.Command, _ []string) error {
			if requestTimeout <= 0 || resync <= 0 || className == "" || namespace == "" || workspace == "" || organizationWorkspace == "" {
				return errors.New("provider class/workspace, runtime namespace, request timeout and resync interval are required")
			}
			kcpConfig, err := getKubeconfigFromPath(kubeconfig)
			if err != nil {
				return err
			}
			kcpConfig.Timeout = requestTimeout
			providerScheme := app.NewScheme()
			if err := pmidentityv1alpha1.AddToScheme(providerScheme); err != nil {
				return err
			}
			runtimeConfig, err := ctrl.GetConfig()
			if err != nil {
				return err
			}
			private, err := ctrlruntimeclient.New(runtimeConfig, ctrlruntimeclient.Options{Scheme: providerScheme})
			if err != nil {
				return err
			}
			provider, err := pathaware.New(kcpConfig, endpointSlice, apiexport.Options{Scheme: providerScheme})
			if err != nil {
				return err
			}
			manager, err := mcmanager.New(kcpConfig, provider, ctrl.Options{
				Scheme: providerScheme, Metrics: metricsserver.Options{BindAddress: defaultCfg.Metrics.BindAddress},
				HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
				LeaderElection:         true, LeaderElectionConfig: runtimeConfig,
				LeaderElectionNamespace: namespace, LeaderElectionID: "pm-keycloak-identity-provider",
			})
			if err != nil {
				return err
			}
			getter := iclient.NewConfigSchemeKCPClientGetter(kcpConfig, providerScheme)
			classes, err := getter.NewClientForLogicalCluster(context.Background(), workspace)
			if err != nil {
				return err
			}
			orgs, err := getter.NewClientForLogicalCluster(context.Background(), organizationWorkspace)
			if err != nil {
				return err
			}
			classWorkspaceCluster := &kcpcorev1alpha1.LogicalCluster{}
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			err = classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, classWorkspaceCluster)
			cancel()
			if err != nil {
				return err
			}
			organizationCluster := &kcpcorev1alpha1.LogicalCluster{}
			ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
			err = orgs.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, organizationCluster)
			cancel()
			if err != nil {
				return err
			}
			welcomeCluster := logicalcluster.From(classWorkspaceCluster)
			if !welcomeCluster.IsValid() {
				return errors.New("provider class workspace has no immutable logical cluster identity")
			}
			r := &identityprovider.Runtime{
				Classes: classes, Private: private, ClassName: className, RuntimeNamespace: namespace,
				Organizations: orgs, OrganizationCluster: logicalcluster.From(organizationCluster), WelcomeCluster: welcomeCluster,
				RequestTimeout: requestTimeout, ResyncInterval: resync,
				ClientForCluster: func(ctx context.Context, cluster logicalcluster.Name) (ctrlruntimeclient.Client, error) {
					return getter.NewClientForLogicalCluster(ctx, cluster.String())
				},
			}
			if err := r.SetupWithManager(manager); err != nil {
				return err
			}
			if validation.bindAddress != "" {
				validation.configure(classes, private, className, namespace, requestTimeout)
				if err := manager.GetLocalManager().Add(validation); err != nil {
					return err
				}
			}
			if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				return err
			}
			if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				return err
			}
			return manager.Start(ctrl.SetupSignalHandler())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&kubeconfig, "kcp-kubeconfig", "/api-kubeconfig/kubeconfig", "KCP service identity kubeconfig")
	flags.StringVar(&workspace, "class-workspace", "root:platform-mesh-system", "Administrator-owned provider-class/configuration workspace")
	flags.StringVar(&organizationWorkspace, "organization-workspace", "root:orgs", "Trusted organization identity-tenant workspace")
	flags.StringVar(&className, "provider-class", "default", "Active provider class")
	flags.StringVar(&namespace, "runtime-namespace", "platform-mesh-system", "Private provider Secret and leader-election namespace")
	flags.StringVar(&endpointSlice, "identity-api-export-endpoint-slice-name", "identity.platform-mesh.io", "Platform-owned identity APIExportEndpointSlice")
	flags.DurationVar(&requestTimeout, "request-timeout", 30*time.Second, "Maximum duration of a provider reconciliation")
	flags.DurationVar(&resync, "resync-interval", 30*time.Second, "Provider state and credential/configuration resync interval")
	flags.StringVar(&validation.bindAddress, "validation-bind-address", "", "Optional mutual-TLS provider validation listen address")
	flags.StringVar(&validation.certFile, "validation-tls-cert-file", "", "Provider validator serving certificate")
	flags.StringVar(&validation.keyFile, "validation-tls-key-file", "", "Provider validator serving private key")
	flags.StringVar(&validation.clientCA, "validation-client-ca-file", "", "Trusted generic admission caller CA")
	flags.StringSliceVar(&validation.workloads, "validation-allowed-workloads", nil, "Exact SPIFFE identities permitted to request provider validation")
	return cmd
}
