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

package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// openfgaRelease is the HelmRelease and the Deployment of OpenFGA,
	// openfgaPath where its values are in the profile.
	openfgaRelease  = "openfga"
	openfgaPath     = "components.services.openfga.values"
	openfgaSelector = "app.kubernetes.io/name=openfga,app.kubernetes.io/instance=openfga"

	// The PostgreSQL the OpenFGA chart brought along, which releases
	// before 26.11 keep the data in: database postgres, user postgres.
	bundledStatefulSet = "openfga-postgres"
	bundledPod         = "openfga-postgres-0"
	bundledSecret      = "openfga-postgres"
	bundledPasswordKey = "postgres-password"
	bundledPVC         = "data-openfga-postgres-0"

	// The shared CNPG cluster of the infra chart, which has a database
	// and a role for OpenFGA since before 26.11.
	cnpgCluster    = "platform-mesh-pg"
	cnpgDatabase   = "openfga-db"
	cnpgContainer  = "postgres"
	cnpgRole       = "openfga"
	cnpgRoleSecret = "cnpg-openfga-user"

	openfgaProfileBackup = "openfga-cnpg-profile-before.yaml"
	openfgaStateFile     = "openfga-cnpg.json"
)

var (
	cnpgClusterGVR  = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	cnpgDatabaseGVR = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "databases"}
)

// withPassword runs the command that follows it with PGPASSWORD read from
// the standard input, up to a NUL: the password is in no argument and no
// environment of the exec request.
var withPassword = []string{"bash", "-c", `IFS= read -r -d "" PGPASSWORD; export PGPASSWORD; exec "$@"`, "bash"}

// openfgaCounts are the numbers of rows that have to be the same on both
// sides of the migration.
type openfgaCounts struct {
	Stores int64 `json:"stores"`
	Models int64 `json:"models"`
	Tuples int64 `json:"tuples"`
}

func (c openfgaCounts) String() string {
	return fmt.Sprintf("%d stores, %d models, %d tuples", c.Stores, c.Models, c.Tuples)
}

const openfgaCountSQL = `SELECT (SELECT count(*) FROM store) || ' ' || (SELECT count(*) FROM authorization_model) || ' ' || (SELECT count(*) FROM tuple);`

// openfgaState is what the Before part leaves in the backup directory for
// the After part, and for the user.
type openfgaState struct {
	Dump   string        `json:"dump"`
	SHA256 string        `json:"sha256"`
	Counts openfgaCounts `json:"counts"`
}

// openfgaToCNPG moves the data of OpenFGA from the PostgreSQL its chart
// brought along to the shared CNPG cluster. 26.11 points OpenFGA at CNPG and
// drops the bundled PostgreSQL; without the migration OpenFGA comes up on an
// empty database, and every role of every user is gone.
//
// Before stops OpenFGA through the profile, dumps the old database and
// restores it into CNPG. OpenFGA stays stopped: it must not write to the old
// database anymore, and only the profile of 26.11 points it at the new one.
func openfgaToCNPG() Migration {
	return Migration{
		Name:    "openfga-cnpg",
		Version: "26.11.0",
		Summary: "move the data of OpenFGA from its bundled PostgreSQL to the shared CNPG cluster; OpenFGA is stopped until the upgrade",
		Before:  openfgaBefore,
		After:   openfgaAfter,
	}
}

func openfgaBefore(ctx context.Context, e *Env) error {
	// Nothing to move when OpenFGA is not on the bundled PostgreSQL: a
	// fresh 26.11, or an installation with a database of its own.
	_, err := e.kube.AppsV1().StatefulSets(e.Namespace).Get(ctx, bundledStatefulSet, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		e.notef("no bundled PostgreSQL (StatefulSet %s): nothing to move", bundledStatefulSet)
		return nil
	}
	if err != nil {
		return err
	}
	uri, _, err := e.helmValue(ctx, openfgaRelease, "datastore", "uri")
	if err != nil {
		return err
	}
	if s, _ := uri.(string); s != "" && !strings.Contains(s, bundledStatefulSet) {
		e.notef("OpenFGA does not use the bundled PostgreSQL: nothing to move")
		return nil
	}

	// 1. The destination, before anything is stopped.
	var primary string
	var restored bool
	err = e.step("Checking the shared CNPG cluster", func() error {
		if primary, err = e.cnpgPrimary(ctx); err != nil {
			return err
		}
		empty, err := e.cnpgSQL(ctx, primary, `SELECT NOT EXISTS (SELECT FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public');`)
		if err != nil {
			return err
		}
		if empty == "t" {
			return nil
		}
		// Not empty is fine only if it is what an earlier run of this
		// migration restored, and that run left OpenFGA stopped.
		replicas, _, err := e.helmValue(ctx, openfgaRelease, "replicaCount")
		if err != nil {
			return err
		}
		if fmt.Sprint(replicas) != "0" {
			return fmt.Errorf("database %s of CNPG cluster %s is not empty while OpenFGA runs on the bundled PostgreSQL: refusing to overwrite it", cnpgRole, cnpgCluster)
		}
		restored = true
		return nil
	})
	if err != nil {
		return err
	}

	// 2. Stop OpenFGA. Through the profile: the operator and Flux undo a
	//    scale-down of the Deployment.
	var profileBackup string
	err = e.step("Stopping OpenFGA", func() error {
		if profileBackup, err = e.saveProfile(ctx, openfgaProfileBackup); err != nil {
			return err
		}
		err := e.setProfile(ctx, map[string]any{
			openfgaPath + ".replicaCount":        0,
			openfgaPath + ".autoscaling.enabled": false,
		})
		if err != nil {
			return err
		}
		return e.waitOpenFGAStopped(ctx)
	})
	if err != nil {
		return err
	}
	// From here on a failure leaves OpenFGA down.
	stopped := func(err error) error { return errStopped(err, "OpenFGA", profileBackup) }

	password, err := e.bundledPassword(ctx)
	if err != nil {
		return stopped(err)
	}
	var source openfgaCounts
	if source, err = e.bundledCounts(ctx, password); err != nil {
		return stopped(fmt.Errorf("counting the rows of the bundled PostgreSQL: %w", err))
	}

	// 3. Dump and restore, unless an earlier run did.
	state := openfgaState{Counts: source}
	if restored {
		e.notef("database %s of CNPG is not empty: taking it for what an earlier run restored", cnpgRole)
	} else {
		err = e.step("Dumping the bundled PostgreSQL", func() error {
			state.Dump, state.SHA256, err = e.dumpBundled(ctx, password)
			return err
		})
		if err != nil {
			return stopped(err)
		}
		err = e.step("Restoring into CNPG", func() error {
			return e.restoreCNPG(ctx, primary, state.Dump)
		})
		if err != nil {
			return stopped(err)
		}
	}

	// 4. Both sides hold the same.
	err = e.step("Comparing the databases", func() error {
		out, err := e.cnpgSQL(ctx, primary, openfgaCountSQL)
		if err != nil {
			return err
		}
		destination, err := parseOpenFGACounts(out)
		if err != nil {
			return err
		}
		if destination != source {
			return fmt.Errorf("CNPG has %s, the bundled PostgreSQL %s", destination, source)
		}
		return nil
	})
	if err != nil {
		return stopped(err)
	}
	if !restored {
		if err := writeJSON(filepath.Join(e.BackupDir, openfgaStateFile), state); err != nil {
			return stopped(err)
		}
		e.notef("dump: %s", state.Dump)
	}

	e.notef("moved %s", source)
	e.notef("OpenFGA is stopped. Upgrade to 26.11 now: its profile points OpenFGA at CNPG")
	e.notef("(datastore, CA volume, postgresql.enabled=false and replicaCount of the 26.11 profile).")
	e.notef("The profile as it was is in %s.", profileBackup)
	return nil
}

// openfgaAfter checks, on the new version, that OpenFGA runs on CNPG and
// still has the data.
func openfgaAfter(ctx context.Context, e *Env) error {
	err := e.step("Checking that OpenFGA runs on CNPG", func() error {
		uri, _, err := e.helmValue(ctx, openfgaRelease, "datastore", "uri")
		if err != nil {
			return err
		}
		if s, _ := uri.(string); !strings.Contains(s, cnpgCluster) {
			return fmt.Errorf("the datastore of OpenFGA does not point at CNPG cluster %s: the profile has not been switched", cnpgCluster)
		}
		return e.poll(ctx, func(ctx context.Context) (bool, string, error) {
			d, err := e.kube.AppsV1().Deployments(e.Namespace).Get(ctx, openfgaRelease, metav1.GetOptions{})
			if err != nil {
				return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
			}
			if d.Status.ObservedGeneration < d.Generation || d.Status.AvailableReplicas < 1 || d.Status.UpdatedReplicas < d.Status.Replicas {
				return false, "Deployment openfga is not available", nil
			}
			return true, "", nil
		})
	})
	if err != nil {
		return err
	}

	err = e.step("Checking the data in CNPG", func() error {
		primary, err := e.cnpgPrimary(ctx)
		if err != nil {
			return err
		}
		out, err := e.cnpgSQL(ctx, primary, openfgaCountSQL)
		if err != nil {
			return err
		}
		now, err := parseOpenFGACounts(out)
		if err != nil {
			return err
		}
		e.notef("CNPG has %s", now)

		var state openfgaState
		data, err := os.ReadFile(filepath.Join(e.BackupDir, openfgaStateFile))
		if errors.Is(err, os.ErrNotExist) {
			e.notef("no %s in %s: not compared with what was moved", openfgaStateFile, e.BackupDir)
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("%s: %w", openfgaStateFile, err)
		}
		// The platform has been writing since, so the numbers differ;
		// data that was moved does not vanish altogether though.
		if (state.Counts.Stores > 0 && now.Stores == 0) || (state.Counts.Tuples > 0 && now.Tuples == 0) {
			return fmt.Errorf("the migration moved %s, and CNPG has %s now", state.Counts, now)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if _, err := e.kube.CoreV1().PersistentVolumeClaims(e.Namespace).Get(ctx, bundledPVC, metav1.GetOptions{}); err == nil {
		e.notef("PersistentVolumeClaim %s still holds the old database: delete it once you no longer want to go back.", bundledPVC)
	}
	return nil
}

// waitOpenFGAStopped waits until the change of the profile has gone all the
// way: the HelmRelease, the Deployment, the pods.
func (e *Env) waitOpenFGAStopped(ctx context.Context) error {
	return e.poll(ctx, func(ctx context.Context) (bool, string, error) {
		replicas, _, err := e.helmValue(ctx, openfgaRelease, "replicaCount")
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		if fmt.Sprint(replicas) != "0" {
			return false, fmt.Sprintf("HelmRelease openfga still has replicaCount %v", replicas), nil
		}
		d, err := e.kube.AppsV1().Deployments(e.Namespace).Get(ctx, openfgaRelease, metav1.GetOptions{})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
			return false, "Deployment openfga is not scaled to zero yet", nil
		}
		pods, err := e.kube.CoreV1().Pods(e.Namespace).List(ctx, metav1.ListOptions{LabelSelector: openfgaSelector})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		if len(pods.Items) > 0 {
			return false, fmt.Sprintf("%d OpenFGA pods are still there", len(pods.Items)), nil
		}
		return true, "", nil
	})
}

// cnpgPrimary checks that the CNPG cluster is ready to take the data of
// OpenFGA and returns the pod of its primary.
func (e *Env) cnpgPrimary(ctx context.Context) (string, error) {
	err := e.poll(ctx, func(ctx context.Context) (bool, string, error) {
		db, err := e.dynamic.Resource(cnpgDatabaseGVR).Namespace(e.Namespace).Get(ctx, cnpgDatabase, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, "", fmt.Errorf("CNPG has no Database %s: enable cnpg with the openfga role and database in the infra values of the profile first", cnpgDatabase)
		}
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		applied, _, _ := unstructured.NestedBool(db.Object, "status", "applied")
		return applied, "Database " + cnpgDatabase + " is not applied yet", nil
	})
	if err != nil {
		return "", err
	}

	cluster, err := e.dynamic.Resource(cnpgClusterGVR).Namespace(e.Namespace).Get(ctx, cnpgCluster, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("CNPG cluster %s: %w", cnpgCluster, err)
	}
	primary, _, _ := unstructured.NestedString(cluster.Object, "status", "currentPrimary")
	if primary == "" {
		return "", fmt.Errorf("CNPG cluster %s reports no primary", cnpgCluster)
	}
	if err := e.waitPodReady(ctx, primary); err != nil {
		return "", err
	}

	writable, err := e.cnpgSQL(ctx, primary, `SELECT NOT pg_is_in_recovery() AND current_setting('transaction_read_only') = 'off';`)
	if err != nil {
		return "", err
	}
	if writable != "t" {
		return "", fmt.Errorf("the primary %s of CNPG is read-only: try again when the cluster has settled", primary)
	}
	secret, err := e.kube.CoreV1().Secrets(e.Namespace).Get(ctx, cnpgRoleSecret, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if user := string(secret.Data["username"]); user != cnpgRole {
		return "", fmt.Errorf("secret %s names the role %q, and the migration restores as %q", cnpgRoleSecret, user, cnpgRole)
	}
	owned, err := e.cnpgSQL(ctx, primary, `SELECT pg_get_userbyid(datdba) = '`+cnpgRole+`' FROM pg_database WHERE datname = current_database();`)
	if err != nil {
		return "", err
	}
	if owned != "t" {
		return "", fmt.Errorf("database %s of CNPG is not owned by the role %s", cnpgRole, cnpgRole)
	}
	return primary, nil
}

// cnpgSQL runs a statement in the OpenFGA database of CNPG and returns what
// it prints.
func (e *Env) cnpgSQL(ctx context.Context, primary, sql string) (string, error) {
	return e.output(ctx, primary, cnpgContainer, nil,
		"psql", "-X", "-U", "postgres", "-d", cnpgRole, "--set=ON_ERROR_STOP=1", "--tuples-only", "--no-align", "-c", sql)
}

func (e *Env) bundledPassword(ctx context.Context) (string, error) {
	secret, err := e.kube.CoreV1().Secrets(e.Namespace).Get(ctx, bundledSecret, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	password := string(secret.Data[bundledPasswordKey])
	if password == "" {
		return "", fmt.Errorf("secret %s has no %s", bundledSecret, bundledPasswordKey)
	}
	return password, nil
}

func (e *Env) bundledCounts(ctx context.Context, password string) (openfgaCounts, error) {
	if err := e.waitPodReady(ctx, bundledPod); err != nil {
		return openfgaCounts{}, err
	}
	command := append(append([]string{}, withPassword...),
		"psql", "-X", "-U", "postgres", "-d", "postgres", "--set=ON_ERROR_STOP=1", "--tuples-only", "--no-align", "-c", openfgaCountSQL)
	out, err := e.output(ctx, bundledPod, "", strings.NewReader(password+"\x00"), command...)
	if err != nil {
		return openfgaCounts{}, err
	}
	return parseOpenFGACounts(out)
}

// dumpBundled writes a dump of the bundled PostgreSQL to the backup
// directory and returns the file and its checksum. A dump that failed stays
// behind as .partial.
func (e *Env) dumpBundled(ctx context.Context, password string) (file, sum string, err error) {
	if err := os.MkdirAll(e.BackupDir, 0o700); err != nil {
		return "", "", err
	}
	file = filepath.Join(e.BackupDir, "openfga-"+time.Now().UTC().Format("20060102T150405Z")+".dump")
	partial, err := os.OpenFile(file+".partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", err
	}
	defer partial.Close() //nolint:errcheck // closed below on the way that matters

	hash := sha256.New()
	command := append(append([]string{}, withPassword...),
		"pg_dump", "-U", "postgres", "-d", "postgres", "--schema=public", "--format=custom", "--no-owner", "--no-acl")
	if err := e.exec(ctx, bundledPod, "", strings.NewReader(password+"\x00"), io.MultiWriter(partial, hash), command...); err != nil {
		return "", "", err
	}
	if err := partial.Close(); err != nil {
		return "", "", err
	}
	if info, err := os.Stat(partial.Name()); err != nil || info.Size() == 0 {
		return "", "", errors.New("pg_dump wrote an empty archive")
	}
	if err := os.Rename(partial.Name(), file); err != nil {
		return "", "", err
	}
	sum = hex.EncodeToString(hash.Sum(nil))
	return file, sum, os.WriteFile(file+".sha256", []byte(sum+"\n"), 0o600)
}

// restoreCNPG restores the dump into the OpenFGA database of CNPG. The whole
// archive is one transaction: a failure leaves the database as it was.
// Restoring as the role of OpenFGA makes it the owner of what is restored.
func (e *Env) restoreCNPG(ctx context.Context, primary, file string) error {
	dump, err := os.Open(file) //nolint:gosec // the file dumpBundled just wrote
	if err != nil {
		return err
	}
	defer dump.Close() //nolint:errcheck // read only
	return e.exec(ctx, primary, cnpgContainer, dump, nil,
		"pg_restore", "-U", "postgres", "-d", cnpgRole,
		"--single-transaction", "--exit-on-error", "--clean", "--if-exists", "--no-owner", "--no-acl", "--role="+cnpgRole)
}

func parseOpenFGACounts(s string) (openfgaCounts, error) {
	var c openfgaCounts
	if _, err := fmt.Sscanf(s, "%d %d %d", &c.Stores, &c.Models, &c.Tuples); err != nil {
		return c, fmt.Errorf("unexpected row counts %q: %w", s, err)
	}
	return c, nil
}

func writeJSON(file string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o600)
}
