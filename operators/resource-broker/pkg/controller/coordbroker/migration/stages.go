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

package migration

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/sync"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// StagesFinalizer is placed on Migration objects to clean up stage
	// templates deployed to the compute cluster.
	StagesFinalizer = "broker.platform-mesh.io/migration-stages"

	// MigrationStageLabel labels deployed stage templates with the
	// stage name.
	MigrationStageLabel = "broker.platform-mesh.io/migration-stage"
	// MigrationNameLabel labels deployed stage templates with the
	// Migration name.
	MigrationNameLabel = "broker.platform-mesh.io/migration-name"
)

// stagesSubroutine runs the stages of the matching MigrationConfiguration.
type stagesSubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &stagesSubroutine{}
	_ subroutines.Finalizer = &stagesSubroutine{}
)

func (s *stagesSubroutine) GetName() string {
	return pmcoordbrokerv1alpha1.MigrationConditionStagesCompleted
}

func (s *stagesSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{StagesFinalizer}
}

func (s *stagesSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	migration, ok := obj.(*pmcoordbrokerv1alpha1.Migration)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Migration, got %T", obj)
	}

	switch migration.Status.State {
	case pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress, pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted:
		return subroutines.OK(), nil
	case pmcoordbrokerv1alpha1.MigrationStateUnknown:
		migration.Status.State = pmcoordbrokerv1alpha1.MigrationStatePending
	}

	cl, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	nn := types.NamespacedName{Namespace: migration.Spec.Namespace, Name: migration.Spec.Name}

	// Mirror the related resources of both provider-side copies into
	// the compute cluster so stage workloads can migrate data.
	if result, err := s.copyRelatedResources(ctx, migration.Spec.FromStagingWorkspace, migration.Spec.From, nn, "from-"); err != nil || result.Requeue() > 0 {
		return result, err
	}
	if result, err := s.copyRelatedResources(ctx, migration.Spec.StagingWorkspace, migration.Spec.To, nn, "to-"); err != nil || result.Requeue() > 0 {
		return result, err
	}

	config, err := s.migrationConfiguration(ctx, cl, migration)
	if err != nil {
		return subroutines.Result{}, err
	}

	if config == nil || len(config.Spec.Stages) == 0 {
		migration.Status.State = pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress
		return subroutines.OK(), nil
	}

	stageIndex := 0
	if migration.Status.Stage != "" {
		stageIndex = -1
		for i, stage := range config.Spec.Stages {
			if stage.Name == migration.Status.Stage {
				stageIndex = i
				break
			}
		}
		if stageIndex < 0 {
			return subroutines.Result{}, fmt.Errorf("stage %q not found in MigrationConfiguration %q", migration.Status.Stage, config.Name)
		}
	}
	stage := config.Spec.Stages[stageIndex]

	if migration.Status.State == pmcoordbrokerv1alpha1.MigrationStatePending {
		migration.Status.State = pmcoordbrokerv1alpha1.MigrationStateInitialInProgress
	}

	resources, err := s.deployStage(ctx, migration.Name, stage)
	if err != nil {
		return subroutines.Result{}, err
	}

	success, err := checkSuccessConditions(ctx, stage, resources)
	if err != nil {
		return subroutines.Result{}, err
	}
	if !success {
		return subroutines.Pending(s.opts.RequeueInterval, fmt.Sprintf("waiting for stage %q success conditions", stage.Name)), nil
	}

	for _, resource := range resources {
		if err := s.opts.ComputeClient.Delete(ctx, resource); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return subroutines.Result{}, fmt.Errorf("deleting stage resource %q: %w", resource.GetName(), err)
		}
	}

	if stageIndex+1 >= len(config.Spec.Stages) {
		migration.Status.State = pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress
		return subroutines.OK(), nil
	}

	migration.Status.Stage = config.Spec.Stages[stageIndex+1].Name
	if stage.Progress && migration.Status.State == pmcoordbrokerv1alpha1.MigrationStateInitialInProgress {
		migration.Status.State = pmcoordbrokerv1alpha1.MigrationStateInitialCompleted
	}
	return subroutines.Pending(s.opts.RequeueInterval, fmt.Sprintf("stage %q completed", stage.Name)), nil
}

// Finalize deletes the stage templates deployed to the compute cluster.
func (s *stagesSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	migration, ok := obj.(*pmcoordbrokerv1alpha1.Migration)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Migration, got %T", obj)
	}

	cl, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	config, err := s.migrationConfiguration(ctx, cl, migration)
	if err != nil {
		return subroutines.Result{}, err
	}
	if config == nil {
		return subroutines.OK(), nil
	}

	for _, stage := range config.Spec.Stages {
		for name, template := range stage.Templates {
			resource := &unstructured.Unstructured{}
			if err := resource.UnmarshalJSON(template.Raw); err != nil {
				return subroutines.Result{}, fmt.Errorf("unmarshaling template %q of stage %q: %w", name, stage.Name, err)
			}
			resource.SetName(migration.Name + "-" + name)
			resource.SetNamespace(s.opts.StageNamespace)
			if err := s.opts.ComputeClient.Delete(ctx, resource); ctrlruntimeclient.IgnoreNotFound(err) != nil {
				return subroutines.Result{}, fmt.Errorf("deleting stage resource %q: %w", resource.GetName(), err)
			}
		}
	}

	return subroutines.OK(), nil
}

// migrationConfiguration returns the MigrationConfiguration matching the
// migration's From/To GVKs, or nil if none exists.
func (s *stagesSubroutine) migrationConfiguration(ctx context.Context, cl ctrlruntimeclient.Client, migration *pmcoordbrokerv1alpha1.Migration) (*pmcoordbrokerv1alpha1.MigrationConfiguration, error) {
	configs := &pmcoordbrokerv1alpha1.MigrationConfigurationList{}
	if err := cl.List(ctx, configs); err != nil {
		return nil, fmt.Errorf("listing MigrationConfigurations: %w", err)
	}

	for i, config := range configs.Items {
		if config.Spec.From == migration.Spec.From.GVK && config.Spec.To == migration.Spec.To.GVK {
			return &configs.Items[i], nil
		}
	}

	return nil, nil
}

// copyRelatedResources mirrors the related resources of the staging copy
// in the given staging workspace into the compute cluster with prefixed
// names.
func (s *stagesSubroutine) copyRelatedResources(ctx context.Context, wsName string, target pmcoordbrokerv1alpha1.MigrationTarget, nn types.NamespacedName, prefix string) (subroutines.Result, error) {
	if wsName == "" {
		return subroutines.OK(), nil
	}

	stagingClient, err := s.opts.WorkspaceClientFunc(s.opts.StagingTreeRoot + ":" + wsName)
	if err != nil {
		return subroutines.Result{}, fmt.Errorf("building client for staging workspace %q: %w", wsName, err)
	}

	gvk := schema.GroupVersionKind{Group: target.GVK.Group, Version: target.GVK.Version, Kind: target.GVK.Kind}
	related, err := sync.CollectRelatedResources(ctx, stagingClient, gvk, nn)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.Pending(s.opts.RequeueInterval, fmt.Sprintf("waiting for staging copy in workspace %q", wsName)), nil
	case err != nil:
		return subroutines.Result{}, err
	}

	for _, rr := range related {
		if err := s.copyRelatedResource(ctx, stagingClient, rr.SchemaGVK(), types.NamespacedName{Namespace: rr.Namespace, Name: rr.Name}, prefix); err != nil {
			return subroutines.Result{}, err
		}
	}

	return subroutines.OK(), nil
}

// copyRelatedResource copies a single related resource into the compute
// cluster under a prefixed name.
func (s *stagesSubroutine) copyRelatedResource(ctx context.Context, stagingClient ctrlruntimeclient.Client, gvk schema.GroupVersionKind, nn types.NamespacedName, prefix string) error {
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(gvk)
	if err := stagingClient.Get(ctx, nn, source); err != nil {
		return fmt.Errorf("getting related resource %q: %w", nn, err)
	}

	target := sync.StripClusterMetadata(source)
	target.SetName(prefix + nn.Name)
	target.SetNamespace(nn.Namespace)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(gvk)
	err := s.opts.ComputeClient.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(target), existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := s.opts.ComputeClient.Create(ctx, target); err != nil {
			return fmt.Errorf("creating related resource copy %q: %w", target.GetName(), err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("getting related resource copy %q: %w", target.GetName(), err)
	}

	target.SetResourceVersion(existing.GetResourceVersion())
	if err := s.opts.ComputeClient.Update(ctx, target); err != nil {
		return fmt.Errorf("updating related resource copy %q: %w", target.GetName(), err)
	}

	return nil
}

// deployStage deploys the stage templates to the compute cluster and
// returns the deployed resources keyed by template name.
func (s *stagesSubroutine) deployStage(ctx context.Context, migrationName string, stage pmcoordbrokerv1alpha1.MigrationStage) (map[string]*unstructured.Unstructured, error) {
	resources := make(map[string]*unstructured.Unstructured, len(stage.Templates))

	for name, template := range stage.Templates {
		resource := &unstructured.Unstructured{}
		if err := resource.UnmarshalJSON(template.Raw); err != nil {
			return nil, fmt.Errorf("unmarshaling template %q of stage %q: %w", name, stage.Name, err)
		}
		resource.SetName(migrationName + "-" + name)
		resource.SetNamespace(s.opts.StageNamespace)
		resource.SetLabels(map[string]string{
			MigrationStageLabel: stage.Name,
			MigrationNameLabel:  migrationName,
		})

		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(resource.GroupVersionKind())
		err := s.opts.ComputeClient.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(resource), existing)
		switch {
		case apierrors.IsNotFound(err):
			if err := s.opts.ComputeClient.Create(ctx, resource); err != nil {
				return nil, fmt.Errorf("creating stage resource %q: %w", resource.GetName(), err)
			}
			resources[name] = resource
		case err != nil:
			return nil, fmt.Errorf("getting stage resource %q: %w", resource.GetName(), err)
		default:
			resources[name] = existing
		}
	}

	return resources, nil
}

// checkSuccessConditions evaluates the stage's CEL success conditions
// against the deployed resources. All expressions must evaluate to true.
func checkSuccessConditions(ctx context.Context, stage pmcoordbrokerv1alpha1.MigrationStage, resources map[string]*unstructured.Unstructured) (bool, error) {
	envOpts := make([]cel.EnvOption, 0, len(resources))
	evalArgs := make(map[string]any, len(resources))
	for name, resource := range resources {
		envOpts = append(envOpts, cel.Variable(name, cel.DynType))
		evalArgs[name] = resource.Object
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	for _, expression := range stage.SuccessConditions {
		ast, issues := env.Compile(expression)
		if issues != nil && issues.Err() != nil {
			return false, fmt.Errorf("compiling success condition %q: %w", expression, issues.Err())
		}

		program, err := env.Program(ast)
		if err != nil {
			return false, fmt.Errorf("building program for success condition %q: %w", expression, err)
		}

		value, _, err := program.ContextEval(ctx, evalArgs)
		if err != nil {
			return false, fmt.Errorf("evaluating success condition %q: %w", expression, err)
		}

		success, ok := value.Value().(bool)
		if !ok {
			return false, fmt.Errorf("success condition %q did not evaluate to a boolean", expression)
		}
		if !success {
			return false, nil
		}
	}

	return true, nil
}
