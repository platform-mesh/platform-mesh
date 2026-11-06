//go:build unix

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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOrphanedServers(t *testing.T) {
	procs := parseProcs([]byte(`
    1     0     1 /sbin/launchd
  100     1   100 /repo/bin/sharded-test-server -number-of-shards 2 -work-dir-path /repo/tests/.test/a
  101   100   100 /repo/bin/kcp start --root-directory=/repo/tests/.test/a/.kcp-0
  102   100   100 /repo/bin/kcp-front-proxy-0.33.6 --root-directory /repo/tests/.test/a/.kcp-front-proxy
  200   300   200 /repo/bin/sharded-test-server -number-of-shards 2 -work-dir-path /repo/tests/.test/b
  300     1   300 /usr/local/go/pkg/tool/b.test
  400     1   400 /repo/bin/kcp start --root-directory=/repo/tests/.test/c/.kcp-1
  500     1   500 /repo/bin/kcp start --root-directory=/elsewhere/.kcp-0
  600     1   600 /usr/bin/vim /repo/tests/.test/a/server.pid
  700   999   700 /repo/bin/cache-server --root-directory /repo/tests/.test/d/.kcp-cache
`))
	require.Len(t, procs, 10)

	orphans := orphanedServers(procs, "/repo/tests/.test/")
	pids := make([]int, 0, len(orphans))
	for _, p := range orphans {
		pids = append(pids, p.pid)
	}
	// 100: parent is pid 1; 400: a shard whose server died; 700: parent
	// not running. Not: 101/102 (parent alive), 200 (its test binary is
	// alive), 500 (not under tests/.test), 600 (not a kcp binary).
	require.Equal(t, []int{100, 400, 700}, pids)

	p, ok := childServer(procs, 300)
	require.True(t, ok)
	require.Equal(t, 200, p.pid)
}

// assetsDir returns TEST_KCP_ASSETS or skips: the tests below start real
// sharded-test-servers (the integration task sets it; run them with
// TEST_KCP_ASSETS=$PWD/../bin go test ./framework/kcp/).
func assetsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv(AssetsEnv)
	if dir == "" {
		t.Skipf("%s not set, not starting a kcp test server", AssetsEnv)
	}
	setDefaultEnv()
	return dir
}

// TestStopLeavesNoProcesses starts a server the way Main does and checks
// that, once stop returned, no process of it is left and its ports are free.
func TestStopLeavesNoProcesses(t *testing.T) {
	assetsDir(t)
	_, stop, err := startSharded()
	require.NoError(t, err)
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()

	dir, err := workDir()
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(dir, pidFile))
	require.NoError(t, err, "Main writes the server pid")
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)

	procs, err := listProcs()
	require.NoError(t, err)
	var group []proc
	for _, p := range procs {
		if p.pgid == pid {
			group = append(group, p)
		}
	}
	// sharded-test-server, cache server, two shards, front-proxy.
	require.GreaterOrEqual(t, len(group), 5, "server process group: %v", group)

	stop()
	stopped = true
	for _, p := range group {
		err := syscall.Kill(p.pid, 0)
		require.True(t, errors.Is(err, syscall.ESRCH), "%s survived stop", p)
	}
	require.Empty(t, busyPorts())
}

// TestReapsOrphanedServer leaves a sharded-test-server behind the way a
// killed test binary does (parent gone) and checks that freePorts kills it.
func TestReapsOrphanedServer(t *testing.T) {
	assets := assetsDir(t)
	unlock, err := acquireServerLock()
	require.NoError(t, err)
	defer unlock()
	require.Empty(t, busyPorts(), "another kcp test server is running")

	dir, err := workDir()
	require.NoError(t, err)
	dir += "-orphan"
	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// sh starts the server in the background in its own process group and
	// exits, so the server is re-parented like an orphan.
	sh := exec.Command("sh", "-c", `"$0" -number-of-shards 2 -work-dir-path "$1" >"$1/server.log" 2>&1 </dev/null & echo $!`,
		filepath.Join(assets, "sharded-test-server"), dir)
	sh.Dir = dir
	sh.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := sh.Output()
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Kill(-sh.Process.Pid, syscall.SIGKILL) })

	deadline := time.Now().Add(2 * time.Minute)
	for len(busyPorts()) < len(serverPorts) {
		require.True(t, time.Now().Before(deadline), "orphan server did not bind its ports")
		time.Sleep(500 * time.Millisecond)
	}

	require.NoError(t, freePorts(filepath.Dir(dir)+string(filepath.Separator)))
	require.Empty(t, busyPorts())
	require.True(t, errors.Is(syscall.Kill(pid, 0), syscall.ESRCH), "orphan server %d survived", pid)
	require.False(t, groupAlive(sh.Process.Pid), "orphan process group %d survived", sh.Process.Pid)
}
