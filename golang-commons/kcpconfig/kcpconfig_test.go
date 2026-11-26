package kcpconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/client-go/rest"
)

func TestForWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, host, workspace, want string
	}{
		{"bare server", "https://kcp:6443", "root:platform-mesh-system", "https://kcp:6443/clusters/root:platform-mesh-system"},
		{"replaces a workspace", "https://kcp:6443/clusters/root", "root:orgs", "https://kcp:6443/clusters/root:orgs"},
		{"replaces a virtual workspace path", "https://kcp:6443/services/apiexport/root/x", "root", "https://kcp:6443/clusters/root"},
		{"empty keeps the host", "https://kcp:6443/clusters/root:orgs", "", "https://kcp:6443/clusters/root:orgs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &rest.Config{Host: tc.host, BearerToken: "t"}
			got, err := ForWorkspace(in, tc.workspace)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Host)
			assert.Equal(t, tc.host, in.Host, "the input was changed")
			assert.Equal(t, "t", got.BearerToken, "the credentials were not copied")
		})
	}
}

func TestRoot(t *testing.T) {
	in := &rest.Config{Host: "https://kcp:6443/clusters/root:orgs", BearerToken: "t"}
	got, err := Root(in)
	require.NoError(t, err)
	assert.Equal(t, "https://kcp:6443", got.Host)
	assert.Equal(t, "https://kcp:6443/clusters/root:orgs", in.Host, "the input was changed")
	assert.Equal(t, "t", got.BearerToken, "the credentials were not copied")
}

func TestInvalidHost(t *testing.T) {
	_, err := ForWorkspace(&rest.Config{Host: "https://kcp:port"}, "root")
	require.Error(t, err)

	_, err = Root(&rest.Config{Host: "https://kcp:port"})
	require.Error(t, err)
}
