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

package backup

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	cnpgBackupBucket             = "cnpg-backups"
	cnpgSecretName               = "cnpg-backup-s3"
	cnpgAccessKeyIDSecretKey     = "ACCESS_KEY_ID"
	cnpgSecretAccessKeySecretKey = "SECRET_ACCESS_KEY"
)

var cnpgSystemIDPattern = regexp.MustCompile(`(?m)"?systemid"?\s*[=:]\s*"?([0-9]+)`)

type CNPGCaptureSubroutine struct {
	name             string
	operandNamespace string
	clusters         []string
	client           ctrlruntimeclient.Client
}

func NewCNPGCaptureSubroutine(namespace string, cli ctrlruntimeclient.Client) *CNPGCaptureSubroutine {
	return &CNPGCaptureSubroutine{
		name:             "cnpg-capture",
		operandNamespace: namespace,
		clusters:         []string{"platform-mesh-pg"},
		client:           cli,
	}
}

func (c *CNPGCaptureSubroutine) GetName() string {
	return c.name
}

func (c *CNPGCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	backup, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a pmbackupv1alpha1.PlatformBackup, got a %T", obj)
	}

	log := logger.LoadLoggerFromContext(ctx)
	statusChanged := false

	if !backup.Spec.Components.CNPG.Enabled {
		log.Info().
			Str("subroutine", c.name).
			Str("platformBackup", backup.Name).
			Bool("cnpgBackupEnabled", backup.Spec.Components.CNPG.Enabled).
			Msg("cnpg backup is not enabled on PlatformBackup CR")
		return subroutines.OK(), false, nil
	}

	// Every cluster is inspected until its immutable S3 snapshot exists. The
	// legacy Backups status map records only the CNPG Backup CR name, so it
	// cannot by itself prove that the new snapshot was captured.
	toCapture := map[string]struct{}{}
	for _, cluster := range c.clusters {
		toCapture[cluster] = struct{}{}
	}

	// create a Backup CR for each required cluster's capture
	log.Info().
		Str("subroutine", c.name).
		Str("platformBackup", backup.Name).
		Int("clusterCount", len(toCapture)).
		Msg("creating cnpg backup resources")
	for cluster := range toCapture {
		taskName := fmt.Sprintf("%s-%s-backup", backup.Name, cluster)
		cnpgBackup := buildBackupCR(taskName, c.operandNamespace, cluster)
		if err := c.client.Create(ctx, cnpgBackup); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				log.Error().
					Str("subroutine", c.name).
					Str("platformBackup", backup.Name).
					Str("cluster", cluster).
					Str("taskName", taskName).
					Err(err).
					Msg("failed to create cnpg backup resource")
				return subroutines.OKWithRequeue(2 * time.Second), false, err
			}
			log.Info().
				Str("subroutine", c.name).
				Str("platformBackup", backup.Name).
				Str("cluster", cluster).
				Str("taskName", taskName).
				Msg("cnpg backup resource already exists")
		} else {
			log.Info().
				Str("subroutine", c.name).
				Str("platformBackup", backup.Name).
				Str("cluster", cluster).
				Str("taskName", taskName).
				Msg("created cnpg backup resource")
		}
	}

	// verify status for each cnpgv1.Backup CR
	for cluster := range toCapture {
		taskName := fmt.Sprintf("%s-%s-backup", backup.Name, cluster)
		var task cnpgv1.Backup
		if err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: c.operandNamespace, Name: taskName}, &task); err != nil {
			return subroutines.OK(), false, fmt.Errorf("failed to get cnpg task %s: %w", taskName, err)
		}

		switch task.Status.Phase {
		case cnpgv1.BackupPhaseCompleted:
			archiveReady, err := c.snapshotCNPGArchive(ctx, backup, cluster, &task)
			if err != nil {
				return subroutines.OK(), false, err
			}

			if !archiveReady {
				return subroutines.StopWithRequeue(
					10*time.Second,
					fmt.Sprintf("waiting for immutable CNPG archive snapshot for backup task %s", taskName),
				), false, nil
			}

			if backup.Status.Artefacts.CNPG == nil {
				backup.Status.Artefacts.CNPG = &pmbackupv1alpha1.CNPGArtefact{}
			}

			if backup.Status.Artefacts.CNPG.Backups == nil {
				backup.Status.Artefacts.CNPG.Backups = map[string]string{}
			}

			if backup.Status.Artefacts.CNPG.Backups[cluster] == "" {
				backup.Status.Artefacts.CNPG.Backups[cluster] = taskName
				statusChanged = true
			}
		case "",
			cnpgv1.BackupPhasePending,
			cnpgv1.BackupPhaseStarted,
			cnpgv1.BackupPhaseRunning,
			cnpgv1.BackupPhaseFinalizing:
			return subroutines.StopWithRequeue(2*time.Second, fmt.Sprintf("waiting for cnpg backup task %s", taskName)), false, nil
		case cnpgv1.BackupPhaseFailed,
			cnpgv1.BackupPhaseWalArchivingFailing,
			cnpgv1.BackupPhaseDefinitionInvalid:
			return subroutines.OK(), false, fmt.Errorf("cnpg backup task %s failed", taskName)
		default:
			return subroutines.OK(), false, fmt.Errorf("unexpected task status %s", task.Status.Phase)
		}
	}

	log.Info().
		Str("subroutine", c.name).
		Str("platformBackup", backup.Name).
		Msg("cnpg backup tasks succeeded")
	return subroutines.OK(), statusChanged, nil
}

func buildBackupCR(taskName, namespace, clusterName string) *cnpgv1.Backup {
	return &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskName,
			Namespace: namespace,
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{
				Name: clusterName,
			},
			// force wal capturing
			Target: cnpgv1.BackupTargetPrimary,
		},
	}
}

// snapshotCNPGArchive freezes the completed base backup and its WAL archive
// under a PlatformBackup-specific prefix. CNPG's normal Barman prefix is live:
// a later cluster with the same serverName can otherwise change the objects a
// PlatformRestore reads.
func (c *CNPGCaptureSubroutine) snapshotCNPGArchive(ctx context.Context, backup *pmbackupv1alpha1.PlatformBackup, clusterName string, task *cnpgv1.Backup) (bool, error) {
	beginWAL := strings.TrimSpace(task.Status.BeginWal)
	if beginWAL == "" {
		return false, nil
	}
	if task.Status.BackupID == "" || task.Status.DestinationPath == "" {
		return false, fmt.Errorf("completed CNPG backup %s has no backup ID or destination path", task.Name)
	}

	s3Client, err := c.cnpgS3Client(ctx, backup)
	if err != nil {
		return false, err
	}
	// Barman stores each server below destinationPath/serverName. Preserve that
	// layout in the immutable snapshot: CNPG recovery will append serverName
	// while locating base backups and WALs.
	archiveRoot := cnpgArchiveRoot(backup.Name)
	archivePrefix := path.Join(archiveRoot, clusterName)
	completeMarker := path.Join(archiveRoot, ".platform-mesh-complete")
	if _, err := s3Client.StatObject(ctx, cnpgBackupBucket, completeMarker, minio.StatObjectOptions{}); err == nil {
		return true, nil
	} else if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		// fall through for the normal not-found case; every other error means
		// the operator could not determine snapshot completion safely.
		return false, fmt.Errorf("stat CNPG archive completion marker: %w", err)
	}

	sourceBucket, sourcePrefix, err := barmanArchiveLocation(task.Status.DestinationPath, task.Status.ServerName, clusterName)
	if err != nil {
		return false, err
	}
	walsPrefix := path.Join(sourcePrefix, "wals") + "/"
	beginWALFound := false
	beginWALKey := ""
	basePrefix := path.Join(sourcePrefix, "base", task.Status.BackupID) + "/"

	for object := range s3Client.ListObjects(ctx, sourceBucket, minio.ListObjectsOptions{
		Prefix:    sourcePrefix + "/",
		Recursive: true,
	}) {
		if object.Err != nil {
			return false, fmt.Errorf("list CNPG archive objects under s3://%s/%s: %w", sourceBucket, sourcePrefix, object.Err)
		}

		objectName := path.Base(object.Key)
		if objectName == beginWAL || objectName == beginWAL+".gz" {
			beginWALFound = true
			beginWALKey = object.Key
		}
		if !strings.HasPrefix(object.Key, basePrefix) && !strings.HasPrefix(object.Key, walsPrefix) {
			continue
		}

		relativeKey := strings.TrimPrefix(object.Key, sourcePrefix+"/")
		if _, err := s3Client.CopyObject(ctx,
			minio.CopyDestOptions{Bucket: cnpgBackupBucket, Object: path.Join(archivePrefix, relativeKey)},
			minio.CopySrcOptions{Bucket: sourceBucket, Object: object.Key},
		); err != nil {
			return false, fmt.Errorf("copy CNPG archive object %s: %w", object.Key, err)
		}
	}

	if !beginWALFound {
		return false, nil
	}

	expectedSystemID, err := cnpgBackupSystemID(ctx, s3Client, sourceBucket, path.Join(sourcePrefix, "base", task.Status.BackupID, "backup.info"))
	if err != nil {
		return false, err
	}
	walMatches, err := cnpgWALMatchesSystemID(ctx, s3Client, sourceBucket, beginWALKey, expectedSystemID)
	if err != nil {
		return false, err
	}
	if !walMatches {
		return false, fmt.Errorf("CNPG archive is inconsistent: base backup %s and required WAL %s have different PostgreSQL system IDs", task.Status.BackupID, beginWAL)
	}
	if _, err := s3Client.PutObject(ctx, cnpgBackupBucket, completeMarker, strings.NewReader(task.Status.BackupID+"\n"), int64(len(task.Status.BackupID)+1), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		return false, fmt.Errorf("write CNPG archive completion marker: %w", err)
	}

	logger.LoadLoggerFromContext(ctx).Info().
		Str("platformBackup", backup.Name).
		Str("cluster", clusterName).
		Str("backupID", task.Status.BackupID).
		Str("source", fmt.Sprintf("s3://%s/%s", sourceBucket, sourcePrefix)).
		Str("destination", fmt.Sprintf("s3://%s/%s", cnpgBackupBucket, archiveRoot)).
		Msg("captured immutable CNPG archive snapshot")
	return true, nil
}

func (c *CNPGCaptureSubroutine) cnpgS3Client(ctx context.Context, backup *pmbackupv1alpha1.PlatformBackup) (*minio.Client, error) {
	secret := &corev1.Secret{}
	if err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: c.operandNamespace, Name: cnpgSecretName}, secret); err != nil {
		return nil, fmt.Errorf("get CNPG backup secret %s/%s: %w", c.operandNamespace, cnpgSecretName, err)
	}
	accessKeyID, err := secretString(secret, cnpgAccessKeyIDSecretKey)
	if err != nil {
		return nil, err
	}
	secretAccessKey, err := secretString(secret, cnpgSecretAccessKeySecretKey)
	if err != nil {
		return nil, err
	}
	endpoint, secure := normalizeS3Endpoint(backup.Spec.Storage.S3.Endpoint)
	region := backup.Spec.Storage.S3.Region
	if region == "" {
		region = "us-east-1"
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKeyID, secretAccessKey, ""), Secure: secure, Region: region})
	if err != nil {
		return nil, fmt.Errorf("create CNPG S3 client: %w", err)
	}
	return client, nil
}

func barmanArchiveLocation(destinationPath, serverName, fallbackServerName string) (string, string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(destinationPath), "s3://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid CNPG Barman destination path %q", destinationPath)
	}
	if serverName == "" {
		serverName = fallbackServerName
	}
	return parts[0], path.Join(parts[1], serverName), nil
}

func cnpgArchiveRoot(platformBackupName string) string {
	return path.Join("platform-mesh", "cnpg", platformBackupName)
}

func cnpgBackupSystemID(ctx context.Context, client *minio.Client, bucket, objectKey string) (uint64, error) {
	raw, err := cnpgObjectBytes(ctx, client, bucket, objectKey)
	if err != nil {
		return 0, fmt.Errorf("read CNPG backup metadata %s: %w", objectKey, err)
	}
	matches := cnpgSystemIDPattern.FindSubmatch(raw)
	if len(matches) != 2 {
		return 0, fmt.Errorf("CNPG backup metadata %s has no system ID", objectKey)
	}
	systemID, err := strconv.ParseUint(string(matches[1]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse CNPG backup system ID: %w", err)
	}
	return systemID, nil
}

func cnpgWALMatchesSystemID(ctx context.Context, client *minio.Client, bucket, objectKey string, expectedSystemID uint64) (bool, error) {
	object, err := client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return false, fmt.Errorf("get CNPG WAL %s: %w", objectKey, err)
	}
	defer func() { _ = object.Close() }()

	var reader io.Reader = object
	if strings.HasSuffix(objectKey, ".gz") {
		gzipReader, err := gzip.NewReader(object)
		if err != nil {
			return false, fmt.Errorf("open compressed CNPG WAL %s: %w", objectKey, err)
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}

	// PostgreSQL's long WAL-page header stores the system identifier at offset
	// 24. Accept either byte order so an archive captured on another CPU
	// architecture is validated correctly.
	header := make([]byte, 32)
	if _, err := io.ReadFull(reader, header); err != nil {
		return false, fmt.Errorf("read CNPG WAL header %s: %w", objectKey, err)
	}
	return binary.LittleEndian.Uint64(header[24:32]) == expectedSystemID ||
		binary.BigEndian.Uint64(header[24:32]) == expectedSystemID, nil
}

func cnpgObjectBytes(ctx context.Context, client *minio.Client, bucket, objectKey string) ([]byte, error) {
	object, err := client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = object.Close() }()
	return io.ReadAll(object)
}

func secretString(secret *corev1.Secret, key string) (string, error) {
	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s is missing key %s", secret.Namespace, secret.Name, key)
	}

	if len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s key %s is empty", secret.Namespace, secret.Name, key)
	}

	return string(value), nil
}

func normalizeS3Endpoint(endpoint string) (string, bool) {
	endpoint = strings.TrimSpace(endpoint)

	if strings.HasPrefix(endpoint, "https://") {
		return strings.TrimPrefix(endpoint, "https://"), true
	}

	if strings.HasPrefix(endpoint, "http://") {
		return strings.TrimPrefix(endpoint, "http://"), false
	}

	return endpoint, false
}
