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
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockFile serialises private test servers on one host: sharded-test-server
// binds fixed ports (front-proxy 6443, shards 6444+n, cache server 8012 and
// its etcd 8010/8011), so a second one would fail to start.
var lockFile = filepath.Join(os.TempDir(), "platform-mesh-kcp-test-server.lock")

// acquireServerLock blocks until this process holds the host-wide test
// server lock and returns its release function.
func acquireServerLock() (func(), error) {
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", lockFile, err)
	}
	fd := int(f.Fd()) //nolint:gosec // file descriptors fit in an int

	locked := make(chan error, 1)
	go func() { locked <- syscall.Flock(fd, syscall.LOCK_EX) }()
	select {
	case err = <-locked:
	case <-time.After(2 * time.Second):
		fmt.Fprintf(os.Stderr, "kcp: waiting for the kcp test server lock held by another test binary (%s)\n", lockFile)
		err = <-locked
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", lockFile, err)
	}

	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
