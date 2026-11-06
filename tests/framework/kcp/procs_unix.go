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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// serverBinaries are the processes a sharded-test-server consists of.
var serverBinaries = []string{"sharded-test-server", "kcp", "kcp-front-proxy", "cache-server"}

// proc is one line of ps.
type proc struct {
	pid, ppid, pgid int
	command         string
}

func (p proc) String() string {
	return fmt.Sprintf("pid %d (ppid %d, pgid %d): %s", p.pid, p.ppid, p.pgid, p.command)
}

// listProcs returns every process on the host.
func listProcs() ([]proc, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parseProcs(out), nil
}

func parseProcs(out []byte) []proc {
	var procs []proc
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		var ids [3]int
		ok := true
		for i := range ids {
			n, err := strconv.Atoi(fields[i])
			if err != nil {
				ok = false
				break
			}
			ids[i] = n
		}
		if !ok {
			continue
		}
		procs = append(procs, proc{pid: ids[0], ppid: ids[1], pgid: ids[2], command: strings.Join(fields[3:], " ")})
	}
	return procs
}

// isServerProcess reports whether p runs one of serverBinaries, possibly
// under a versioned name (bin/kcp-0.33.6).
func isServerProcess(p proc) bool {
	fields := strings.Fields(p.command)
	if len(fields) == 0 {
		return false
	}
	exe := filepath.Base(fields[0])
	for _, b := range serverBinaries {
		if exe == b {
			return true
		}
		if version, ok := strings.CutPrefix(exe, b+"-"); ok && version != "" && !strings.Contains(version, "-") {
			return true
		}
	}
	return false
}

// childServer returns the sharded-test-server process whose parent is pid.
func childServer(procs []proc, pid int) (proc, bool) {
	for _, p := range procs {
		if p.ppid == pid && strings.Contains(p.command, "sharded-test-server") {
			return p, true
		}
	}
	return proc{}, false
}

// orphanedServers returns the kcp test server processes left behind by a
// test binary that died without stopping its server: processes of
// serverBinaries whose command line mentions testDir (tests/.test/, where
// every server of this framework and of tests:integration:shared runs) and
// whose parent is gone (re-parented to pid 1, or not in procs). The rest of
// such a server is in the orphan's process group.
func orphanedServers(procs []proc, testDir string) []proc {
	alive := make(map[int]bool, len(procs))
	for _, p := range procs {
		alive[p.pid] = true
	}
	var orphans []proc
	for _, p := range procs {
		if !strings.Contains(p.command, testDir) || !isServerProcess(p) {
			continue
		}
		if p.ppid <= 1 || !alive[p.ppid] {
			orphans = append(orphans, p)
		}
	}
	return orphans
}

// reapOrphanedServers kills the process groups of orphanedServers. It must
// only be called while holding the server lock: every live server of this
// framework holds it, so none of them can be mistaken for an orphan.
func reapOrphanedServers(testDir string) {
	procs, err := listProcs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kcp: looking for orphaned test servers: %v\n", err)
		return
	}
	self := syscall.Getpgrp()
	for _, p := range orphanedServers(procs, testDir) {
		fmt.Fprintf(os.Stderr, "kcp: killing orphaned kcp test server (its test binary is gone) %s\n", p)
		if p.pgid > 1 && p.pgid != self {
			if err := killGroup(p.pgid, 10*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "kcp: %v\n", err)
			}
			continue
		}
		// Not in a group of its own: kill just the process.
		_ = syscall.Kill(p.pid, syscall.SIGKILL)
	}
}

// killServerGroup kills what is left of the server's process group pgid
// after the server stopped, unless that is the test binary's own group.
func killServerGroup(pgid int) error {
	if pgid <= 1 || pgid == syscall.Getpgrp() {
		return nil
	}
	return killGroup(pgid, 10*time.Second)
}

// killGroup sends SIGTERM to the process group pgid, SIGKILL after grace,
// and waits until the group is empty.
func killGroup(pgid int, grace time.Duration) error {
	if !groupAlive(pgid) {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if waitGroupGone(pgid, grace) {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if waitGroupGone(pgid, 10*time.Second) {
		return nil
	}
	return fmt.Errorf("process group %d still alive after SIGKILL", pgid)
}

func groupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for groupAlive(pgid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

// portHolders describes the processes listening on ports, for error messages.
func portHolders(ports []int) string {
	procs, _ := listProcs()
	byPID := make(map[int]proc, len(procs))
	for _, p := range procs {
		byPID[p.pid] = p
	}
	var lines []string
	for _, port := range ports {
		out, err := exec.Command("lsof", "-nP", "-t", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			lines = append(lines, fmt.Sprintf("port %d: holder unknown (lsof: %v)", port, err))
			continue
		}
		for _, field := range strings.Fields(string(out)) {
			pid, _ := strconv.Atoi(field)
			if p, ok := byPID[pid]; ok {
				lines = append(lines, fmt.Sprintf("port %d: %s", port, p))
			} else {
				lines = append(lines, fmt.Sprintf("port %d: pid %d", port, pid))
			}
		}
	}
	return strings.Join(lines, "\n")
}
