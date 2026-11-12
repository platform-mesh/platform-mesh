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

package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	restoreCfg  cliConfig
	restoreOpts struct {
		Only                 []string
		KcpOperator          string
		PlatformMeshOperator string
		Timeout              time.Duration
	}
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "restore a backup archive into a Platform Mesh with the same topology",
	Long: `restore downloads the archive and checks it before it changes the cluster. Then it stops
platform-mesh-operator, kcp-operator, kcp and the kcp clients, restores the secrets, the
certificates and etcd, and starts everything again. It can run again after a failure.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := restoreCfg.check(); err != nil {
			return err
		}
		parts, err := restore.ParseParts(restoreOpts.Only)
		if err != nil {
			return err
		}
		cl, err := restoreCfg.client()
		if err != nil {
			return err
		}
		opened, err := restoreCfg.opened()
		if err != nil {
			return err
		}
		cmd.SilenceUsage = true
		ctx := ctrl.SetupSignalHandler()

		dir, err := os.MkdirTemp("", "pm-restore-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir) //nolint:errcheck

		key := manifest.ArchiveKey(restoreCfg.Prefix, restoreCfg.Namespace, restoreCfg.Name)
		logf("download and check archive %s", key)
		r, err := opened.Store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		x, err := archive.Extract(r, opened.Key, dir)
		_ = r.Close()
		if err != nil {
			return err
		}

		return restore.New(cl, storage.S3Factory{EtcdEndpoint: restoreCfg.EtcdEndpoint}, x, restore.Options{
			Namespace:            restoreCfg.Namespace,
			KcpOperator:          restoreOpts.KcpOperator,
			PlatformMeshOperator: restoreOpts.PlatformMeshOperator,
			Parts:                parts,
			Timeout:              restoreOpts.Timeout,
			Log:                  logf,
		}).Run(ctx)
	},
}

func init() {
	restoreCfg.addFlags(restoreCmd.Flags())
	fs := restoreCmd.Flags()
	fs.StringSliceVar(&restoreOpts.Only, "only", nil, "parts to restore: secrets, certificates, etcd. Default is all")
	fs.StringVar(&restoreOpts.KcpOperator, "kcp-operator-deployment", "kcp-operator", "name of the kcp-operator Deployment")
	fs.StringVar(&restoreOpts.PlatformMeshOperator, "platform-mesh-operator-deployment", "platform-mesh-operator", "name of the platform-mesh-operator Deployment")
	fs.DurationVar(&restoreOpts.Timeout, "timeout", 30*time.Minute, "timeout of each step")
}
