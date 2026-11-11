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

package quickstart

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// DeleteOptions holds the flags and dependencies of "pmctl quickstart delete".
type DeleteOptions struct {
	genericiooptions.IOStreams
	ClusterOptions
}

// NewDeleteOptions returns DeleteOptions with defaults.
func NewDeleteOptions(streams genericiooptions.IOStreams) *DeleteOptions {
	return &DeleteOptions{
		IOStreams:      streams,
		ClusterOptions: NewOptions(streams).ClusterOptions,
	}
}

// NewDeleteCommand returns the "pmctl quickstart delete" command.
func NewDeleteCommand(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewDeleteOptions(streams)
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the local Platform Mesh installation",
		Long:  "Delete the kind cluster created by \"pmctl quickstart\", its kubeconfig and its CA certificate file, and the registry and build state of --version=main.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	o.AddFlags(cmd)
	return cmd
}

// Complete fills in the fields that are derived from the command line.
func (o *DeleteOptions) Complete(_ *cobra.Command, _ []string) error {
	o.ClusterOptions.Complete()
	return nil
}

// Validate checks the options.
func (o *DeleteOptions) Validate() error {
	return o.ClusterOptions.Validate()
}

// Run deletes the cluster and the files written next to it. A cluster that
// does not exist is not an error.
func (o *DeleteOptions) Run(ctx context.Context) error {
	provider, err := newKindProvider(o.ContainerRuntime)
	if err != nil {
		return err
	}
	if err := provider.Delete(o.Name, o.Kubeconfig); err != nil {
		return fmt.Errorf("deleting kind cluster %q: %w", o.Name, err)
	}
	// What --version=main left behind: the registry and the build state.
	if _, err := exec.LookPath("docker"); err == nil {
		removeRegistry(ctx, o.Name)
	}
	if state, err := stateDir(o.Name); err == nil {
		if err := os.RemoveAll(state); err != nil {
			return err
		}
	}
	for _, f := range []string{o.Kubeconfig, o.caFile()} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	_, _ = fmt.Fprintf(o.ErrOut, ">>> kind cluster %q deleted\n", o.Name)
	return nil
}
