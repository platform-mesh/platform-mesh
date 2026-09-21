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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"

	kcpapiv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
)

func TestVirtualWorkspacePathFromSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		slice   *kcpapiv1alpha1.APIExportEndpointSlice
		want    string
		wantErr bool
	}{
		{
			name: "kind local-setup (root.kcp.localhost) — path segment is workspace logical cluster id, varies per cluster",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://root.kcp.localhost:8443/services/apiexport/158ffh0myu3e6xhu/core.platform-mesh.io"},
					},
				},
			},
			want: "/services/apiexport/158ffh0myu3e6xhu/core.platform-mesh.io",
		},
		{
			name: "in-cluster front-proxy host from working-state reference",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://frontproxy-front-proxy.platform-mesh-system:8443/services/apiexport/2n6dxtatafypkpsg/core.platform-mesh.io"},
					},
				},
			},
			want: "/services/apiexport/2n6dxtatafypkpsg/core.platform-mesh.io",
		},
		{
			name: "path with wildcard clusters suffix from kcp",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://shard.internal:8443/services/apiexport/abc123/core.platform-mesh.io/clusters/%2A"},
					},
				},
			},
			// net/url decodes %2A in Path to '*'; kubeconfig server string uses this decoded form.
			want: "/services/apiexport/abc123/core.platform-mesh.io/clusters/*",
		},
		{
			name: "trailing slash on URL path trimmed",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "x"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://h:8443/services/apiexport/id/export-name/"},
					},
				},
			},
			want: "/services/apiexport/id/export-name",
		},
		{
			name: "first endpoint wins",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "multi"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://a:1/services/apiexport/first/export"},
						{URL: "https://b:2/services/apiexport/second/export"},
					},
				},
			},
			want: "/services/apiexport/first/export",
		},
		{
			name:    "nil slice",
			slice:   nil,
			wantErr: true,
		},
		{
			name: "no endpoints",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "empty"},
			},
			wantErr: true,
		},
		{
			name: "invalid URL",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "bad"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "://nohost"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "URL with only host no path",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "nopath"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://only.host:8443"},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := virtualWorkspacePathFromSlice(tt.slice)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("path: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestVirtualWorkspaceServerURLFromSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		slice   *kcpapiv1alpha1.APIExportEndpointSlice
		want    string
		wantErr bool
	}{
		{
			name: "status URL used 1:1 as kubeconfig server (kind / local)",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://root.kcp.localhost:8443/services/apiexport/158ffh0myu3e6xhu/core.platform-mesh.io"},
					},
				},
			},
			want: "https://root.kcp.localhost:8443/services/apiexport/158ffh0myu3e6xhu/core.platform-mesh.io",
		},
		{
			name: "real cluster provider1 URL from APIExportEndpointSlice status (docs: use URL as published)",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "kind-e2e-scoped-provider.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://localhost:8443/services/apiexport/2yrxttxw0pyrhs0z/kind-e2e-scoped-provider.platform-mesh.io"},
					},
				},
			},
			want: "https://localhost:8443/services/apiexport/2yrxttxw0pyrhs0z/kind-e2e-scoped-provider.platform-mesh.io",
		},
		{
			name: "real cluster provider2 URL from APIExportEndpointSlice status (docs: use URL as published)",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "kind-e2e-scoped-provider.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://localhost:8443/services/apiexport/7mjkv2qzlbt8rig7/kind-e2e-scoped-provider.platform-mesh.io"},
					},
				},
			},
			want: "https://localhost:8443/services/apiexport/7mjkv2qzlbt8rig7/kind-e2e-scoped-provider.platform-mesh.io",
		},
		{
			name: "in-cluster front-proxy host from slice status",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://frontproxy-front-proxy.platform-mesh-system:8443/services/apiexport/2n6dxtatafypkpsg/core.platform-mesh.io"},
					},
				},
			},
			want: "https://frontproxy-front-proxy.platform-mesh-system:8443/services/apiexport/2n6dxtatafypkpsg/core.platform-mesh.io",
		},
		{
			name: "trailing slash on URL trimmed",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "x"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://h:8443/services/apiexport/id/export-name/"},
					},
				},
			},
			want: "https://h:8443/services/apiexport/id/export-name",
		},
		{
			name: "first endpoint wins",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "multi"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://a:1/services/apiexport/first/export"},
						{URL: "https://b:2/services/apiexport/second/export"},
					},
				},
			},
			want: "https://a:1/services/apiexport/first/export",
		},
		{
			name:    "nil slice",
			slice:   nil,
			wantErr: true,
		},
		{
			name: "no endpoints",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "empty"},
			},
			wantErr: true,
		},
		{
			name: "invalid URL",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "bad"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "://nohost"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "URL with only host no path",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "nopath"},
				Status: kcpapiv1alpha1.APIExportEndpointSliceStatus{
					APIExportEndpoints: []kcpapiv1alpha1.APIExportEndpoint{
						{URL: "https://only.host:8443"},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := virtualWorkspaceServerURLFromSlice(tt.slice)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("server URL: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestAPIExportLocationFromEndpointSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		slice        *kcpapiv1alpha1.APIExportEndpointSlice
		wantName     string
		wantPath     string
		wantErr      bool
		errSubstring string
	}{
		{
			name: "local cluster core slice (spec from kubectl get … -o yaml)",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Spec: kcpapiv1alpha1.APIExportEndpointSliceSpec{
					APIExport: kcpapiv1alpha1.ExportBindingReference{
						Name: "core.platform-mesh.io",
						Path: "root:platform-mesh-system",
					},
				},
			},
			wantName: "core.platform-mesh.io",
			wantPath: "root:platform-mesh-system",
		},
		{
			name:         "empty spec.export.path",
			wantErr:      true,
			errSubstring: "empty spec.export.path",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
				Spec: kcpapiv1alpha1.APIExportEndpointSliceSpec{
					APIExport: kcpapiv1alpha1.ExportBindingReference{
						Name: "core.platform-mesh.io",
					},
				},
			},
		},
		{
			name:     "spec values returned as stored (no trim)",
			wantName: "  my-export  ",
			wantPath: "  root:custom  ",
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "slice"},
				Spec: kcpapiv1alpha1.APIExportEndpointSliceSpec{
					APIExport: kcpapiv1alpha1.ExportBindingReference{
						Name: "  my-export  ",
						Path: "  root:custom  ",
					},
				},
			},
		},
		{
			name:         "empty spec.export.name",
			wantErr:      true,
			errSubstring: `empty spec.export.name`,
			slice: &kcpapiv1alpha1.APIExportEndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "named-slice"},
				Spec: kcpapiv1alpha1.APIExportEndpointSliceSpec{
					APIExport: kcpapiv1alpha1.ExportBindingReference{},
				},
			},
		},
		{
			name:         "nil slice",
			slice:        nil,
			wantErr:      true,
			errSubstring: "nil APIExportEndpointSlice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotPath, err := apiExportLocationFromEndpointSlice(tt.slice)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errSubstring)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotName != tt.wantName || gotPath != tt.wantPath {
				t.Fatalf("got name=%q path=%q want name=%q path=%q", gotName, gotPath, tt.wantName, tt.wantPath)
			}
		})
	}
}

func buildKCPConfigForPath(cfg *rest.Config, workspacePath string) *rest.Config {
	out := rest.CopyConfig(cfg)
	h := cfg.Host
	if h == "" {
		return out
	}
	if !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
		h = "https://" + h
	}
	u, err := url.Parse(h)
	if err != nil || u.Host == "" {
		out.Host = h + "/clusters/" + workspacePath
		return out
	}
	out.Host = u.Scheme + "://" + u.Host + "/clusters/" + workspacePath
	return out
}

func TestBuildKCPConfigForPath(t *testing.T) {
	t.Parallel()
	cfg := &rest.Config{Host: "https://shard:8443/clusters/root:orgs:ws"}
	out := buildKCPConfigForPath(cfg, "root:platform-mesh-system")
	if out.Host != "https://shard:8443/clusters/root:platform-mesh-system" {
		t.Fatalf("Host: got %q", out.Host)
	}
	cfgBare := &rest.Config{Host: "shard:8443"}
	outBare := buildKCPConfigForPath(cfgBare, "root:x")
	if outBare.Host != "https://shard:8443/clusters/root:x" {
		t.Fatalf("Host (bare): got %q", outBare.Host)
	}
}

func TestResolveAPIExportVirtualWorkspaceRawPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := &rest.Config{Host: "https://kcp:8443"}
	t.Run("empty slice name", func(t *testing.T) {
		t.Parallel()
		_, err := resolveAPIExportVirtualWorkspaceRawPath(ctx, &Helper{}, cfg, "root:platform-mesh-system", "")
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected empty name error, got %v", err)
		}
	})
}

func TestWorkspaceClusterScopedServerURLJoinPath(t *testing.T) {
	t.Parallel()
	// Same shape as writeScopedKubeconfigToSecret when apiExportName is set (no endpoint slice).
	hostPort := "https://frontproxy-front-proxy.platform-mesh-system:8443"
	pcPath := "root:platform-mesh-system"
	got, err := url.JoinPath(hostPort, "clusters", pcPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://frontproxy-front-proxy.platform-mesh-system:8443/clusters/root:platform-mesh-system"
	if got != want {
		t.Fatalf("server URL: got %q want %q", got, want)
	}
}

func TestEndpointSlicePathRewrittenToFrontProxyHost(t *testing.T) {
	t.Parallel()

	// Official providersecret behavior: parse endpoint URL from slice status and keep only path,
	// then join that path with configured front-proxy host.
	endpointURL := "https://localhost:8443/services/apiexport/2yrxttxw0pyrhs0z/kind-e2e-scoped-provider.platform-mesh.io"
	address, err := url.Parse(endpointURL)
	if err != nil {
		t.Fatal(err)
	}

	hostPort := "https://frontproxy-front-proxy.platform-mesh-system:8443"
	got, err := url.JoinPath(hostPort, address.Path)
	if err != nil {
		t.Fatal(err)
	}

	want := "https://frontproxy-front-proxy.platform-mesh-system:8443/services/apiexport/2yrxttxw0pyrhs0z/kind-e2e-scoped-provider.platform-mesh.io"
	if got != want {
		t.Fatalf("server URL: got %q want %q", got, want)
	}
}

func TestRewriteScopedVirtualWorkspaceURLToFrontProxy(t *testing.T) {
	t.Parallel()

	operatorCfg := config.NewOperatorConfig()
	instance := &pmcorev1alpha1.PlatformMesh{
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{
				BaseDomain: "example.com",
				Port:       8443,
			},
		},
	}

	t.Run("in-cluster: host rewritten, path+query preserved, trailing slash trimmed", func(t *testing.T) {
		t.Parallel()
		in := "https://root.kcp.localhost:8443/services/apiexport/abc/core.platform-mesh.io/?watch=true"
		got, err := rewriteScopedVirtualWorkspaceURLToFrontProxy(in, operatorCfg, instance, false)
		if err != nil {
			t.Fatal(err)
		}
		want := "https://frontproxy-front-proxy.platform-mesh-system:8443/services/apiexport/abc/core.platform-mesh.io?watch=true"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("external: base uses exposure domain+port", func(t *testing.T) {
		t.Parallel()
		in := "https://root.kcp.localhost:8443/services/apiexport/abc/core.platform-mesh.io"
		got, err := rewriteScopedVirtualWorkspaceURLToFrontProxy(in, operatorCfg, instance, true)
		if err != nil {
			t.Fatal(err)
		}
		want := "https://kcp.api.example.com:8443/services/apiexport/abc/core.platform-mesh.io"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("external without exposure errors", func(t *testing.T) {
		t.Parallel()
		_, err := rewriteScopedVirtualWorkspaceURLToFrontProxy(
			"https://root.kcp.localhost:8443/services/apiexport/abc/core.platform-mesh.io",
			operatorCfg,
			&pmcorev1alpha1.PlatformMesh{},
			true,
		)
		if err == nil || !strings.Contains(err.Error(), "requires spec.exposure") {
			t.Fatalf("expected exposure error, got %v", err)
		}
	})
}

func TestCreateScopedKubeconfigURLForAPIExportName(t *testing.T) {
	t.Parallel()

	operatorCfg := config.NewOperatorConfig()
	instance := &pmcorev1alpha1.PlatformMesh{
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{
				BaseDomain: "localhost",
				Port:       8443,
			},
		},
	}

	t.Run("in-cluster front-proxy URL for workspace path", func(t *testing.T) {
		got, err := createScopedKubeconfigURLForAPIExportName(operatorCfg, instance, "root:providers:provider2", false)
		if err != nil {
			t.Fatal(err)
		}
		want := "https://frontproxy-front-proxy.platform-mesh-system:8443/clusters/root:providers:provider2"
		if got != want {
			t.Fatalf("server URL: got %q want %q", got, want)
		}
	})

	t.Run("external URL uses exposure domain and port", func(t *testing.T) {
		got, err := createScopedKubeconfigURLForAPIExportName(operatorCfg, instance, "root:providers:provider2", true)
		if err != nil {
			t.Fatal(err)
		}
		want := "https://kcp.api.localhost:8443/clusters/root:providers:provider2"
		if got != want {
			t.Fatalf("server URL: got %q want %q", got, want)
		}
	})
}

func TestParseScopedKubeconfigExportSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		pc          pmcorev1alpha1.ProviderConnection
		wantSlice   string
		wantExports []string
		wantErr     bool
		errContains string
	}{
		{
			name: "apiExportNames single",
			pc: pmcorev1alpha1.ProviderConnection{
				APIExportNames: []string{"core.platform-mesh.io"},
			},
			wantExports: []string{"core.platform-mesh.io"},
		},
		{
			name: "apiExportNames multiple",
			pc: pmcorev1alpha1.ProviderConnection{
				APIExportNames: []string{"core.platform-mesh.io", "extensions.platform-mesh.io"},
			},
			wantExports: []string{"core.platform-mesh.io", "extensions.platform-mesh.io"},
		},
		{
			name: "endpointSliceName only",
			pc: pmcorev1alpha1.ProviderConnection{
				EndpointSliceName: ptr.To("core.platform-mesh.io"),
			},
			wantSlice: "core.platform-mesh.io",
		},
		{
			name:        "both set",
			pc:          pmcorev1alpha1.ProviderConnection{EndpointSliceName: ptr.To("a"), APIExportNames: []string{"b"}},
			wantErr:     true,
			errContains: "only one",
		},
		{
			name:        "neither set",
			pc:          pmcorev1alpha1.ProviderConnection{},
			wantErr:     true,
			errContains: "requires endpointSliceName or apiExportNames",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSlice, gotExports, err := parseScopedKubeconfigExportSource(tt.pc)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotSlice != tt.wantSlice {
				t.Fatalf("got slice=%q want slice=%q", gotSlice, tt.wantSlice)
			}
			if len(gotExports) != len(tt.wantExports) {
				t.Fatalf("got exports=%v want exports=%v", gotExports, tt.wantExports)
			}
			for i, got := range gotExports {
				if got != tt.wantExports[i] {
					t.Fatalf("got exports[%d]=%q want=%q", i, got, tt.wantExports[i])
				}
			}
		})
	}
}

func TestMergeRootCAPEMIfMissing(t *testing.T) {
	t.Parallel()
	t.Run("empty inputs unchanged", func(t *testing.T) {
		t.Parallel()
		got, o, err := mergeRootCAPEMIfMissing([]byte("x"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "x" || o != mergeRootCAUnchangedNoMerge {
			t.Fatalf("got %q outcome %v", got, o)
		}
		got, o, err = mergeRootCAPEMIfMissing(nil, []byte("y"))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil || o != mergeRootCAUnchangedNoMerge {
			t.Fatalf("got %v outcome %v", got, o)
		}
	})
	t.Run("invalid root PEM leaves chain", func(t *testing.T) {
		t.Parallel()
		chain := []byte("intermediate-only")
		got, o, err := mergeRootCAPEMIfMissing(chain, []byte("not pem"))
		if err == nil {
			t.Fatal("expected parse error")
		}
		if string(got) != string(chain) || o != mergeRootCAUnchangedInvalidRootPEM {
			t.Fatalf("got %q outcome %v err %v", got, o, err)
		}
	})
	t.Run("append once then idempotent", func(t *testing.T) {
		t.Parallel()
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "root"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageCertSign,
			IsCA:         true,
		}
		rootDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})

		leafTemplate := template
		leafTemplate.SerialNumber = big.NewInt(2)
		leafTemplate.IsCA = false
		leafTemplate.KeyUsage = x509.KeyUsageDigitalSignature
		leafDER, err := x509.CreateCertificate(rand.Reader, &leafTemplate, &template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})

		merged, o, err := mergeRootCAPEMIfMissing(leafPEM, rootPEM)
		if err != nil {
			t.Fatal(err)
		}
		if o != mergeRootCAAppended || len(merged) <= len(leafPEM) {
			t.Fatal("expected root appended")
		}
		again, o2, err := mergeRootCAPEMIfMissing(merged, rootPEM)
		if err != nil {
			t.Fatal(err)
		}
		if o2 != mergeRootCAUnchangedAlreadyPresent || len(again) != len(merged) {
			t.Fatalf("expected idempotent merge, got outcome %v len %d vs %d", o2, len(again), len(merged))
		}
	})
}

func fakeServiceAccountToken(t *testing.T, iat, exp time.Time, saUID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"iat": iat.Unix(),
		"exp": exp.Unix(),
		"kubernetes.io": map[string]any{
			"serviceaccount": map[string]any{"uid": saUID},
		},
	})
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + "." + enc([]byte("sig"))
}

func TestScopedTokenNeedsRenewal(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour

	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "empty", token: "", want: true},
		{name: "not a JWT", token: "abc", want: true},
		{name: "payload is not base64", token: "a.%%%.c", want: true},
		{name: "payload is not JSON", token: "a." + base64.RawURLEncoding.EncodeToString([]byte("x")) + ".c", want: true},
		{name: "fresh", token: fakeServiceAccountToken(t, now.Add(-time.Hour), now.Add(week-time.Hour), "uid-1"), want: false},
		{name: "just before half-life", token: fakeServiceAccountToken(t, now.Add(-week/2+time.Minute), now.Add(week/2+time.Minute), "uid-1"), want: false},
		{name: "at half-life", token: fakeServiceAccountToken(t, now.Add(-week/2), now.Add(week/2), "uid-1"), want: true},
		{name: "expired", token: fakeServiceAccountToken(t, now.Add(-2*week), now.Add(-week), "uid-1"), want: true},
		{name: "no lifetime", token: fakeServiceAccountToken(t, now, now, "uid-1"), want: true},
		{name: "ServiceAccount was recreated", token: fakeServiceAccountToken(t, now.Add(-time.Hour), now.Add(week), "uid-old"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, scopedTokenNeedsRenewal(tt.token, "uid-1", now))
		})
	}
}

func TestBuildScopedKubeconfigReloadsTokenFromFile(t *testing.T) {
	kubeconfigBytes, err := clientcmd.Write(*buildScopedKubeconfig("https://kcp.example/clusters/root", "inline-token", nil))
	require.NoError(t, err)

	// Same layout as the mounted secret: both keys in one directory.
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, scopedKubeconfigSecretKey)
	require.NoError(t, os.WriteFile(kubeconfigPath, kubeconfigBytes, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, scopedTokenSecretKey), []byte("file-token"), 0o600))

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	require.NoError(t, err)
	assert.Equal(t, "inline-token", cfg.BearerToken)
	assert.Equal(t, filepath.Join(dir, scopedTokenSecretKey), cfg.BearerTokenFile)

	// Consumers that only get the kubeconfig bytes keep working with the inline token.
	cfg, err = clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	require.NoError(t, err)
	assert.Equal(t, "inline-token", cfg.BearerToken)
}
