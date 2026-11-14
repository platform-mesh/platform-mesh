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

// Package migrate implements "pmctl migrate": the changes to an installation
// that an upgrade of Platform Mesh does not make by itself.
//
// Every migration has a cut-off version, the first release that needs it.
// "pmctl migrate --from <installed version>" runs the migrations with a
// cut-off after that version, in the order of their cut-offs. A migration
// has a part that runs before the upgrade and can have one that runs after
// it (--phase).
package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

const (
	// PhaseBefore and PhaseAfter are the values of --phase.
	PhaseBefore = "before"
	PhaseAfter  = "after"

	defaultNamespace = "platform-mesh-system"
)

var long = `Run the migrations an upgrade of Platform Mesh needs.

An upgrade replaces the charts and the images. Some releases also need a
change the upgrade does not make by itself, such as moving data to another
database. Each of those is a migration with a cut-off version, the first
release that needs it: an installation older than the cut-off has to run the
migration when it is upgraded to that release or a later one.

Run migrate before the upgrade, with the version that is installed (--from;
read from the installation when left out). It runs every migration with a
cut-off after that version, oldest first. Run it again with --phase=after
once the upgrade is rolled out, with the same --from: some migrations have
something left to check or clean up on the new version.

A migration can stop a service of Platform Mesh until the upgrade is done:
plan a maintenance window. --dry-run lists what would run.

migrate changes the installation profile (ConfigMap platform-mesh-profile) in
the cluster. If the profile is managed from somewhere else, for example a
GitOps repository, make the same changes there or they are reverted.`

var example = `  # List the migrations the installed version needs
  pmctl migrate --dry-run

  # Before upgrading from v26.10.2
  pmctl migrate --from v26.10.2

  # After the upgrade is rolled out
  pmctl migrate --from v26.10.2 --phase after

  # Any patch of 26.10, against the cluster of "pmctl quickstart"
  pmctl migrate --from v26.10.x --kubeconfig pm-quickstart.kubeconfig`

// Options holds the flags and dependencies of "pmctl migrate".
type Options struct {
	genericiooptions.IOStreams

	// Kubeconfig of the cluster Platform Mesh is installed in; empty
	// follows the rules of kubectl (KUBECONFIG, ~/.kube/config).
	Kubeconfig string
	// Namespace Platform Mesh is installed in.
	Namespace string
	// From is the version that is, or before the upgrade was, installed.
	// Empty reads it from the installation; Run sets it then.
	From string
	// Phase is PhaseBefore or PhaseAfter.
	Phase string
	// BackupDir is where migrations keep what they save: dumps, the
	// profile before they changed it.
	BackupDir string
	// DryRun lists the migrations instead of running them.
	DryRun bool
	// Timeout bounds the whole run.
	Timeout time.Duration
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,
		Namespace: defaultNamespace,
		Phase:     PhaseBefore,
		BackupDir: "pmctl-migrate",
		Timeout:   30 * time.Minute,
	}
}

// NewMigrateCommand returns the "pmctl migrate" command.
func NewMigrateCommand(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)
	cmd := &cobra.Command{
		Use:     "migrate",
		Short:   "Run the migrations an upgrade of Platform Mesh needs",
		Long:    long,
		Example: example,
		Args:    cobra.NoArgs,
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
	cmd.Flags().StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Kubeconfig of the cluster Platform Mesh is installed in (default: as kubectl).")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", o.Namespace, "Namespace Platform Mesh is installed in.")
	cmd.Flags().StringVar(&o.From, "from", o.From, "Version the installation is upgraded from, e.g. v26.10.2 or v26.10.x (default: the installed version; required with --phase=after).")
	cmd.Flags().StringVar(&o.Phase, "phase", o.Phase, "Part of the migrations to run: before the upgrade, or after it. One of: before|after.")
	cmd.Flags().StringVar(&o.BackupDir, "backup-dir", o.BackupDir, "Directory the migrations keep their backups in. Use the same one for both phases.")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", o.DryRun, "List the migrations that would run, and change nothing.")
	cmd.Flags().DurationVar(&o.Timeout, "timeout", o.Timeout, "Time the migrations may take.")
	return cmd
}

// Complete fills in the fields that are derived from the command line.
func (o *Options) Complete(_ *cobra.Command, _ []string) error {
	return nil
}

// Validate checks the options.
func (o *Options) Validate() error {
	if o.Namespace == "" {
		return errors.New("--namespace must not be empty")
	}
	if o.BackupDir == "" {
		return errors.New("--backup-dir must not be empty")
	}
	if o.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	switch o.Phase {
	case PhaseBefore:
	case PhaseAfter:
		// The installation is at the new version by now, which says
		// nothing about where it came from.
		if o.From == "" {
			return errors.New("--phase=after needs --from, the version the installation was upgraded from")
		}
	default:
		return fmt.Errorf("--phase must be one of before|after, got %q", o.Phase)
	}
	if o.From != "" {
		if _, err := parseVersion(o.From); err != nil {
			return fmt.Errorf("--from: %w", err)
		}
	}
	return nil
}

// Run runs the migrations the installation needs.
func (o *Options) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	env, err := newEnv(o)
	if err != nil {
		return err
	}
	if o.From == "" {
		if o.From, err = env.installedVersion(ctx); err != nil {
			return fmt.Errorf("finding the installed version (use --from to name it): %w", err)
		}
	}
	from, err := parseVersion(o.From)
	if err != nil {
		return fmt.Errorf("--from: %w", err)
	}

	todo := pending(All(), from)
	if len(todo) == 0 {
		_, _ = fmt.Fprintf(o.Out, "An installation at %s needs no migration.\n", o.From)
		return nil
	}
	for _, m := range todo {
		run := m.Before
		if o.Phase == PhaseAfter {
			run = m.After
		}
		_, _ = fmt.Fprintf(o.ErrOut, "Migration %s (needed from v%s on, %s the upgrade): %s\n", m.Name, m.Version, o.Phase, m.Summary)
		switch {
		case run == nil:
			_, _ = fmt.Fprintf(o.ErrOut, "   nothing to do %s the upgrade\n", o.Phase)
		case o.DryRun:
			_, _ = fmt.Fprintln(o.ErrOut, "   not run: --dry-run")
		default:
			if err := run(ctx, env); err != nil {
				return fmt.Errorf("migration %s: %w", m.Name, err)
			}
		}
	}
	return nil
}
