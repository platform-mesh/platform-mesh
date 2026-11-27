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

// pm runs Platform Mesh as one process: "pm start" is to Platform Mesh what
// "kcp start" is to kcp.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/oidc"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/start"

	genericapiserver "k8s.io/apiserver/pkg/server"
	cliflag "k8s.io/component-base/cli/flag"
)

func main() {
	cmd := &cobra.Command{
		Use:           "pm",
		Short:         "Platform Mesh in a single binary",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// The root directory decides the defaults of the kcp flags, so it is
	// read before the flags are parsed, as kcp does.
	rootDir := ".pm"
	for i, arg := range os.Args {
		if arg == "--root-directory" && i < len(os.Args)-1 {
			rootDir = os.Args[i+1]
		} else if v, ok := strings.CutPrefix(arg, "--root-directory="); ok {
			rootDir = v
		}
	}
	opts := start.NewOptions(rootDir)

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start kcp and every Platform Mesh component in this process",
		Long: `Start an embedded kcp (with embedded etcd), install the Platform Mesh
workspaces and APIs into it and run the Platform Mesh components against it,
all in this process.

State is kept in --root-directory (default .pm); the admin kubeconfig is
written to <root-directory>/admin.kubeconfig.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return start.Run(genericapiserver.SetupSignalContext(), opts)
		},
	}
	fss := cliflag.NamedFlagSets{}
	opts.AddFlags(&fss)
	for _, fs := range fss.FlagSets {
		startCmd.Flags().AddFlagSet(fs)
	}
	cmd.AddCommand(startCmd)

	var email string
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: `Print an ID token of the mock OIDC issuer of a running "pm start"`,
		Long: `Log in at the mock OIDC issuer of the "pm start" that keeps its state in
--root-directory and print the ID token, to use as a bearer token:

  kubectl --token "$(pm token)" ...`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := oidc.LoadConfig(filepath.Join(rootDir, "oidc"))
			if err != nil {
				return fmt.Errorf("no mock OIDC issuer in %s (is pm start running?): %w", rootDir, err)
			}
			token, err := cfg.Token(cmd.Context(), email)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), token)
			return nil
		},
	}
	tokenCmd.Flags().String("root-directory", rootDir, "Root directory of the running pm start.")
	tokenCmd.Flags().StringVar(&email, "email", "", "Who to log in as (default: the --mock-oidc-email of pm start).")
	cmd.AddCommand(tokenCmd)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
