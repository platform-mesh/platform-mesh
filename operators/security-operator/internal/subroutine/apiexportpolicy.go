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
	"slices"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	orgsWorkspacePath     = "root:orgs"
	bindRelation          = "bind"
	bindInheritedRelation = "bind_inherited"
)

type APIExportPolicySubroutine struct {
	fga             openfgav1.OpenFGAServiceClient
	cfg             *config.Config
	storeIDGetter   fga.StoreIDGetter
	lister          iclient.Lister
	kcpClientGetter iclient.KCPClientGetter
}

func NewAPIExportPolicySubroutine(fgaClient openfgav1.OpenFGAServiceClient, cfg *config.Config, storeIDGetter fga.StoreIDGetter, lister iclient.Lister, kcpClientGetter iclient.KCPClientGetter) *APIExportPolicySubroutine {
	return &APIExportPolicySubroutine{
		fga:             fgaClient,
		cfg:             cfg,
		storeIDGetter:   storeIDGetter,
		lister:          lister,
		kcpClientGetter: kcpClientGetter,
	}
}

var _ subroutines.Subroutine = &APIExportPolicySubroutine{}

func (a *APIExportPolicySubroutine) GetName() string {
	return "APIExportPolicySubroutine"
}

func (a *APIExportPolicySubroutine) Finalizers(_ client.Object) []string {
	return []string{"system.platform-mesh.io/apiexportpolicy-finalizer"}
}

func (a *APIExportPolicySubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	policy := obj.(*corev1alpha1.APIExportPolicy)

	providerClusterID, err := a.getClusterIDFromPath(ctx, policy.Spec.APIExportRef.ClusterPath)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting provider cluster ID for %s: %w", policy.Spec.APIExportRef.ClusterPath, err)
	}

	// Delete tuples for expressions that were removed from the spec
	if err := a.deleteRemovedExpressions(ctx, policy); err != nil {
		return subroutines.OK(), fmt.Errorf("removing tuples for policy %s: %w", policy.Name, err)
	}

	for _, expression := range policy.Spec.AllowPathExpressions {
		workspacePath, relation, err := a.parseAllowExpression(expression)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("parsing allow expression %s: %w", expression, err)
		}

		// for orgs workspace we need to write 1 tuple in every store
		// for this we need to get cluster id for every org's workspace
		if workspacePath == orgsWorkspacePath {
			var accountInfoList corev1alpha1.AccountInfoList
			if err := a.lister.List(ctx, &accountInfoList); err != nil {
				return subroutines.OK(), fmt.Errorf("listing AccountInfo resources: %w", err)
			}

			for _, ai := range accountInfoList.Items {
				if ai.Spec.Account.Type != corev1alpha1.AccountTypeOrg {
					continue
				}

				storeID, err := a.storeIDGetter.Get(ctx, ai.Spec.Organization.Name)
				if err != nil {
					return subroutines.OK(), fmt.Errorf("getting store ID for org %s: %w", ai.Spec.Organization.Name, err)
				}

				tuple := corev1alpha1.Tuple{
					Object:   fmt.Sprintf("core_platform-mesh_io_account:%s/%s", ai.Spec.Account.OriginClusterId, ai.Spec.Account.Name),
					Relation: relation,
					User:     fmt.Sprintf("apis_kcp_io_apiexport:%s/%s", providerClusterID, policy.Spec.APIExportRef.Name),
				}

				tm := fga.NewTupleManager(a.fga, storeID, fga.AuthorizationModelIDLatest, log)
				if err := tm.Apply(ctx, []corev1alpha1.Tuple{tuple}); err != nil {
					return subroutines.OK(), fmt.Errorf("applying tuple for expression %s: %w", expression, err)
				}
			}
			continue
		}

		// for all valid expressions except of :root:orgs:*
		// e.g :root:orgs:A:B, find store id
		// and clusterID of logical cluster where account B lives (logical cluster A)
		cl, err := a.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.CoreProviderName, workspacePath)))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting client for workspace %s: %w", workspacePath, err)
		}

		var ai corev1alpha1.AccountInfo
		if err := cl.Get(ctx, client.ObjectKey{Name: "account"}, &ai); err != nil {
			return subroutines.OK(), fmt.Errorf("getting AccountInfo for workspace %s: %w", workspacePath, err)
		}

		storeID, err := a.storeIDGetter.Get(ctx, ai.Spec.Organization.Name)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting store ID for org %s: %w", ai.Spec.Organization.Name, err)
		}

		tuple := corev1alpha1.Tuple{
			Object:   fmt.Sprintf("core_platform-mesh_io_account:%s/%s", ai.Spec.Account.OriginClusterId, ai.Spec.Account.Name),
			Relation: relation,
			User:     fmt.Sprintf("apis_kcp_io_apiexport:%s/%s", providerClusterID, policy.Spec.APIExportRef.Name),
		}

		tm := fga.NewTupleManager(a.fga, storeID, fga.AuthorizationModelIDLatest, log)
		if err := tm.Apply(ctx, []corev1alpha1.Tuple{tuple}); err != nil {
			return subroutines.OK(), fmt.Errorf("applying tuple for expression %s: %w", expression, err)
		}
	}

	cl, err := a.kcpClientGetter.NewClientFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context %w", err)
	}

	// Update status with managed expressions
	original := policy.DeepCopy()
	policy.Status.ManagedAllowExpressions = policy.Spec.AllowPathExpressions

	if err := cl.Status().Patch(ctx, policy, client.MergeFrom(original)); err != nil {
		return subroutines.OK(), fmt.Errorf("failed to patch APIExportPolicy status: %w", err)
	}

	log.Info().Msg("Successfully processed APIExportPolicy")
	return subroutines.OK(), nil
}

func (a *APIExportPolicySubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	policy := obj.(*corev1alpha1.APIExportPolicy)

	providerClusterID, err := a.getClusterIDFromPath(ctx, policy.Spec.APIExportRef.ClusterPath)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting provider cluster ID for %s: %w", policy.Spec.APIExportRef.ClusterPath, err)
	}

	// iterate over each expression and delete tuples
	// which were created for this expression
	for _, expression := range policy.Spec.AllowPathExpressions {
		err := a.deleteTuplesForExpression(ctx, expression, providerClusterID, policy.Spec.APIExportRef.Name)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("deleting tuples for expression %s: %w", expression, err)
		}
	}

	log.Info().Msg("Finalized APIExportPolicy")
	return subroutines.OK(), nil
}

func (a *APIExportPolicySubroutine) getClusterIDFromPath(ctx context.Context, clusterPath string) (string, error) {
	cl, err := a.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.CoreProviderName, clusterPath)))
	if err != nil {
		return "", fmt.Errorf("getting client for workspace %s: %w", clusterPath, err)
	}

	var lc kcpcorev1alpha1.LogicalCluster
	if err := cl.Get(ctx, client.ObjectKey{Name: "cluster"}, &lc); err != nil {
		return "", fmt.Errorf("getting logical cluster for path %s: %w", clusterPath, err)
	}

	clusterID, ok := lc.Annotations["kcp.io/cluster"]
	if !ok {
		return "", fmt.Errorf("kcp.io/cluster annotation not found on logical cluster %s", clusterPath)
	}
	return clusterID, nil
}

func (a *APIExportPolicySubroutine) parseAllowExpression(expr string) (workspacePath string, relation string, err error) {
	expr = strings.TrimPrefix(expr, ":")

	if !strings.HasPrefix(expr, "root:orgs:") {
		return "", "", fmt.Errorf("invalid path expression: must start with root:orgs")
	}

	if strings.HasSuffix(expr, ":*") {
		// Wildcard pattern, use bind_inherited relation
		// Remove the trailing :*
		workspacePath = strings.TrimSuffix(expr, ":*")
		relation = bindInheritedRelation
		return workspacePath, relation, nil
	}
	return expr, bindRelation, nil
}

// finds expressions which are present in the status but aren't in the spec
// and do the cleanup of the tupels for removed expressions
func (a *APIExportPolicySubroutine) deleteRemovedExpressions(ctx context.Context, policy *corev1alpha1.APIExportPolicy) error {
	providerClusterID, err := a.getClusterIDFromPath(ctx, policy.Spec.APIExportRef.ClusterPath)
	if err != nil {
		return fmt.Errorf("getting provider cluster ID for %s: %w", policy.Spec.APIExportRef.ClusterPath, err)
	}

	for _, managedExpr := range policy.Status.ManagedAllowExpressions {
		exists := slices.Contains(policy.Spec.AllowPathExpressions, managedExpr)
		if exists {
			continue
		}

		err := a.deleteTuplesForExpression(ctx, managedExpr, providerClusterID, policy.Spec.APIExportRef.Name)
		if err != nil {
			return fmt.Errorf("removing tuples for expression %s: %w", managedExpr, err)
		}
	}
	return nil
}

// based on the expression and apiexport data
// removes tuples which were created for this expression
func (a *APIExportPolicySubroutine) deleteTuplesForExpression(ctx context.Context, expression string, providerClusterID string, apiExportName string) error {
	log := logger.LoadLoggerFromContext(ctx)

	workspacePath, relation, err := a.parseAllowExpression(expression)
	if err != nil {
		return fmt.Errorf("parsing expression %s: %w", expression, err)
	}

	if workspacePath == orgsWorkspacePath {
		var accountInfoList corev1alpha1.AccountInfoList
		if err := a.lister.List(ctx, &accountInfoList); err != nil {
			return fmt.Errorf("listing AccountInfo resources for %s: %w", expression, err)
		}

		for _, ai := range accountInfoList.Items {
			storeID, err := a.storeIDGetter.Get(ctx, ai.Spec.Organization.Name)
			if err != nil {
				return fmt.Errorf("getting store ID for org %s: %w", ai.Spec.Organization.Name, err)
			}

			tupleToDelete := corev1alpha1.Tuple{
				Object:   fmt.Sprintf("core_platform-mesh_io_account:%s/%s", ai.Spec.Account.OriginClusterId, ai.Spec.Account.Name),
				Relation: relation,
				User:     fmt.Sprintf("apis_kcp_io_apiexport:%s/%s", providerClusterID, apiExportName),
			}

			tm := fga.NewTupleManager(a.fga, storeID, fga.AuthorizationModelIDLatest, log)
			if err := tm.Delete(ctx, []corev1alpha1.Tuple{tupleToDelete}); err != nil {
				return fmt.Errorf("removing tuple in openFGA: %w", err)
			}
		}
		return nil
	}

	cl, err := a.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.CoreProviderName, workspacePath)))
	if err != nil {
		return fmt.Errorf("getting client for workspace %s: %w", workspacePath, err)
	}

	var ai corev1alpha1.AccountInfo
	if err := cl.Get(ctx, client.ObjectKey{Name: "account"}, &ai); err != nil {
		return fmt.Errorf("getting AccountInfo for workspace %s: %w", workspacePath, err)
	}

	storeID, err := a.storeIDGetter.Get(ctx, ai.Spec.Organization.Name)
	if err != nil {
		return fmt.Errorf("getting store ID for org %s: %w", ai.Spec.Organization.Name, err)
	}

	tupleToDelete := corev1alpha1.Tuple{
		Object:   fmt.Sprintf("core_platform-mesh_io_account:%s/%s", ai.Spec.Account.OriginClusterId, ai.Spec.Account.Name),
		Relation: relation,
		User:     fmt.Sprintf("apis_kcp_io_apiexport:%s/%s", providerClusterID, apiExportName),
	}

	tm := fga.NewTupleManager(a.fga, storeID, fga.AuthorizationModelIDLatest, log)
	if err := tm.Delete(ctx, []corev1alpha1.Tuple{tupleToDelete}); err != nil {
		return fmt.Errorf("removing tuples: %w", err)
	}

	return nil
}
