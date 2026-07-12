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

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/controller/coordbroker/migration"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// migrationFixture is a frame with an x86 and an arm64 provider, a VM
// migration configuration with the given stages, and a consumer with an
// x86_64 VM.
type migrationFixture struct {
	frame    *Frame
	x86      *ControlPlane
	arm      *ControlPlane
	consumer *ControlPlane
	nn       types.NamespacedName
}

// newMigrationFixture builds the fixture. The migration is not triggered.
func newMigrationFixture(t *testing.T, stages ...pmcoordbrokerv1alpha1.MigrationStage) *migrationFixture {
	t.Helper()

	frame := NewFrame(t)

	fix := &migrationFixture{
		frame: frame,
		x86:   frame.NewProvider(t, "x86"),
		arm:   frame.NewProvider(t, "arm64"),
	}
	createVMAcceptAPI(t, fix.x86, "accept-x86", "x86_64")
	createVMAcceptAPI(t, fix.arm, "accept-arm64", "arm64")

	config := &pmcoordbrokerv1alpha1.MigrationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrate-vm",
		},
		Spec: pmcoordbrokerv1alpha1.MigrationConfigurationSpec{
			From:   vmGVK,
			To:     vmGVK,
			Stages: stages,
		},
	}
	require.NoError(t, frame.CoordinationClient.Create(t.Context(), config))

	frame.StartBroker(t)

	fix.consumer = frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "x86_64",
			Memory: 512,
		},
	}
	require.NoError(t, fix.consumer.Client.Create(t.Context(), vm))
	fix.nn = types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	waitForVM(t, frame.StagingClient(t, fix.x86), fix.nn)

	return fix
}

// triggerMigration flips the VM's architecture to arm64 and waits for the
// resulting Migration.
func (fix *migrationFixture) triggerMigration(t *testing.T) *pmcoordbrokerv1alpha1.Migration {
	t.Helper()

	updateVM(t, fix.consumer.Client, fix.nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Arch = "arm64"
	})
	return waitForMigration(t, fix.frame)
}

// configMapStage builds a migration stage with a single ConfigMap template.
func configMapStage(stageName, templateName string, progress bool, successConditions ...string) pmcoordbrokerv1alpha1.MigrationStage {
	return pmcoordbrokerv1alpha1.MigrationStage{
		Name: stageName,
		Templates: map[string]runtime.RawExtension{
			templateName: {
				Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","data":{"key":"value"}}`),
			},
		},
		SuccessConditions: successConditions,
		Progress:          progress,
	}
}

// completeStageConfigMap satisfies a stage's success condition.
func completeStageConfigMap(t *testing.T, cl ctrlruntimeclient.Client, nn types.NamespacedName) {
	t.Helper()

	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		if err := cl.Get(t.Context(), nn, cm); err != nil {
			t.Logf("getting stage configmap: %v", err)
			return false
		}
		cm.Data["key"] = "done"
		if err := cl.Update(t.Context(), cm); err != nil {
			t.Logf("updating stage configmap: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)
}

// getMigration fetches the migration. Errors yield an empty migration so
// the helper is safe to call from polling closures.
func getMigration(t *testing.T, frame *Frame, name string) *pmcoordbrokerv1alpha1.Migration {
	t.Helper()

	current := &pmcoordbrokerv1alpha1.Migration{}
	if err := frame.CoordinationClient.Get(t.Context(), types.NamespacedName{Name: name}, current); err != nil {
		t.Logf("getting migration %q: %v", name, err)
	}
	return current
}

// TestMigrationMultiStage verifies stage progression, including the
// Progress flag moving the migration to InitialCompleted.
func TestMigrationMultiStage(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t,
		configMapStage("one", "a", true, `a.data.key == "done"`),
		configMapStage("two", "b", false, `b.data.key == "done"`),
	)
	migrationCR := fix.triggerMigration(t)

	cmA := types.NamespacedName{Namespace: migration.DefaultStageNamespace, Name: migrationCR.Name + "-a"}
	cmB := types.NamespacedName{Namespace: migration.DefaultStageNamespace, Name: migrationCR.Name + "-b"}

	require.Eventually(t, func() bool {
		return fix.frame.ComputeClient.Get(t.Context(), cmA, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "first stage configmap should be deployed")
	require.Eventually(t, func() bool {
		return getMigration(t, fix.frame, migrationCR.Name).Status.State == pmcoordbrokerv1alpha1.MigrationStateInitialInProgress
	}, wait.ForeverTestTimeout, time.Second, "migration should be in the initial phase")

	completeStageConfigMap(t, fix.frame.ComputeClient, cmA)
	require.Eventually(t, func() bool {
		current := getMigration(t, fix.frame, migrationCR.Name)
		return current.Status.Stage == "two" && current.Status.State == pmcoordbrokerv1alpha1.MigrationStateInitialCompleted
	}, wait.ForeverTestTimeout, time.Second, "migration should advance to the second stage with InitialCompleted")
	require.Eventually(t, func() bool {
		err := fix.frame.ComputeClient.Get(t.Context(), cmA, &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "first stage configmap should be cleaned up")
	require.Eventually(t, func() bool {
		return fix.frame.ComputeClient.Get(t.Context(), cmB, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "second stage configmap should be deployed")

	completeStageConfigMap(t, fix.frame.ComputeClient, cmB)
	require.Eventually(t, func() bool {
		return getMigration(t, fix.frame, migrationCR.Name).Status.State == pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress
	}, wait.ForeverTestTimeout, time.Second, "migration should reach the cutover")

	markVMAvailable(t, fix.frame.StagingClient(t, fix.arm), fix.nn, "arm64")
	waitForMigrationFinished(t, fix.frame, fix.x86)

	require.Eventually(t, func() bool {
		err := fix.frame.ComputeClient.Get(t.Context(), cmB, &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "second stage configmap should be cleaned up")
}

// TestMigrationStageRelatedResources verifies that related resources of the
// origin staging copy are mirrored into the compute cluster.
func TestMigrationStageRelatedResources(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t,
		configMapStage("copy-data", "dummy", false, `dummy.data.key == "done"`),
	)

	x86Staging := fix.frame.StagingClient(t, fix.x86)
	cmName := publishRelatedConfigMap(t, x86Staging, fix.nn)
	require.Eventually(t, func() bool {
		return fix.consumer.Client.Get(t.Context(), cmName, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "related configmap should be copied into the consumer workspace")

	migrationCR := fix.triggerMigration(t)

	mirrored := types.NamespacedName{Namespace: cmName.Namespace, Name: "from-" + cmName.Name}
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		if err := fix.frame.ComputeClient.Get(t.Context(), mirrored, cm); err != nil {
			t.Logf("getting mirrored configmap: %v", err)
			return false
		}
		return cm.Data["hello"] == "world"
	}, wait.ForeverTestTimeout, time.Second, "related configmap should be mirrored into the compute cluster")

	stageCM := types.NamespacedName{Namespace: migration.DefaultStageNamespace, Name: migrationCR.Name + "-dummy"}
	completeStageConfigMap(t, fix.frame.ComputeClient, stageCM)
	markVMAvailable(t, fix.frame.StagingClient(t, fix.arm), fix.nn, "arm64")
	waitForMigrationFinished(t, fix.frame, fix.x86)
}

// waitForStagesCompletedError waits until the StagesCompleted condition
// reports an error containing the given message.
func waitForStagesCompletedError(t *testing.T, frame *Frame, migrationName, message string) {
	t.Helper()

	require.Eventually(t, func() bool {
		current := getMigration(t, frame, migrationName)
		cond := meta.FindStatusCondition(current.Status.Conditions, pmcoordbrokerv1alpha1.MigrationConditionStagesCompleted)
		if cond == nil {
			t.Logf("no StagesCompleted condition")
			return false
		}
		if cond.Status == metav1.ConditionTrue {
			t.Logf("StagesCompleted is unexpectedly true")
			return false
		}
		if !strings.Contains(cond.Message, message) {
			t.Logf("StagesCompleted message %q does not contain %q", cond.Message, message)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "StagesCompleted should report %q", message)
}

// TestMigrationUnknownStageErrors verifies that a migration in a stage
// missing from its MigrationConfiguration reports an error.
func TestMigrationUnknownStageErrors(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t,
		configMapStage("copy-data", "dummy", false, `dummy.data.key == "done"`),
	)
	migrationCR := fix.triggerMigration(t)

	stageCM := types.NamespacedName{Namespace: migration.DefaultStageNamespace, Name: migrationCR.Name + "-dummy"}
	require.Eventually(t, func() bool {
		return fix.frame.ComputeClient.Get(t.Context(), stageCM, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "stage configmap should be deployed")

	require.Eventually(t, func() bool {
		current := getMigration(t, fix.frame, migrationCR.Name)
		current.Status.Stage = "bogus"
		if err := fix.frame.CoordinationClient.Status().Update(t.Context(), current); err != nil {
			t.Logf("updating migration status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	waitForStagesCompletedError(t, fix.frame, migrationCR.Name, "not found in MigrationConfiguration")
}

// TestMigrationStageCELCompileError verifies that an invalid success
// condition reports a compile error and blocks the migration.
func TestMigrationStageCELCompileError(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t,
		configMapStage("copy-data", "dummy", false, `this is not CEL(((`),
	)
	migrationCR := fix.triggerMigration(t)

	// The stages only run once the target staging copy exists.
	waitForVM(t, fix.frame.StagingClient(t, fix.arm), fix.nn)

	waitForStagesCompletedError(t, fix.frame, migrationCR.Name, "compiling success condition")

	require.Never(t, func() bool {
		return getMigration(t, fix.frame, migrationCR.Name).Status.State == pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress
	}, 5*time.Second, time.Second, "migration must not reach the cutover with a broken success condition")
}

// TestMigrationStageCELNonBoolean verifies that a success condition not
// evaluating to a boolean reports an error.
func TestMigrationStageCELNonBoolean(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t,
		configMapStage("copy-data", "dummy", false, `dummy.data.key`),
	)
	migrationCR := fix.triggerMigration(t)

	// The stages only run once the target staging copy exists.
	waitForVM(t, fix.frame.StagingClient(t, fix.arm), fix.nn)

	waitForStagesCompletedError(t, fix.frame, migrationCR.Name, "did not evaluate to a boolean")
}

// TestCutoverWaitsForAvailable verifies that the cutover waits until the
// target staging copy reports Available.
func TestCutoverWaitsForAvailable(t *testing.T) {
	t.Parallel()

	fix := newMigrationFixture(t)
	migrationCR := fix.triggerMigration(t)

	// The target VM reports Provisioning: the cutover must wait.
	armStaging := fix.frame.StagingClient(t, fix.arm)
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		if err := armStaging.Get(t.Context(), fix.nn, vm); err != nil {
			t.Logf("getting target staging vm: %v", err)
			return false
		}
		if vm.Spec.Arch != "arm64" {
			t.Logf("target staging vm arch is %q", vm.Spec.Arch)
			return false
		}
		vm.Status.Status = pmbrokerv1alpha1.StatusProvisioning
		if err := armStaging.Status().Update(t.Context(), vm); err != nil {
			t.Logf("updating target staging vm status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	require.Eventually(t, func() bool {
		current := getMigration(t, fix.frame, migrationCR.Name)
		if current.Status.State != pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress {
			t.Logf("migration state is %q", current.Status.State)
			return false
		}
		stages := meta.FindStatusCondition(current.Status.Conditions, pmcoordbrokerv1alpha1.MigrationConditionStagesCompleted)
		if stages == nil || stages.Status != metav1.ConditionTrue {
			t.Logf("StagesCompleted condition: %+v", stages)
			return false
		}
		cutover := meta.FindStatusCondition(current.Status.Conditions, pmcoordbrokerv1alpha1.MigrationConditionCutoverCompleted)
		if cutover == nil || cutover.Status == metav1.ConditionTrue {
			t.Logf("CutoverCompleted condition: %+v", cutover)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "migration should wait in the cutover")

	require.Never(t, func() bool {
		return len(fix.frame.listMigrations(t)) == 0
	}, 5*time.Second, time.Second, "migration must not complete while the target is provisioning")

	markVMAvailable(t, armStaging, fix.nn, "arm64")
	waitForMigrationFinished(t, fix.frame, fix.x86)
}

// TestMigrationStagePreconditions verifies that a stage's templates are
// only deployed once its preconditions are met.
func TestMigrationStagePreconditions(t *testing.T) {
	t.Parallel()

	stage := configMapStage("copy-data", "dummy", false, `dummy.data.key == "done"`)
	stage.Preconditions = []string{`to.status.status == "Available"`}
	fix := newMigrationFixture(t, stage)
	migrationCR := fix.triggerMigration(t)

	armStaging := fix.frame.StagingClient(t, fix.arm)
	waitForVM(t, armStaging, fix.nn)

	stageCM := types.NamespacedName{Namespace: migration.DefaultStageNamespace, Name: migrationCR.Name + "-dummy"}
	require.Never(t, func() bool {
		return fix.frame.ComputeClient.Get(t.Context(), stageCM, &corev1.ConfigMap{}) == nil
	}, 5*time.Second, time.Second, "stage must not be deployed before its preconditions are met")

	markVMAvailable(t, armStaging, fix.nn, "arm64")
	require.Eventually(t, func() bool {
		return fix.frame.ComputeClient.Get(t.Context(), stageCM, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "stage should be deployed once its preconditions are met")

	completeStageConfigMap(t, fix.frame.ComputeClient, stageCM)
	waitForMigrationFinished(t, fix.frame, fix.x86)
}

// TestMigrationCustomStageNamespace verifies that stage templates are
// deployed into the configured stage namespace.
func TestMigrationCustomStageNamespace(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)

	x86 := frame.NewProvider(t, "x86")
	arm := frame.NewProvider(t, "arm64")
	createVMAcceptAPI(t, x86, "accept-x86", "x86_64")
	createVMAcceptAPI(t, arm, "accept-arm64", "arm64")

	config := &pmcoordbrokerv1alpha1.MigrationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrate-vm",
		},
		Spec: pmcoordbrokerv1alpha1.MigrationConfigurationSpec{
			From:   vmGVK,
			To:     vmGVK,
			Stages: []pmcoordbrokerv1alpha1.MigrationStage{configMapStage("copy-data", "dummy", false, `dummy.data.key == "done"`)},
		},
	}
	require.NoError(t, frame.CoordinationClient.Create(t.Context(), config))

	require.NoError(t, frame.ComputeClient.Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "stages"},
	}))

	opts := frame.Options(t)
	opts.StageNamespace = "stages"
	frame.StartBrokerWithOptions(t, opts)

	consumer := frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "x86_64",
			Memory: 512,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), vm))
	nn := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	waitForVM(t, frame.StagingClient(t, x86), nn)
	updateVM(t, consumer.Client, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Arch = "arm64"
	})
	migrationCR := waitForMigration(t, frame)

	stageCM := types.NamespacedName{Namespace: "stages", Name: migrationCR.Name + "-dummy"}
	require.Eventually(t, func() bool {
		return frame.ComputeClient.Get(t.Context(), stageCM, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "stage configmap should be deployed into the custom namespace")

	completeStageConfigMap(t, frame.ComputeClient, stageCM)
	markVMAvailable(t, frame.StagingClient(t, arm), nn, "arm64")
	waitForMigrationFinished(t, frame, x86)
}
