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

package kcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkDir(t *testing.T) {
	dir, err := workDir()
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Dir(filepath.Dir(cwd)) // tests/framework/kcp -> tests
	require.Equal(t, filepath.Join(root, ".test", "go.platform-mesh.io_tests_framework_kcp"), dir)
}

func TestLoadKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
clusters:
- name: root
  cluster: {server: https://localhost:6443/clusters/root}
- name: base
  cluster: {server: https://localhost:6443}
users:
- name: admin
  user: {token: t}
contexts:
- name: root
  context: {cluster: root, user: admin}
- name: base
  context: {cluster: base, user: admin}
current-context: root
`), 0o600))

	cfg, err := loadKubeconfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://localhost:6443", cfg.Host, "the base context wins over the current one")
}
