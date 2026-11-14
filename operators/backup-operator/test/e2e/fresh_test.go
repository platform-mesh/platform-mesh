//go:build e2e

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

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// fresh is what the backup phase gives to the restore phase, in E2E_WORK.
type fresh struct {
	Store           cliStore `json:"store"`
	State           state    `json:"state"`
	AdminKubeconfig []byte   `json:"adminKubeconfig"`
	// ProviderKubeconfig was minted with a Kubeconfig object in the old cluster.
	ProviderKubeconfig []byte `json:"providerKubeconfig"`
}

// TestFreshCluster restores a backup into a new install of Platform Mesh with the same topology.
// fresh.sh runs it twice: E2E_FRESH=backup on the old cluster, E2E_FRESH=restore on the new one.
func TestFreshCluster(t *testing.T) {
	phase, work := os.Getenv("E2E_FRESH"), os.Getenv("E2E_WORK")
	if phase == "" || work == "" {
		t.Skip("run with fresh.sh, it sets E2E_FRESH and E2E_WORK")
	}
	file := filepath.Join(work, "fresh.json")

	switch phase {
	case "backup":
		e := newEnv(t)
		e.waitHealthy(20 * time.Minute)
		raw := make([]byte, 16)
		_, _ = rand.Read(raw)
		store := cliStore{Prefix: "fresh-" + time.Now().Format("20060102-150405"), KeyFile: filepath.Join(work, "backup.key")}
		require.NoError(t, os.WriteFile(store.KeyFile, []byte(hex.EncodeToString(raw)), 0o600))

		f := fresh{Store: store, State: e.populate(store.Prefix)}
		f.ProviderKubeconfig = e.secret("bk-provider-kubeconfig")
		f.AdminKubeconfig = e.secret("kubeconfig-kcp-admin")
		e.cliBackup(store, "fresh")

		data, err := json.Marshal(f)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(file, data, 0o600))
		e.logf("backup done, state in %s", file)

	case "restore":
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		f := fresh{}
		require.NoError(t, json.Unmarshal(data, &f))

		e := newEnvWithKcp(t, f.AdminKubeconfig)
		e.providerCfg = kcpConfig(t, f.ProviderKubeconfig)
		e.waitHealthy(20 * time.Minute)
		assert.NotEmpty(t, e.diff(f.State, dataWorkspaces, dataAccounts), "the new cluster does not have the old data yet")

		require.NoError(t, e.cliRestore(f.Store, "fresh"))
		e.verify(t, f.State, "fresh")

	default:
		t.Fatalf("E2E_FRESH must be backup or restore, got %q", phase)
	}
}

func (e *env) secret(name string) []byte {
	s := &corev1.Secret{}
	require.NoError(e.t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: name}, s))
	return s.Data["kubeconfig"]
}
