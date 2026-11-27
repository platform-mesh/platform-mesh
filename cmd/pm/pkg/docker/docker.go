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

// Package docker runs the components that cannot be linked into the process
// (the portal is a Node.js server) as containers, through the docker CLI:
// a container lives as long as the context it was started with.
package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"time"

	"k8s.io/klog/v2"
)

// Container is one "docker run".
type Container struct {
	// Name is the container name; a container of that name left behind by
	// an earlier run is removed first.
	Name  string
	Image string
	Env   map[string]string
	// User is the "uid:gid" to run as; empty for the image's own.
	User string
	// Mounts are "host:container[:ro]" bind mounts.
	Mounts []string
	// HostNetwork shares the host's network, so that the container reaches
	// what the process serves on localhost and serves on localhost itself.
	HostNetwork bool
	// Ports are "host:container" publications, for when HostNetwork is off.
	Ports []string
	Args  []string
}

// Available reports whether there is a docker CLI talking to a daemon.
func Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker is not usable: %w: %s", err, out)
	}
	return nil
}

// Run starts the container, copies its output to the log and blocks until
// it exits or ctx is done, when it stops and removes the container.
func (c Container) Run(ctx context.Context) error {
	log := klog.FromContext(ctx).WithName(c.Name)
	if err := Available(ctx); err != nil {
		return err
	}
	// Left behind when a previous process was killed.
	_ = exec.Command("docker", "rm", "-f", c.Name).Run()

	args := []string{"run", "--rm", "--name", c.Name}
	if c.HostNetwork {
		args = append(args, "--network", "host")
	}
	if c.User != "" {
		args = append(args, "--user", c.User)
	}
	for _, p := range c.Ports {
		args = append(args, "-p", p)
	}
	for _, m := range c.Mounts {
		args = append(args, "-v", m)
	}
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+c.Env[k])
	}
	args = append(args, c.Image)
	args = append(args, c.Args...)

	// Not CommandContext: killing the CLI leaves the container running.
	cmd := exec.Command("docker", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}
	go copyLines(out, func(line string) { log.Info(line) })

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			err = errors.New("exited")
		}
		return fmt.Errorf("container %s: %w", c.Name, err)
	case <-ctx.Done():
		log.Info("stopping container")
		_ = exec.Command("docker", "stop", "-t", "5", c.Name).Run()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = exec.Command("docker", "rm", "-f", c.Name).Run()
		}
		return nil
	}
}

func copyLines(r io.Reader, emit func(string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		emit(scanner.Text())
	}
}
