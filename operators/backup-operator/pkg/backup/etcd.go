package backup

import (
	"context"
	"fmt"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LabelKCPShard      = "platform-mesh.io/kcp-shard"
	etcdSubroutineName = "etcd-capture"
	etcdCacheName      = "etcd-cache"
)

type EtcdCaptureSubroutine struct {
	name             string
	operandNamespace string
	client           ctrlruntimeclient.Client
}

func NewEtcdCaptureSubroutine(namespace string, cli ctrlruntimeclient.Client) *EtcdCaptureSubroutine {
	return &EtcdCaptureSubroutine{
		name:             etcdSubroutineName,
		operandNamespace: namespace,
		client:           cli,
	}
}

func (e *EtcdCaptureSubroutine) GetName() string {
	return e.name
}

func (e *EtcdCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	backup, ok := obj.(*v1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a v1alpha1.PlatformBackup, got a %T", obj)
	}

	log := logger.LoadLoggerFromContext(ctx)

	if !backup.Spec.Components.Etcd.Enabled {
		log.Info().
			Str("subroutine", etcdSubroutineName).
			Str("platformBackup", backup.Name).
			Bool("etcd-backup-enabled", backup.Spec.Components.Etcd.Enabled).
			Msg("etcd backup is not enabled on PlatformBackup CR")
		return subroutines.OK(), false, nil
	}

	log.Info().
		Str("subroutine", etcdSubroutineName).
		Str("platformBackup", backup.Name).
		Msg("listing all kcp-etcd shards")

	// retrieve all kcp-etcd shards
	var shards druidv1alpha1.EtcdList
	if err := e.client.List(ctx, &shards,
		ctrlruntimeclient.InNamespace(e.operandNamespace),
		ctrlruntimeclient.MatchingLabels{
			LabelKCPShard: "true"}); err != nil {
		return subroutines.OK(), false, fmt.Errorf("failed to list kcp-etcd shards: %w", err)
	}
	shards.Items = authoritativeEtcdShards(shards.Items)

	// early return in case no kcp-etcd shards exist
	if len(shards.Items) == 0 {
		log.Info().
			Str("subroutine", etcdSubroutineName).
			Str("platformBackup", backup.Name).
			Any("shards", shards.Items).
			Int("count", len(shards.Items)).
			Msg("no kcp-etcd shards found")
		return subroutines.OK(), false, nil
	}

	// check if etcd-druid has already taken etcd-backup and updated
	// the CR status accordingly
	if backup.Status.Artefacts.Etcd != nil && backup.Status.Artefacts.Etcd.Shards != nil && len(backup.Status.Artefacts.Etcd.Shards) == len(shards.Items) {
		allShardsRecorded := true
		for _, shard := range shards.Items {
			if _, recorded := backup.Status.Artefacts.Etcd.Shards[shard.Name]; !recorded {
				allShardsRecorded = false
				break
			}
		}

		if allShardsRecorded {
			log.Info().
				Str("subroutine", etcdSubroutineName).
				Str("platformBackup", backup.Name).
				Int("count", len(shards.Items)).
				Msg("etcd backup already completed")
			return subroutines.OK(), false, nil
		}
	}

	// success
	log.Info().
		Str("subroutine", etcdSubroutineName).
		Str("platformBackup", backup.Name).
		Any("shards", shards.Items).
		Int("count", len(shards.Items)).
		Msg("found kcp-etcd shards")

	// create EtcdOpsTask per kcp-etcd shard to be reconciled by etcd-druid operator
	for _, shard := range shards.Items {
		taskName := fmt.Sprintf("%s-%s-full-snapshot", backup.Name, shard.Name)

		log.Info().
			Str("subroutine", etcdSubroutineName).
			Str("platformBackup", backup.Name).
			Str("shard", shard.Name).
			Str("taskName", taskName).
			Msg("attempting to create etcd snapshot EtcdOpsTask")

		task := &druidv1alpha1.EtcdOpsTask{
			ObjectMeta: metav1.ObjectMeta{
				Name:      taskName,
				Namespace: shard.Namespace,
			},
			Spec: druidv1alpha1.EtcdOpsTaskSpec{
				EtcdName: ptr.To(shard.Name),
				Config: druidv1alpha1.EtcdOpsTaskConfig{
					OnDemandSnapshot: &druidv1alpha1.OnDemandSnapshotConfig{
						Type: druidv1alpha1.OnDemandSnapshotTypeFull,
					},
				},
			},
		}

		if err := e.client.Create(ctx, task); err != nil {
			if apierrors.IsAlreadyExists(err) {
				log.Info().
					Str("subroutine", etcdSubroutineName).
					Str("platformBackup", backup.Name).
					Str("shard", shard.Name).
					Str("taskName", taskName).
					Msg("etcd snapshot EtcdOpsTask already exists")
			} else {
				log.Error().
					Str("subroutine", etcdSubroutineName).
					Str("platformBackup", backup.Name).
					Str("shard", shard.Name).
					Str("taskName", taskName).
					Err(err).
					Msg("failed to create etcd snapshot EtcdOpsTask")
				return subroutines.OK(), false, fmt.Errorf("failed to create etcd snapshot task %s: %w", taskName, err)
			}
		} else {
			log.Info().
				Str("subroutine", etcdSubroutineName).
				Str("platformBackup", backup.Name).
				Str("shard", shard.Name).
				Str("taskName", taskName).
				Msg("successfully created etcd snapshot EtcdOpsTask")
		}
	}

	statusChanged := false
	// verify status for each EtcdOpsTask CR
	for _, shard := range shards.Items {
		taskName := fmt.Sprintf("%s-%s-full-snapshot", backup.Name, shard.Name)

		var task druidv1alpha1.EtcdOpsTask
		if err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Namespace: shard.Namespace,
			Name:      taskName,
		}, &task); err != nil {
			return subroutines.OK(), statusChanged, fmt.Errorf("failed to get etcd snapshot task %s: %w", taskName, err)
		}

		if task.Status.State == nil {
			return subroutines.StopWithRequeue(2*time.Second, fmt.Sprintf("waiting for etcd snapshot task %s", taskName)), statusChanged, nil
		}

		switch *task.Status.State {
		case druidv1alpha1.TaskStateSucceeded:
			if backup.Status.Artefacts.Etcd == nil {
				backup.Status.Artefacts.Etcd = &v1alpha1.EtcdArtefact{}
			}

			if backup.Status.Artefacts.Etcd.Shards == nil {
				backup.Status.Artefacts.Etcd.Shards = make(map[string]v1alpha1.EtcdShardArtefact)
			}

			// fetch backup key to update status accordingly
			snapshotKey, err := e.readFullSnapLeaseKey(ctx, shard.Namespace, shard.Name)
			if err != nil {
				return subroutines.OK(), statusChanged, fmt.Errorf("failed to read full snapshot lease for etcd %s: %w", shard.Name, err)
			}

			if snapshotKey == "" {
				return subroutines.StopWithRequeue(2*time.Second, fmt.Sprintf("waiting for full snapshot lease key for etcd %s", shard.Name)), statusChanged, nil
			}

			if _, ok := backup.Status.Artefacts.Etcd.Shards[shard.Name]; !ok {
				backup.Status.Artefacts.Etcd.Shards[shard.Name] = v1alpha1.EtcdShardArtefact{
					SnapshotKey:  snapshotKey,
					SnapshotTime: metav1.Now(),
				}
				statusChanged = true
			}

			log.Info().
				Str("subroutine", etcdSubroutineName).
				Str("platformBackup", backup.Name).
				Str("shard", shard.Name).
				Str("taskName", taskName).
				Msg("etcd snapshot EtcdOpsTask successful")

		case druidv1alpha1.TaskStateRejected:
			log.Info().
				Str("subroutine", etcdSubroutineName).
				Str("platformBackup", backup.Name).
				Str("shard", shard.Name).
				Str("taskName", taskName).
				Any("state", task.Status.State).
				Msg("etcd snapshot EtcdOpsTask rejected")
			return subroutines.OK(), statusChanged, fmt.Errorf("etcd snapshot task %s rejected", taskName)
		case druidv1alpha1.TaskStateFailed:
			log.Info().
				Str("subroutine", etcdSubroutineName).
				Str("platformBackup", backup.Name).
				Str("shard", shard.Name).
				Str("taskName", taskName).
				Any("state", task.Status.State).
				Msg("etcd snapshot EtcdOpsTask failed")
			return subroutines.OK(), statusChanged, fmt.Errorf("etcd snapshot task %s failed", taskName)
		default:
			return subroutines.StopWithRequeue(2*time.Second, fmt.Sprintf("waiting for etcd snapshot task %s", taskName)), statusChanged, nil
		}
	}

	// success
	log.Info().
		Str("subroutine", etcdSubroutineName).
		Str("platformBackup", backup.Name).
		Msg("etcd snapshot tasks succeeded")
	return subroutines.OK(), statusChanged, nil
}

// authoritativeEtcdShards removes etcd-cache from the snapshot set. The cache
// contains derived KCP state and is recreated empty by restore.
func authoritativeEtcdShards(shards []druidv1alpha1.Etcd) []druidv1alpha1.Etcd {
	authoritative := make([]druidv1alpha1.Etcd, 0, len(shards))
	for _, shard := range shards {
		if shard.Name != etcdCacheName {
			authoritative = append(authoritative, shard)
		}
	}
	return authoritative
}

func (e *EtcdCaptureSubroutine) readFullSnapLeaseKey(ctx context.Context, namespace, etcdName string) (string, error) {
	var lease coordinationv1.Lease

	leaseName := druidv1alpha1.GetFullSnapshotLeaseName(
		metav1.ObjectMeta{Name: etcdName},
	)

	if err := e.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: leaseName, Namespace: namespace}, &lease); err != nil {
		return "", ctrlruntimeclient.IgnoreNotFound(err)
	}

	if lease.Spec.HolderIdentity == nil {
		return "", nil
	}

	return *lease.Spec.HolderIdentity, nil
}
