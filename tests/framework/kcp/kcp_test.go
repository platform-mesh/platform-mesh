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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
)

func TestImage(t *testing.T) {
	t.Logf("Image: %s", Image)
	require.True(t, strings.HasPrefix(Image, kcptc.DefaultImage+":v"), "Image should be kcp-testcontainers' image at a kcp release, got %q", Image)
}

func TestLogDir(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Dir(filepath.Dir(filepath.Dir(cwd))) // tests/framework/kcp -> repo root

	dir, err := logDir("unit")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "tests", ".test", "unit"), dir)
	require.DirExists(t, dir)
	require.NoError(t, os.Remove(dir))
}

func TestAdvertise(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{
		Cmd: []string{"start", "--shard-name=root", "--shard-external-url=https://front-proxy:6443", "--shard-base-url=https://root:6444"},
	}}
	require.NoError(t, advertise("https://localhost:16443", "https://localhost:16444")(req))
	require.Equal(t, []string{
		"start", "--shard-name=root", "--shard-external-url=https://localhost:16443", "--shard-base-url=https://root:6444",
		"--shard-virtual-workspace-url=https://localhost:16444",
	}, req.Cmd)
}
