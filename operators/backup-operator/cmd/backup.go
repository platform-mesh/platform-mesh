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
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	subbackup "go.platform-mesh.io/backup-operator/pkg/subroutines/backup"
	"go.platform-mesh.io/subroutines"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var backupCfg cliConfig

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "take one backup of kcp into an archive in S3, like a Backup object does",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := backupCfg.check(); err != nil {
			return err
		}
		cl, err := backupCfg.client()
		if err != nil {
			return err
		}
		opened, err := backupCfg.opened()
		if err != nil {
			return err
		}
		open := func(context.Context, *pmbackupv1alpha1.Backup) (*storage.Opened, error) { return opened, nil }
		subs := subbackup.New(cl, storage.S3Factory{EtcdEndpoint: backupCfg.EtcdEndpoint}, open)
		b := &pmbackupv1alpha1.Backup{ObjectMeta: metav1.ObjectMeta{Name: backupCfg.Name, Namespace: backupCfg.Namespace}}
		cmd.SilenceUsage = true
		return runBackup(ctrl.SetupSignalHandler(), subs, b)
	},
}

func init() {
	backupCfg.addFlags(backupCmd.Flags())
}

// runBackup calls the subroutines like the controller does, until the backup is finished.
func runBackup(ctx context.Context, subs []subroutines.Subroutine, b *pmbackupv1alpha1.Backup) error {
	last := ""
	for {
		wait := time.Duration(0)
		for _, s := range subs {
			res, err := s.(subroutines.Processor).Process(ctx, b)
			if err != nil {
				logf("%s: %v, trying again", s.GetName(), err)
				wait = 5 * time.Second
				break
			}
			if res.IsStop() || res.IsSkipAll() {
				break
			}
			if res.IsStopWithRequeue() || res.IsPending() {
				wait = res.Requeue()
				break
			}
		}
		if b.Status.Message != last {
			logf("%s", b.Status.Message)
			last = b.Status.Message
		}
		switch b.Status.Phase {
		case pmbackupv1alpha1.PhaseCompleted:
			logf("archive %s", b.Status.Archive)
			return nil
		case pmbackupv1alpha1.PhaseFailed:
			return errors.New(b.Status.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
