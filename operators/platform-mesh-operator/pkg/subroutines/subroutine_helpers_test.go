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

package subroutines

import (
	"context"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"
	testfixtures "go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/test"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type permissionClaimsManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Spec       struct {
		PermissionClaims []permissionClaim `yaml:"permissionClaims"`
	} `yaml:"spec"`
}

type permissionClaim struct {
	Resource        string   `yaml:"resource"`
	Verbs           []string `yaml:"verbs"`
	All             bool     `yaml:"all"`
	Selector        selector `yaml:"selector"`
	DefaultSelector selector `yaml:"defaultSelector"`
}

type selector struct {
	MatchAll         bool              `yaml:"matchAll"`
	MatchExpressions []matchExpression `yaml:"matchExpressions"`
}

type matchExpression struct {
	Key      string `yaml:"key"`
	Operator string `yaml:"operator"`
}

type HelperTestSuite struct {
	suite.Suite

	KcpHelper
}

func TestHelperTestSuite(t *testing.T) {
	suite.Run(t, new(HelperTestSuite))
}

func countPEMCertificateBlocks(t *testing.T, b []byte) int {
	t.Helper()
	n := 0
	rest := b
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			n++
		}
	}
	return n
}

func TestAppendPEMCertsDedupe(t *testing.T) {
	t.Parallel()
	raw, err := fs.ReadFile(testfixtures.Resources, "kubeconfig.yaml")
	require.NoError(t, err)
	kc, err := clientcmd.Load(raw)
	require.NoError(t, err)
	pemA := kc.Clusters["base"].CertificateAuthorityData
	require.NotEmpty(t, pemA)

	wantN := countPEMCertificateBlocks(t, pemA)
	got := appendPEMCertsDedupe(nil, pemA)
	require.Equal(t, wantN, countPEMCertificateBlocks(t, got), "first merge should keep unique certs")

	got2 := appendPEMCertsDedupe(append([]byte(nil), got...), pemA)
	require.Equal(t, wantN, countPEMCertificateBlocks(t, got2), "appending same bundle again should not duplicate")
}

func TestIDPSecretPermissionClaims(t *testing.T) {
	t.Parallel()

	readManifest := func(t *testing.T, path string) permissionClaimsManifest {
		t.Helper()
		raw, err := fs.ReadFile(kcpmanifests.Resources, path)
		require.NoError(t, err)
		rendered, err := ReplaceTemplate(map[string]any{
			"apiExportRootTenancyKcpIoIdentityHash": "test-hash",
		}, raw)
		require.NoError(t, err)

		var manifest permissionClaimsManifest
		require.NoError(t, yaml.Unmarshal(rendered, &manifest))
		return manifest
	}

	findSecretClaim := func(manifest permissionClaimsManifest) *permissionClaim {
		for i := range manifest.Spec.PermissionClaims {
			if manifest.Spec.PermissionClaims[i].Resource == "secrets" {
				return &manifest.Spec.PermissionClaims[i]
			}
		}
		return nil
	}

	assertIDPSelector := func(t *testing.T, got selector) {
		t.Helper()
		require.False(t, got.MatchAll)
		require.ElementsMatch(t, []matchExpression{
			{Key: "core.platform-mesh.io/idp-name", Operator: "Exists"},
			{Key: "core.platform-mesh.io/client-name", Operator: "Exists"},
		}, got.MatchExpressions)
	}

	coreExport := readManifest(t, "01-platform-mesh-system/apiexport-core.platform-mesh.io.yaml")
	require.Equal(t, "apis.kcp.io/v1alpha2", coreExport.APIVersion)
	coreExportSecret := findSecretClaim(coreExport)
	require.NotNil(t, coreExportSecret)
	require.ElementsMatch(t, []string{"get", "create", "update", "delete"}, coreExportSecret.Verbs)
	assertIDPSelector(t, coreExportSecret.DefaultSelector)

	coreBinding := readManifest(t, "01-platform-mesh-system/apibinding-core.platform-mesh.io.yaml")
	coreBindingSecret := findSecretClaim(coreBinding)
	require.NotNil(t, coreBindingSecret)
	require.ElementsMatch(t, []string{"get", "create", "update", "delete"}, coreBindingSecret.Verbs)
	assertIDPSelector(t, coreBindingSecret.Selector)

	systemExport := readManifest(t, "01-platform-mesh-system/apiexport-system.platform-mesh.io.yaml")
	require.Nil(t, findSecretClaim(systemExport))

	systemBinding := readManifest(t, "01-platform-mesh-system/apibinding-system.platform-mesh.io.yaml")
	require.Nil(t, findSecretClaim(systemBinding))
}

func (s *HelperTestSuite) TestGetWorkspaceName() {
	tests := []struct {
		input       string
		expected    string
		expectError bool
	}{
		{"01-platform-mesh-system", "platform-mesh-system", false},
		{"99-abc123", "abc123", false},
		{"00-", "", true},
		{"platform-mesh-system", "", true},
		{"01platform-mesh-system", "01platform-mesh-system", true},
		{"01-platform-mesh_system", "01-platform-mesh_system", true},
		{"01-platform-mesh-system-extra", "platform-mesh-system-extra", false},
		{"1-platform-mesh-system", "1-platform-mesh-system", true},
		{"01-", "-", true},
		{"", "", true},
		{"01-Platform-Mesh-System", "Platform-Mesh-System", false},
		{"01-platform-mesh/", "01-platform-mesh/", true},
		{"../../manifests/kcp/02-orgs", "orgs", false},
		{"/operator/manifests/kcp/02-orgs", "orgs", false},
	}

	for _, tt := range tests {
		s.T().Run(tt.input, func(t *testing.T) {
			result, err := GetWorkspaceName(tt.input)
			if tt.expectError {
				s.Assert().Error(err, "input: %q", tt.input)
			} else {
				s.Assert().NoError(err, "input: %q", tt.input)
				s.Assert().Equal(tt.expected, result, "input: %q", tt.input)
			}
		})
	}
}

func (s *HelperTestSuite) TestListFiles() {
	// Create a temporary directory
	dir, err := os.MkdirTemp("", "listfiles-test")
	s.Require().NoError(err)
	defer os.RemoveAll(dir) //nolint:errcheck

	// Create some files and subdirectories
	files := []string{"file1.txt", "file2.yaml", "file3"}
	subdirs := []string{"subdir1", "subdir2"}
	for _, fname := range files {
		f, err := os.CreateTemp(dir, fname)
		s.Require().NoError(err)
		f.Close() //nolint:errcheck
	}
	for _, dname := range subdirs {
		_, err := os.MkdirTemp(dir, dname)
		s.Require().NoError(err)
	}

	// Get the expected file names (os.CreateTemp adds random suffix)
	entries, err := os.ReadDir(dir)
	s.Require().NoError(err)
	expected := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			expected = append(expected, entry.Name())
		}
	}

	// Call ListFiles
	result, err := ListFiles(os.DirFS(dir), ".")
	s.Require().NoError(err)
	s.ElementsMatch(expected, result)
}

func (s *HelperTestSuite) TestListFiles_DirectoryNotExist() {
	// Call ListFiles on a non-existent directory
	result, err := ListFiles(os.DirFS("/nonexistent/path/to"), "dir")
	s.Error(err)
	s.Empty(result)
	s.Contains(err.Error(), "Failed to read directory")
}

func (s *HelperTestSuite) TestListFiles_EmptyDirectory() {
	dir, err := os.MkdirTemp("", "listfiles-empty")
	s.Require().NoError(err)
	defer os.RemoveAll(dir) //nolint:errcheck

	result, err := ListFiles(os.DirFS(dir), ".")
	s.NoError(err)
	s.Empty(result)
}

func (s *HelperTestSuite) TestIsWorkspace() {
	tests := []struct {
		dir      string
		expected bool
	}{
		{"01-platform-mesh-system", true},
		{"99-abc123", true},
		{"00-", false},
		{"platform-mesh-system", false},
		{"01platform-mesh-system", false},
		{"01-platform-mesh_system", false},
		{"01-platform-mesh-system-extra", true},
		{"1-platform-mesh-system", false},
		{"01-", false},
		{"", false},
		{"01-PlatformMesh-System", true},
		{"01-platform-mesh-system/", false},
	}

	for _, tt := range tests {
		s.T().Run(tt.dir, func(t *testing.T) {
			result := IsWorkspace(tt.dir)
			s.Assert().Equal(tt.expected, result, "dir: %q", tt.dir)
		})
	}
}

func (s *HelperTestSuite) TestConvertToUnstructured() {
	// Create a simple MutatingWebhookConfiguration
	webhook := admissionregistrationv1.MutatingWebhookConfiguration{}
	webhook.Name = "test-webhook"
	webhook.Namespace = "test-namespace"

	// Add a webhook to the configuration
	webhook.Webhooks = []admissionregistrationv1.MutatingWebhook{
		{
			Name: "test.webhook.example.com",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				URL: new("https://example.com/webhook"),
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		},
	}

	// Convert to unstructured
	unstructuredObj, err := ConvertToUnstructured(webhook)

	// Verify no error occurred
	s.Assert().NoError(err)
	s.Assert().NotNil(unstructuredObj)

	// Verify the kind and apiVersion were set correctly
	s.Assert().Equal("MutatingWebhookConfiguration", unstructuredObj.GetKind())
	s.Assert().Equal("admissionregistration.k8s.io/v1", unstructuredObj.GetAPIVersion())

	// Verify managed fields were cleared
	s.Assert().Nil(unstructuredObj.GetManagedFields())

	// Verify name was preserved
	s.Assert().Equal("test-webhook", unstructuredObj.GetName())

	// Verify structure was preserved
	webhooks, found, err := unstructured.NestedSlice(unstructuredObj.Object, "webhooks")
	s.Assert().NoError(err)
	s.Assert().True(found)
	s.Assert().Len(webhooks, 1)

	webhookMap, ok := webhooks[0].(map[string]any)
	s.Assert().True(ok)
	name, found, err := unstructured.NestedString(webhookMap, "name")
	s.Assert().NoError(err)
	s.Assert().True(found)
	s.Assert().Equal("test.webhook.example.com", name)
}

func (s *HelperTestSuite) TestReplaceTemplate_ParseError() {
	templateData := map[string]any{
		"Name": "World",
	}
	// Invalid template syntax {{ .Name
	templateBytes := []byte("Hello, {{ .Name")

	result, err := ReplaceTemplate(templateData, templateBytes)
	s.Assert().Error(err)
	s.Assert().Contains(err.Error(), "Failed to parse template")
	s.Assert().Empty(result)
}

func (s *HelperTestSuite) TestReplaceTemplate_ExecuteError() {
	templateData := map[string]any{
		"Name": "World",
	}
	// Template tries to access a non-existent field in a struct (if data were a struct)
	// or uses an invalid action. Let's use an invalid action.
	templateBytes := []byte("Hello, {{ .Name }}. {{ if true }} Mismatched brackets")

	// First, check parsing error because the template is malformed
	_, parseErr := ReplaceTemplate(templateData, templateBytes)
	s.Assert().Error(parseErr)
	s.Assert().Contains(parseErr.Error(), "Failed to parse template")

	// Test case with missing key (text/template default behavior is to insert <no value>)
	templateBytesMissingKey := []byte("Hello, {{ .Name }}. Your ID is {{ .ID }}.")
	expectedMissingKey := []byte("Hello, World. Your ID is <no value>.")
	resultMissingKey, errMissingKey := ReplaceTemplate(templateData, templateBytesMissingKey)
	s.Assert().NoError(errMissingKey)
	s.Assert().Equal(expectedMissingKey, resultMissingKey)
}

func (s *HelperTestSuite) TestReplaceTemplate_EmptyData() {
	templateData := map[string]any{}
	templateBytes := []byte("Hello, {{ .Name }}!")
	expected := []byte("Hello, <no value>!") // Default behavior for missing keys

	result, err := ReplaceTemplate(templateData, templateBytes)
	s.Assert().NoError(err)
	s.Assert().Equal(expected, result)
}

func (s *HelperTestSuite) TestReplaceTemplate_EmptyTemplate() {
	templateData := map[string]any{
		"Name": "World",
	}
	templateBytes := []byte{}
	expected := []byte{}

	result, err := ReplaceTemplate(templateData, templateBytes)
	s.Assert().NoError(err)
	s.Assert().Equal(expected, result)
}

func (s *HelperTestSuite) TestReplaceTemplate_Success() {
	templateData := map[string]any{
		"Name": "World",
		"Age":  "30",
	}
	templateBytes := []byte("Hello, {{ .Name }}! You are {{ .Age }}.")
	expected := []byte("Hello, World! You are 30.")

	result, err := ReplaceTemplate(templateData, templateBytes)
	s.Assert().NoError(err)
	s.Assert().Equal(expected, result)
}

func (s *HelperTestSuite) SetupTest() {
	s.KcpHelper = &Helper{}
}

func (s *HelperTestSuite) TestConstructorError() {
	client, err := s.NewKcpClient(&rest.Config{}, "")
	s.Assert().Error(err)
	s.Assert().Nil(client)
}

func (s *HelperTestSuite) TestConstructorOK() {
	client, err := s.NewKcpClient(&rest.Config{
		Host: "http://server:1234",
	}, "")
	s.Assert().NoError(err)
	s.Assert().NotNil(client)
}

func (s *HelperTestSuite) TestApplyManifestFromFile() {
	cl := new(mocks.Client)
	// Server-side apply (no Get needed)
	cl.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	err := ApplyManifestFromFile(s.T().Context(), kcpmanifests.Resources, "workspace-platform-mesh-system.yaml", cl, make(map[string]any), "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Nil(err)

	// missing file
	err = ApplyManifestFromFile(s.T().Context(), kcpmanifests.Resources, "invalid", nil, make(map[string]any), "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Error(err)

	// not a YAML manifest
	err = ApplyManifestFromFile(s.T().Context(), os.DirFS("."), "kcpsetup.go", nil, make(map[string]any), "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Error(err)

	cl.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("error")).Once()
	err = ApplyManifestFromFile(s.T().Context(), kcpmanifests.Resources, "workspace-platform-mesh-system.yaml", cl, make(map[string]any), "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Error(err)

	cl.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	err = ApplyManifestFromFile(s.T().Context(), kcpmanifests.Resources, "02-root/workspace-orgs.yaml", cl, make(map[string]any), "root:orgs", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Nil(err)

	cl.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	templateData := map[string]any{
		".account-operator.webhooks.platform-mesh.io.ca-bundle": "CABundle",
	}

	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(s.T().Context(), keys.ConfigCtxKey, operatorCfg)
	err = ApplyManifestFromFile(ctx, kcpmanifests.Resources, "04-platform-mesh-system/mutatingwebhookconfiguration-admissionregistration.k8s.io.yaml", cl, templateData, "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Nil(err)
}

func (s *HelperTestSuite) TestApplyManifestFromFile_IgnoredResources_SkipsMatchedResource() {
	tmp, err := os.CreateTemp(s.T().TempDir(), "content-config-*.yaml")
	s.Require().NoError(err)
	_, err = tmp.WriteString(`apiVersion: ui.platform-mesh.io/v1alpha1
kind: ContentConfiguration
metadata:
  name: main-home
  namespace: default
`)
	s.Require().NoError(err)
	s.Require().NoError(tmp.Close())

	ignored := []config.IgnoredResource{
		{GroupVersionKind: schema.GroupVersionKind{Group: "ui.platform-mesh.io", Version: "v1alpha1", Kind: "ContentConfiguration"}, Name: "main-home"},
	}

	cl := new(mocks.Client)
	// Apply must NOT be called — resource matches the ignore list.
	err = ApplyManifestFromFile(s.T().Context(), os.DirFS(filepath.Dir(tmp.Name())), filepath.Base(tmp.Name()), cl, make(map[string]any), "root", &pmcorev1alpha1.PlatformMesh{}, ignored)
	s.Assert().Nil(err)
	cl.AssertNotCalled(s.T(), "Apply")
}

func (s *HelperTestSuite) TestApplyManifestFromFile_IgnoredResources_AppliesWhenListEmpty() {
	tmp, err := os.CreateTemp(s.T().TempDir(), "content-config-*.yaml")
	s.Require().NoError(err)
	_, err = tmp.WriteString(`apiVersion: ui.platform-mesh.io/v1alpha1
kind: ContentConfiguration
metadata:
  name: main-home
  namespace: default
`)
	s.Require().NoError(err)
	s.Require().NoError(tmp.Close())

	cl := new(mocks.Client)
	cl.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	// Empty ignore list — resource must be applied.
	err = ApplyManifestFromFile(s.T().Context(), os.DirFS(filepath.Dir(tmp.Name())), filepath.Base(tmp.Name()), cl, make(map[string]any), "root", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().Nil(err)
	cl.AssertExpectations(s.T())
}

func (s *HelperTestSuite) TestApplyManifestFromFile_IgnoredResources_SkipsWithWildcardKind() {
	tmp, err := os.CreateTemp(s.T().TempDir(), "content-config-*.yaml")
	s.Require().NoError(err)
	_, err = tmp.WriteString(`apiVersion: ui.platform-mesh.io/v1alpha1
kind: ContentConfiguration
metadata:
  name: main-home
  namespace: default
`)
	s.Require().NoError(err)
	s.Require().NoError(tmp.Close())

	// Only group set — should match any Kind and Name in that group.
	ignored := []config.IgnoredResource{{GroupVersionKind: schema.GroupVersionKind{Group: "ui.platform-mesh.io"}}}

	cl := new(mocks.Client)
	err = ApplyManifestFromFile(s.T().Context(), os.DirFS(filepath.Dir(tmp.Name())), filepath.Base(tmp.Name()), cl, make(map[string]any), "root", &pmcorev1alpha1.PlatformMesh{}, ignored)
	s.Assert().Nil(err)
	cl.AssertNotCalled(s.T(), "Apply")
}
