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

// Package cmd assembles the pmctl command tree. Every subcommand lives in
// its own package below this one and exposes a New<Name>Command constructor
// backed by an Options struct with Complete, Validate and Run.
package cmd

import (
	"github.com/spf13/cobra"

	"go.platform-mesh.io/platform-mesh/cli/pkg/cmd/quickstart"
	"go.platform-mesh.io/platform-mesh/cli/pkg/cmd/version"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewPMCtlCommand returns the pmctl root command.
func NewPMCtlCommand(streams genericiooptions.IOStreams) *cobra.Command {
	root := &cobra.Command{
		Use:   "pmctl",
		Short: "pmctl controls Platform Mesh",
		Long:  "pmctl is the command line interface of Platform Mesh.",
		// Errors are printed once by component-base's cli.Run.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.ErrOut)

	root.AddCommand(
		quickstart.NewQuickstartCommand(streams),
		version.NewVersionCommand(streams),
	)

	return root
}
