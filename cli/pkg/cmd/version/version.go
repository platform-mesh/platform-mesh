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

// Package version implements "pmctl version".
package version

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/component-base/version"
	"sigs.k8s.io/yaml"
)

// Options holds the flags and dependencies of "pmctl version".
type Options struct {
	genericiooptions.IOStreams

	// Output is the output format: "", "json" or "yaml".
	Output string
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

// NewVersionCommand returns the "pmctl version" command.
func NewVersionCommand(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the pmctl version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run()
		},
	}
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml.")
	return cmd
}

// Complete fills in the fields that are derived from the command line.
func (o *Options) Complete(_ *cobra.Command, _ []string) error {
	return nil
}

// Validate checks the options.
func (o *Options) Validate() error {
	switch o.Output {
	case "", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("--output must be one of json|yaml, got %q", o.Output)
	}
}

// Run prints the version.
func (o *Options) Run() error {
	info := version.Get()
	switch o.Output {
	case "json":
		out, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(o.Out, string(out))
		return err
	case "yaml":
		out, err := yaml.Marshal(info)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(o.Out, string(out))
		return err
	default:
		_, err := fmt.Fprintf(o.Out, "pmctl %s\n", info.GitVersion)
		return err
	}
}
