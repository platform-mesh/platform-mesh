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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpactivation"
	"go.platform-mesh.io/identity-provider-operator/pkg/orchestration"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type publicationOptions struct {
	kubeconfig, classWorkspace, organizationWorkspace, className    string
	installationID, runtimeNamespace, activationSecret, clientsFile string
	resync, timeout                                                 time.Duration
}

func newOrganizationPublicationCommand() *cobra.Command {
	options := publicationOptions{}
	cmd := &cobra.Command{
		Use:   "publish-organizations",
		Short: "Run activation-gated organization identity publication",
		RunE:  func(cmd *cobra.Command, _ []string) error { return options.run(cmd.Context()) },
	}
	flags := cmd.Flags()
	flags.StringVar(&options.kubeconfig, "kcp-kubeconfig", "/api-kubeconfig/kubeconfig", "KCP service identity kubeconfig")
	flags.StringVar(&options.classWorkspace, "class-workspace", "root:platform-mesh-system", "Trusted provider-class workspace")
	flags.StringVar(&options.organizationWorkspace, "organization-workspace", "root:orgs", "Trusted organization API workspace")
	flags.StringVar(&options.className, "provider-class", "default", "Selected identity provider class")
	flags.StringVar(&options.installationID, "installation-id", "", "Stable administrator-assigned installation identifier")
	flags.StringVar(&options.runtimeNamespace, "runtime-namespace", "platform-mesh-system", "Private runtime Secret namespace")
	flags.StringVar(&options.activationSecret, "activation-secret", "identity-activation", "Administrator-owned activation Secret")
	flags.StringVar(&options.clientsFile, "client-config-file", "", "JSON file containing explicit organization OIDCClient specifications")
	flags.DurationVar(&options.timeout, "request-timeout", 10*time.Second, "Maximum KCP API request duration")
	flags.DurationVar(&options.resync, "resync-interval", 30*time.Second, "Retry interval for provider readiness and activation")
	return cmd
}

func (o publicationOptions) run(ctx context.Context) error {
	if o.installationID == "" || o.className == "" || o.runtimeNamespace == "" ||
		o.activationSecret == "" || o.clientsFile == "" || o.timeout <= 0 || o.resync <= 0 {
		return errors.New("installation, provider class, private activation Secret, explicit client configuration and positive timeouts are required")
	}
	clients, clientDigest, err := loadOrganizationClients(o.clientsFile)
	if err != nil {
		return err
	}
	kcpConfig, err := clientcmd.BuildConfigFromFlags("", o.kubeconfig)
	if err != nil {
		return err
	}
	kcpConfig.Timeout = o.timeout
	classConfig, err := workspaceConfig(kcpConfig, o.classWorkspace, o.timeout)
	if err != nil {
		return err
	}
	organizationConfig, err := workspaceConfig(kcpConfig, o.organizationWorkspace, o.timeout)
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme, pmcorev1alpha1.AddToScheme, pmidentityv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme, kcptenancyv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	classes, err := ctrlruntimeclient.New(classConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	organizations, err := ctrlruntimeclient.New(organizationConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	organizationLogicalCluster := &kcpcorev1alpha1.LogicalCluster{}
	readCtx, cancel := context.WithTimeout(ctx, o.timeout)
	err = organizations.Get(readCtx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, organizationLogicalCluster)
	cancel()
	if err != nil {
		return fmt.Errorf("resolve trusted organization LogicalCluster: %w", err)
	}
	organizationCluster := logicalcluster.From(organizationLogicalCluster)
	if !organizationCluster.IsValid() || organizationLogicalCluster.UID == "" {
		return errors.New("trusted organization LogicalCluster is missing its immutable identity")
	}

	privateConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	privateConfig.Timeout = o.timeout
	private, err := ctrlruntimeclient.New(privateConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	manager, err := ctrl.NewManager(organizationConfig, ctrl.Options{
		Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0", LeaderElection: true,
		LeaderElectionConfig: privateConfig, LeaderElectionNamespace: o.runtimeNamespace,
		LeaderElectionID: "identity-organization-publisher.platform-mesh.io",
	})
	if err != nil {
		return err
	}
	engine := &orchestration.OrganizationReconciler{
		Organizations: manager.GetClient(), Classes: classes,
		OrganizationCluster: organizationCluster, ClassName: o.className,
		Clients: func(string) ([]pmidentityv1alpha1.OIDCClient, error) {
			return append([]pmidentityv1alpha1.OIDCClient(nil), clients...), nil
		},
	}
	publisher := &orchestration.Publisher{
		Organization: engine,
		Gate: &idpactivation.SecretGate{
			Reader: private, Namespace: o.runtimeNamespace, Name: o.activationSecret,
		},
		InstallationID: o.installationID, ClientConfigurationDigest: clientDigest,
		OrganizationWorkspacePath: o.organizationWorkspace,
	}
	reconciler := &organizationPublicationController{
		Organizations: manager.GetClient(), Publisher: publisher,
		KCPConfig: kcpConfig, RequestTimeout: o.timeout, ResyncInterval: o.resync,
	}
	if err := ctrl.NewControllerManagedBy(manager).Named("identity-organization-publication").
		For(&pmcorev1alpha1.Account{}).
		WithOptions(controller.TypedOptions[reconcile.Request]{MaxConcurrentReconciles: 1}).
		Complete(reconciler); err != nil {
		return err
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return manager.Start(ctx)
}

func loadOrganizationClients(path string) ([]pmidentityv1alpha1.OIDCClient, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open organization client configuration: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	closeErr := file.Close()
	if err != nil {
		return nil, "", errors.Join(fmt.Errorf("read organization client configuration: %w", err), closeErr)
	}
	if closeErr != nil {
		return nil, "", fmt.Errorf("close organization client configuration: %w", closeErr)
	}
	if len(data) > 1<<20 {
		return nil, "", errors.New("organization client configuration exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var clients []pmidentityv1alpha1.OIDCClient
	if err := decoder.Decode(&clients); err != nil {
		return nil, "", fmt.Errorf("decode organization client configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, "", errors.New("organization client configuration must contain exactly one JSON value")
	}
	if len(clients) == 0 {
		return nil, "", errors.New("organization client configuration must not be empty")
	}
	canonical, err := json.Marshal(clients)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize organization client configuration: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return clients, "sha256:" + hex.EncodeToString(digest[:]), nil
}

type organizationPublicationController struct {
	Organizations                  ctrlruntimeclient.Client
	Publisher                      *orchestration.Publisher
	KCPConfig                      *rest.Config
	RequestTimeout, ResyncInterval time.Duration
}

func (r *organizationPublicationController) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.RequestTimeout)
	defer cancel()
	account := &pmcorev1alpha1.Account{}
	if err := r.Organizations.Get(ctx, req.NamespacedName, account); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}
	if account.Spec.Type != pmcorev1alpha1.AccountTypeOrg || !account.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	workspace := &kcptenancyv1alpha1.Workspace{}
	if err := r.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: account.Name}, workspace); err != nil {
		return ctrl.Result{}, err
	}
	cluster := logicalcluster.Name(workspace.Spec.Cluster)
	if !cluster.IsValid() {
		return ctrl.Result{}, errors.New("organization Workspace has no valid immutable cluster ID")
	}
	clusterConfig, err := workspaceConfig(r.KCPConfig, cluster.String(), r.RequestTimeout)
	if err != nil {
		return ctrl.Result{}, err
	}
	workspaceClient, err := ctrlruntimeclient.New(clusterConfig, ctrlruntimeclient.Options{Scheme: r.Organizations.Scheme()})
	if err != nil {
		return ctrl.Result{}, err
	}
	_, err = r.Publisher.Reconcile(ctx, workspaceClient, cluster)
	if errors.Is(err, orchestration.ErrIdentityNotReady) {
		ctrl.LoggerFrom(ctx).Info("identity tenant is not ready; publication will retry", "account", account.Name)
		return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}
