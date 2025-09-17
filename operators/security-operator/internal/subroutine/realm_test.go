package subroutine

import (
	"context"
	"strings"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	repoYAML = `
apiVersion: v1
kind: Config
metadata:
  name: repo-min
spec:
  foo: bar
`
	helmReleaseYAML = `
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: helm-min
spec:
  releaseName: placeholder
  values: {}
`
	baseDomain = "portal.dev.local"
)

func newClientMock(t *testing.T, setup func(m *mocks.MockClient)) *mocks.MockClient {
	t.Helper()
	m := new(mocks.MockClient)
	if setup != nil {
		setup(m)
	}
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func testLogger() *logger.Logger {
	l, _ := logger.New(logger.DefaultConfig())
	return l
}

func trim(s string) string { return strings.TrimSpace(s) }

func TestApplyReleaseAndManifest(t *testing.T) {
	cases := []struct {
		name       string
		call       string // "release" or "manifest"
		content    string
		setupMocks func(m *mocks.MockClient)
		expectErr  bool
	}{
		{"release: invalid YAML", "release", "not: : valid: yaml", nil, true},
		{
			"release: spec scalar",
			"release",
			trim(`
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: test-release
  namespace: default
spec: "test spec"
`),
			nil,
			true,
		},
		{
			"release: patch error wrapped",
			"release",
			trim(`
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: test-release
  namespace: default
spec:
  chart:
    spec:
      chart: mychart
      version: 1.2.3
`),
			func(m *mocks.MockClient) {
				m.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("simulated patch fail")).Once()
			},
			true,
		},
		{"manifest: invalid YAML", "manifest", "this: is: : invalid: yaml", nil, true},
		{
			"manifest: patch error wrapped",
			"manifest",
			trim(`
apiVersion: v1
kind: Config
metadata:
  name: test-manifest
spec:
  foo: bar
`),
			func(m *mocks.MockClient) {
				m.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("simulated patch error for manifest")).Once()
			},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientMock := newClientMock(t, tc.setupMocks)
			ctx := context.Background()

			switch tc.call {
			case "release":
				err := applyReleaseWithValues(ctx, tc.content, clientMock, apiextensionsv1.JSON{}, "org-name")
				if tc.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			case "manifest":
				err := applyManifestWithMergedValues(ctx, tc.content, clientMock, nil)
				if tc.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			default:
				t.Fatalf("unknown call type %q", tc.call)
			}
		})
	}
}

func TestRealmSubroutine_ProcessAndFinalize(t *testing.T) {
	origRepo, origHR := repository, helmRelease
	defer func() { repository, helmRelease = origRepo, origHR }()

	t.Run("Process", func(t *testing.T) {
		t.Run("success create repo then helmrelease", func(t *testing.T) {
			t.Parallel()
			clientMock := newClientMock(t, func(m *mocks.MockClient) {
				m.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
						_, ok := obj.(*unstructured.Unstructured)
						require.True(t, ok)
						return nil
					}).Once()

				m.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
						hr := obj.(*unstructured.Unstructured)
						spec, _, _ := unstructured.NestedFieldNoCopy(hr.Object, "spec")
						specMap := spec.(map[string]interface{})
						specValues, _, _ := unstructured.NestedFieldNoCopy(specMap, "values")
						_, ok := specValues.(apiextensionsv1.JSON)
						require.True(t, ok)
						return nil
					}).Once()
			})

			repository, helmRelease = trim(repoYAML), trim(helmReleaseYAML)

			rs := NewRealmSubroutine(clientMock, baseDomain)
			lc := &kcpv1alpha1.LogicalCluster{}
			lc.Annotations = map[string]string{"kcp.io/path": "root:orgs:test"}
			res, opErr := rs.Process(context.Background(), lc)
			require.Nil(t, opErr)
			require.Equal(t, ctrl.Result{}, res)
		})

		t.Run("oci repository apply fails", func(t *testing.T) {
			t.Parallel()
			clientMock := newClientMock(t, func(m *mocks.MockClient) {
				m.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("simulated patch failure for OCI repo")).Once()
			})

			repository, helmRelease = trim(repoYAML), trim(helmReleaseYAML)
			rs := NewRealmSubroutine(clientMock, baseDomain)
			lc := &kcpv1alpha1.LogicalCluster{}
			lc.Annotations = map[string]string{"kcp.io/path": "root:orgs:test"}
			res, opErr := rs.Process(context.Background(), lc)
			require.NotNil(t, opErr)
			require.Equal(t, ctrl.Result{}, res)
		})

		t.Run("missing workspace annotation", func(t *testing.T) {
			t.Parallel()
			clientMock := newClientMock(t, nil)
			rs := NewRealmSubroutine(clientMock, baseDomain)
			lc := &kcpv1alpha1.LogicalCluster{}
			res, opErr := rs.Process(context.Background(), lc)
			require.NotNil(t, opErr)
			require.Equal(t, ctrl.Result{}, res)
		})
	})

	t.Run("Finalize - delete scenarios", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name           string
			setupMocks     func(m *mocks.MockClient)
			expectErr      bool
			expectedResult ctrl.Result
		}{
			{
				"OCI delete error",
				func(m *mocks.MockClient) {
					m.EXPECT().Delete(mock.Anything, mock.Anything).Return(errors.New("failed to delete oci repository")).Once()
				},
				true,
				ctrl.Result{},
			},
			{
				"HelmRelease delete error",
				func(m *mocks.MockClient) {
					m.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
					m.EXPECT().Delete(mock.Anything, mock.Anything).Return(errors.New("failed to delete helmRelease")).Once()
				},
				true,
				ctrl.Result{},
			},
			{
				"Both deletes succeed",
				func(m *mocks.MockClient) { m.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Twice() },
				false,
				ctrl.Result{},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				clientMock := newClientMock(t, tc.setupMocks)
				rs := NewRealmSubroutine(clientMock, baseDomain)
				lc := &kcpv1alpha1.LogicalCluster{}
				lc.Annotations = map[string]string{"kcp.io/path": "root:orgs:test"}
				res, opErr := rs.Finalize(context.Background(), lc)
				if tc.expectErr {
					require.NotNil(t, opErr)
				} else {
					require.Nil(t, opErr)
				}
				require.Equal(t, tc.expectedResult, res)
			})
		}
	})

}

func TestReplaceTemplateAndUnstructured(t *testing.T) {
	cases := []struct {
		name         string
		templateData map[string]string
		template     []byte
		expectErr    bool
		expectOutput string
	}{
		{"parse error invalid template", nil, []byte("{{"), true, ""},
		{"empty template yields empty result", map[string]string{}, []byte(""), false, ""},
		{"successful template rendering", map[string]string{"Name": "testing"}, []byte("hello {{ .Name }}"), false, "hello testing"},
		{"execute error indexing missing map", map[string]string{}, []byte(`{{ index .MissingMap "k" }}`), true, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := ReplaceTemplate(tc.templateData, tc.template)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.expectOutput != "" {
				require.Equal(t, tc.expectOutput, string(out))
			}
		})
	}

	t.Run("unstructured invalid yaml", func(t *testing.T) {
		l := testLogger()
		_, err := unstructuredFromString("not: : valid: yaml", nil, l)
		require.Error(t, err)
	})

	t.Run("unstructured template success", func(t *testing.T) {
		l := testLogger()
		templ := "kind: Test\nmetadata:\n  name: {{ .Name }}\nspec:\n  v: 1"
		out, err := ReplaceTemplate(map[string]string{"Name": "templated"}, []byte(templ))
		require.NoError(t, err)
		_, err2 := unstructuredFromString(string(out), nil, l)
		require.NoError(t, err2)
	})
}
