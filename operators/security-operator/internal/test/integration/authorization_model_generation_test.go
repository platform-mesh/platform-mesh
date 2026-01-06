package test

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcpapiv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/kcp/sdk/apis/core"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"

	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	securityv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
)

func (suite *IntegrationSuite) TestAuthorizationModelGeneration_Process() {
	ctx := suite.T().Context()
	cli, err := clusterclient.New(suite.kcpConfig, client.Options{})
	suite.Require().NoError(err)

	resourceSchemaName := "v1.testresources.process.test.example.com"
	pluralResourceSchemaName := "testresources"
	suite.createTestAPIResourceSchema(ctx, suite.platformMeshSystemClient, resourceSchemaName, "process.test.example.com", pluralResourceSchemaName, "testresource", apiextensionsv1.NamespaceScoped)

	apiExportName := "process-test.example.com"
	suite.createTestAPIExport(ctx, suite.platformMeshSystemClient, apiExportName, []string{resourceSchemaName})

	orgsPath := logicalcluster.NewPath("root:orgs")

	const (
		testOrgName = "generator-test-process"
		testAccount = "test-account"
	)

	_, testOrgPath := envtest.NewWorkspaceFixture(suite.T(), cli, orgsPath, envtest.WithName(testOrgName), envtest.WithType(core.RootCluster.Path(), tenancyv1alpha1.WorkspaceTypeName("org")))

	_, testAccountPath := envtest.NewWorkspaceFixture(suite.T(), cli, testOrgPath, envtest.WithName(testAccount), envtest.WithType(core.RootCluster.Path(), tenancyv1alpha1.WorkspaceTypeName("account")))

	testAccountClient := cli.Cluster(testAccountPath)

	suite.createAccountInfo(ctx, testAccountClient, testAccount, testOrgName, testAccountPath, testOrgPath, suite.T())

	_ = suite.createTestAPIBinding(ctx, testAccountClient, apiExportName, suite.platformMeshSysPath.String(), apiExportName)

	expectedModelName := fmt.Sprintf("%s-%s", pluralResourceSchemaName, testOrgName)
	var model securityv1alpha1.AuthorizationModel
	suite.Assert().Eventually(func() bool {
		err := suite.platformMeshSystemClient.Get(ctx, client.ObjectKey{Name: expectedModelName}, &model)
		return err == nil
	}, 10*time.Second, 200*time.Millisecond, "authorizationModel should be created by controller")

	suite.Assert().Equal(testOrgName, model.Spec.StoreRef.Name)
	suite.Assert().Equal(testOrgPath.String(), model.Spec.StoreRef.Path)
}

func (suite *IntegrationSuite) TestAuthorizationModelGeneration_Finalize() {
	ctx := suite.T().Context()
	cli, err := clusterclient.New(suite.kcpConfig, client.Options{})
	suite.Require().NoError(err)

	pluralResourceSchemaName := "testresources"
	resourceSchemaName := "v1.testresources.generator.test.example.com"
	suite.createTestAPIResourceSchema(ctx, suite.platformMeshSystemClient, resourceSchemaName, "generator.test.example.com", pluralResourceSchemaName, "testresource", apiextensionsv1.NamespaceScoped)

	apiExportName := "generator-test.example.com"
	suite.createTestAPIExport(ctx, suite.platformMeshSystemClient, apiExportName, []string{resourceSchemaName})

	orgsPath := logicalcluster.NewPath("root:orgs")

	const (
		testAccount1Name = "test-account-1"
		testAccount2Name = "test-account-2"
		testOrgName      = "generator-test-finalize"
	)

	_, testOrgPath := envtest.NewWorkspaceFixture(suite.T(), cli, orgsPath, envtest.WithName(testOrgName), envtest.WithType(core.RootCluster.Path(), tenancyv1alpha1.WorkspaceTypeName("org")))
	testClient := cli.Cluster(testOrgPath)

	suite.createAccount(ctx, testClient, testAccount1Name, accountv1alpha1.AccountTypeAccount, suite.T())
	suite.createAccount(ctx, testClient, testAccount2Name, accountv1alpha1.AccountTypeAccount, suite.T())

	_, testAccount1Path := envtest.NewWorkspaceFixture(suite.T(), cli, testOrgPath, envtest.WithName(testAccount1Name), envtest.WithType(core.RootCluster.Path(), tenancyv1alpha1.WorkspaceTypeName("account")))
	_, testAccount2Path := envtest.NewWorkspaceFixture(suite.T(), cli, testOrgPath, envtest.WithName(testAccount2Name), envtest.WithType(core.RootCluster.Path(), tenancyv1alpha1.WorkspaceTypeName("account")))

	testAccount1Client := cli.Cluster(testAccount1Path)
	testAccount2Client := cli.Cluster(testAccount2Path)

	suite.createAccountInfo(ctx, testAccount1Client, testAccount1Name, testOrgName, testAccount1Path, testOrgPath, suite.T())
	suite.createAccountInfo(ctx, testAccount2Client, testAccount2Name, testOrgName, testAccount2Path, testOrgPath, suite.T())

	apiBinding1 := suite.createTestAPIBinding(ctx, testAccount1Client, apiExportName, suite.platformMeshSysPath.String(), apiExportName)
	apiBinding2 := suite.createTestAPIBinding(ctx, testAccount2Client, apiExportName, suite.platformMeshSysPath.String(), apiExportName)

	expectedModelName := fmt.Sprintf("%s-%s", pluralResourceSchemaName, testOrgName)
	var model securityv1alpha1.AuthorizationModel
	suite.Assert().Eventually(func() bool {
		err := suite.platformMeshSystemClient.Get(ctx, client.ObjectKey{Name: expectedModelName}, &model)
		return err == nil
	}, 10*time.Second, 200*time.Millisecond, "authorizationModel should exist after reconciliations")

	var testApiBinding1, testApiBinding2 kcpapiv1alpha1.APIBinding
	suite.Require().NoError(testAccount1Client.Get(ctx, client.ObjectKey{Name: apiBinding1.Name}, &testApiBinding1))
	suite.Require().NoError(testAccount2Client.Get(ctx, client.ObjectKey{Name: apiBinding2.Name}, &testApiBinding2))

	expectedFinalizers := []string{"apis.kcp.io/apibinding-finalizer", "core.platform-mesh.io/apibinding-finalizer"}

	suite.Assert().Eventually(func() bool {
		var testApiBinding1, testApiBinding2 kcpapiv1alpha1.APIBinding
		if err := testAccount1Client.Get(ctx, client.ObjectKey{Name: apiBinding1.Name}, &testApiBinding1); err != nil {
			return false
		}
		if err := testAccount2Client.Get(ctx, client.ObjectKey{Name: apiBinding2.Name}, &testApiBinding2); err != nil {
			return false
		}
		return suite.Equal(expectedFinalizers, testApiBinding1.Finalizers) &&
			suite.Equal(expectedFinalizers, testApiBinding2.Finalizers)
	}, 5*time.Second, 200*time.Millisecond, "APIBindings should have the expected finalizers")

	err = testAccount1Client.Delete(ctx, apiBinding1)
	suite.Require().NoError(err)

	suite.Assert().Eventually(func() bool {
		var binding kcpapiv1alpha1.APIBinding
		err := testAccount1Client.Get(ctx, client.ObjectKey{Name: apiBinding1.Name}, &binding)
		return kerrors.IsNotFound(err)
	}, 10*time.Second, 200*time.Millisecond, "APIBinding1 should be deleted")

	suite.Assert().Eventually(func() bool {
		var authModel securityv1alpha1.AuthorizationModel
		err := suite.platformMeshSystemClient.Get(ctx, client.ObjectKey{Name: expectedModelName}, &authModel)
		return err == nil && authModel.DeletionTimestamp.IsZero()
	}, 10*time.Second, 200*time.Millisecond, "authorizationModel should still exist after deleting first binding")

	err = testAccount2Client.Delete(ctx, apiBinding2)
	suite.Require().NoError(err)

	suite.Assert().Eventually(func() bool {
		var binding kcpapiv1alpha1.APIBinding
		err := testAccount2Client.Get(ctx, client.ObjectKey{Name: apiBinding2.Name}, &binding)
		return kerrors.IsNotFound(err)
	}, 10*time.Second, 200*time.Millisecond, "APIBinding2 should be deleted")

	suite.Assert().Eventually(func() bool {
		var authModel securityv1alpha1.AuthorizationModel
		err := suite.platformMeshSystemClient.Get(ctx, client.ObjectKey{Name: expectedModelName}, &authModel)
		return kerrors.IsNotFound(err)
	}, 10*time.Second, 200*time.Millisecond, "authorizationModel should be deleted after deleting both bindings")
}

func (suite *IntegrationSuite) createTestAPIResourceSchema(ctx context.Context, client client.Client, name, group, plural, singular string, scope apiextensionsv1.ResourceScope) {
	kind := strings.ToUpper(singular[:1]) + singular[1:]
	listKind := kind + "List"

	schema := &kcpapiv1alpha1.APIResourceSchema{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kcpapiv1alpha1.APIResourceSchemaSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     kind,
				ListKind: listKind,
				Plural:   plural,
				Singular: singular,
			},
			Scope: scope,
			Versions: []kcpapiv1alpha1.APIResourceVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: runtime.RawExtension{
						Raw: []byte(`{
							"description": "TestResource is a test resource for integration tests",
							"type": "object",
							"properties": {
								"apiVersion": {"type": "string"},
								"kind": {"type": "string"},
								"metadata": {"type": "object"},
								"spec": {"type": "object"}
							}
						}`),
					},
				},
			},
		},
	}

	err := client.Create(ctx, schema)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	suite.T().Logf("created test APIResourceSchema: %s", name)
}

func (suite *IntegrationSuite) createTestAPIExport(ctx context.Context, client client.Client, name string, resourceSchemas []string) {
	apiExport := &kcpapiv1alpha1.APIExport{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kcpapiv1alpha1.APIExportSpec{
			LatestResourceSchemas: resourceSchemas,
			PermissionClaims: []kcpapiv1alpha1.PermissionClaim{
				{GroupResource: kcpapiv1alpha1.GroupResource{Group: "apis.kcp.io", Resource: "apibindings"}, All: true, IdentityHash: ""},
				{GroupResource: kcpapiv1alpha1.GroupResource{Group: "apis.kcp.io", Resource: "apiexports"}, All: true, IdentityHash: ""},
				{GroupResource: kcpapiv1alpha1.GroupResource{Group: "apis.kcp.io", Resource: "apiresourceschemas"}, All: true, IdentityHash: ""},
			},
		},
	}

	err := client.Create(ctx, apiExport)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	suite.T().Logf("created test APIExport: %s", name)
}

func (suite *IntegrationSuite) createTestAPIBinding(ctx context.Context, client client.Client, name, exportPath, exportName string) *kcpapiv1alpha1.APIBinding {
	binding := &kcpapiv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kcpapiv1alpha1.APIBindingSpec{
			Reference: kcpapiv1alpha1.BindingReference{
				Export: &kcpapiv1alpha1.ExportBindingReference{
					Path: exportPath,
					Name: exportName,
				},
			},
		},
	}

	err := client.Create(ctx, binding)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	suite.T().Logf("created APIBinding '%s'", name)
	return binding
}
