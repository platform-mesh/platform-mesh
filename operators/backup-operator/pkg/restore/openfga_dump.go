package restore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/subroutines"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const openFGADumpKeyPrefix = "platform-mesh/openfga"

type OpenFGADumpRestoreSubroutine struct {
	client ctrlruntimeclient.Client
	config *rest.Config
}

func NewOpenFGADumpRestoreSubroutine(client ctrlruntimeclient.Client) *OpenFGADumpRestoreSubroutine {
	return &OpenFGADumpRestoreSubroutine{client: client, config: ctrl.GetConfigOrDie()}
}
func (o *OpenFGADumpRestoreSubroutine) GetName() string { return "openfga-dump-restore" }

func (o *OpenFGADumpRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*v1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected PlatformRestore, got %T", obj)
	}
	if restoreTerminal(pr) || conditionIsTrue(pr, conditionOpenFGADataRestored) {
		return subroutines.OK(), false, nil
	}
	if !conditionIsTrue(pr, conditionVeleroRestoreCompleted) {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for Velero restore"), false, nil
	}
	postgres := platformStatefulSet("openfga-postgres")
	if _, err := postgres.scale(ctx, o.client, 1); err != nil {
		return subroutines.OK(), false, err
	}
	ready, err := postgres.ready(ctx, o.client)
	if err != nil {
		return subroutines.OK(), false, err
	}
	if !ready {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for OpenFGA PostgreSQL"), false, nil
	}
	store, err := o.s3(ctx, pr.Spec.Source.Storage)
	if err != nil {
		return subroutines.OK(), false, err
	}
	object, err := store.GetObject(ctx, cnpgBackupBucket, openFGADumpObjectKey(pr.Spec.Source.BackupID), minio.GetObjectOptions{})
	if err != nil {
		return subroutines.OK(), false, err
	}
	defer object.Close()
	if _, err := object.Stat(); err != nil {
		return subroutines.OK(), false, fmt.Errorf("OpenFGA dump is missing from backup: %w", err)
	}
	database, err := o.database(ctx)
	if err != nil {
		return subroutines.OK(), false, err
	}
	command := fmt.Sprintf("PGPASSWORD=\"$POSTGRES_PASSWORD\" pg_restore --clean --if-exists --no-owner --no-privileges -U postgres -d %q", database)
	if err := o.exec(ctx, []string{"sh", "-ec", command}, object, nil); err != nil {
		return subroutines.OK(), false, fmt.Errorf("restore OpenFGA dump: %w", err)
	}
	return subroutines.OK(), markPhaseReady(pr, conditionOpenFGADataRestored, "OpenFGADataRestored", "OpenFGA PostgreSQL dump was restored from S3"), nil
}

func (o *OpenFGADumpRestoreSubroutine) database(ctx context.Context) (string, error) {
	var secret corev1.Secret
	if err := o.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: "openfga-datastore-secret"}, &secret); err != nil {
		return "", fmt.Errorf("get OpenFGA datastore Secret: %w", err)
	}
	uri, err := url.Parse(string(secret.Data["uri"]))
	if err != nil {
		return "", fmt.Errorf("parse OpenFGA datastore URI: %w", err)
	}
	database := strings.Trim(uri.Path, "/")
	if database == "" {
		return "", fmt.Errorf("OpenFGA datastore URI has no database name")
	}
	return database, nil
}

func openFGADumpObjectKey(backupID string) string {
	return fmt.Sprintf("%s/%s.dump", openFGADumpKeyPrefix, backupID)
}

func (o *OpenFGADumpRestoreSubroutine) s3(ctx context.Context, storage v1alpha1.StorageSpec) (*minio.Client, error) {
	var secret corev1.Secret
	if err := o.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: "platform-mesh-velero", Name: storage.S3.CredentialsRef.Name}, &secret); err != nil {
		return nil, err
	}
	access, secretKey, err := openFGAS3Credentials(&secret)
	if err != nil {
		return nil, err
	}
	endpoint, secure := normalizeOpenFGAS3Endpoint(storage.S3.Endpoint)
	return minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secretKey, ""), Secure: secure, Region: storage.S3.Region})
}

func openFGAS3Credentials(secret *corev1.Secret) (string, string, error) {
	if access, ok := secret.Data["ACCESS_KEY_ID"]; ok {
		key := secret.Data["SECRET_ACCESS_KEY"]
		if len(key) == 0 {
			return "", "", fmt.Errorf("S3 Secret %s/%s is missing SECRET_ACCESS_KEY", secret.Namespace, secret.Name)
		}
		return string(access), string(key), nil
	}
	var access, key string
	for _, line := range strings.Split(string(secret.Data["cloud"]), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "aws_access_key_id":
			access = strings.TrimSpace(parts[1])
		case "aws_secret_access_key":
			key = strings.TrimSpace(parts[1])
		}
	}
	if access == "" || key == "" {
		return "", "", fmt.Errorf("S3 Secret %s/%s must contain ACCESS_KEY_ID/SECRET_ACCESS_KEY or cloud AWS credentials", secret.Namespace, secret.Name)
	}
	return access, key, nil
}

func normalizeOpenFGAS3Endpoint(endpoint string) (string, bool) {
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(endpoint, "https://") {
		return strings.TrimPrefix(endpoint, "https://"), true
	}
	if strings.HasPrefix(endpoint, "http://") {
		return strings.TrimPrefix(endpoint, "http://"), false
	}
	return endpoint, false
}

func (o *OpenFGADumpRestoreSubroutine) exec(ctx context.Context, command []string, stdin io.Reader, stdout io.Writer) error {
	clientset, err := kubernetes.NewForConfig(o.config)
	if err != nil {
		return err
	}
	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(platformMeshNamespace).Name("openfga-postgres-0").SubResource("exec").VersionedParams(&corev1.PodExecOptions{Command: command, Stdin: stdin != nil, Stdout: stdout != nil, Stderr: true, TTY: false}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(o.config, "POST", req.URL())
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: &stderr})
	if err != nil {
		return fmt.Errorf("exec %q: %w: %s", command, err, stderr.String())
	}
	return nil
}
