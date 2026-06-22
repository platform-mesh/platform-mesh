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

package lifecycle

import (
	"context"
	"fmt"
	"slices"

	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/api/equality"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/api"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/util"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/sentry"
)

func Reconcile(ctx context.Context, nName types.NamespacedName, instance runtimeobject.RuntimeObject, cl client.Client, l api.Lifecycle) (ctrl.Result, error) {
	ctx, span := otel.Tracer(l.Config().OperatorName).Start(ctx, fmt.Sprintf("%s.Reconcile", l.Config().ControllerName))
	defer span.End()

	result := ctrl.Result{}

	log := l.Log().MustChildLoggerWithAttributes("name", nName.Name, "namespace", nName.Namespace, "reconcile_id", string(controller.ReconcileIDFromContext(ctx)))
	cluster, ok := mccontext.ClusterFrom(ctx)
	if ok {
		log = log.MustChildLoggerWithAttributes("cluster", cluster.String())
	}
	sentryTags := sentry.Tags{"namespace": nName.Namespace, "name": nName.Name}

	ctx = logger.SetLoggerInContext(ctx, log)
	ctx = sentry.ContextWithSentryTags(ctx, sentryTags)

	log.Info().Msg("start reconcile")

	err := cl.Get(ctx, nName, instance)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Info().Msg("instance not found. It was likely deleted")
			return ctrl.Result{}, nil
		}
		return HandleClientError("failed to retrieve instance", log, err, true, sentryTags)
	}

	originalCopy := instance.DeepCopyObject()
	inDeletion := instance.GetDeletionTimestamp() != nil
	generationChanged := true

	if l.Spreader() != nil && instance.GetDeletionTimestamp().IsZero() {
		reconcileRequired := l.Spreader().ReconcileRequired(instance, log)
		if !reconcileRequired {
			log.Info().Msg("skipping reconciliation, spread reconcile is active. No processing needed")
			return l.Spreader().OnNextReconcile(instance, log)
		}
	}

	// Manage Finalizers
	ferr := AddFinalizersIfNeeded(ctx, cl, instance, l.Subroutines(), l.Config().ReadOnly)
	if ferr != nil {
		return ctrl.Result{}, ferr
	}

	var condArr []v1.Condition
	if l.ConditionsManager() != nil {
		condArr = util.MustToInterface[api.RuntimeObjectConditions](instance, log).GetConditions()
		l.ConditionsManager().SetInstanceConditionUnknownIfNotSet(&condArr, instance.GetGeneration())
	}

	if l.PrepareContextFunc() != nil {
		localCtx, oErr := l.PrepareContextFunc()(ctx, instance)
		if oErr != nil {
			return HandleOperatorError(ctx, oErr, "failed to prepare context", generationChanged, l.Log())
		}
		ctx = localCtx
	}

	// In case of deletion execute the finalize subroutines in the reverse order as subroutine processing
	subroutines := make([]subroutine.Subroutine, len(l.Subroutines()))
	copy(subroutines, l.Subroutines())
	if inDeletion {
		slices.Reverse(subroutines)
	}

	// Continue with reconciliation
	for _, s := range subroutines {
		if l.ConditionsManager() != nil {
			l.ConditionsManager().SetSubroutineConditionToUnknownIfNotSet(&condArr, instance.GetGeneration(), s, inDeletion, log)
		}

		// Set current condArr before reconciling the s
		if l.ConditionsManager() != nil {
			util.MustToInterface[api.RuntimeObjectConditions](instance, log).SetConditions(condArr)
		}
		subResult, retry, err := reconcileSubroutine(ctx, instance, s, cl, l, log, generationChanged, sentryTags)
		// Update condArr with any changes the s did
		if l.ConditionsManager() != nil {
			condArr = util.MustToInterface[api.RuntimeObjectConditions](instance, log).GetConditions()
		}
		if err != nil {
			if l.ConditionsManager() != nil {
				l.ConditionsManager().SetSubroutineCondition(&condArr, instance.GetGeneration(), s, result, err, inDeletion, log)
				l.ConditionsManager().SetInstanceConditionReady(&condArr, instance.GetGeneration(), v1.ConditionFalse)
				util.MustToInterface[api.RuntimeObjectConditions](instance, log).SetConditions(condArr)
			}
			if !retry {
				MarkResourceAsFinal(instance, log, condArr, v1.ConditionFalse, l)
			}
			if !l.Config().ReadOnly {
				_ = updateStatus(ctx, cl, originalCopy, instance, log, generationChanged, sentryTags)
			}
			if !retry {
				return ctrl.Result{}, nil
			}
			return subResult, err
		}
		if subResult.RequeueAfter > 0 {
			if subResult.RequeueAfter < result.RequeueAfter || result.RequeueAfter == 0 {
				result.RequeueAfter = subResult.RequeueAfter
			}
		}
		if l.ConditionsManager() != nil {
			if subResult.RequeueAfter == 0 {
				l.ConditionsManager().SetSubroutineCondition(&condArr, instance.GetGeneration(), s, subResult, err, inDeletion, log)
			}
		}
	}

	if result.RequeueAfter == 0 {
		// Reconciliation was successful
		MarkResourceAsFinal(instance, log, condArr, v1.ConditionTrue, l)
	} else {
		if l.ConditionsManager() != nil {
			l.ConditionsManager().SetInstanceConditionReady(&condArr, instance.GetGeneration(), v1.ConditionFalse)
		}
	}

	if t, ok := l.(api.TerminatingLifecycle); ok && result.RequeueAfter == 0 && inDeletion && t.Terminator() != "" {
		log.Debug().Msgf("Removing terminator")
		if err := removeTerminator(ctx, instance, cl, t.Terminator()); err != nil {
			return result, fmt.Errorf("potentially removing Terminator: %w", err)
		}
	}

	if i, ok := l.(api.InitializingLifecycle); ok && result.RequeueAfter == 0 && !inDeletion && i.Initializer() != "" {
		log.Debug().Msgf("Removing initializer")
		if err := removeInitializer(ctx, instance, cl, i.Initializer()); err != nil {
			return result, fmt.Errorf("potentially removing Initializer: %w", err)
		}
	}

	if l.ConditionsManager() != nil {
		util.MustToInterface[api.RuntimeObjectConditions](instance, log).SetConditions(condArr)
	}

	if !l.Config().ReadOnly {
		// Skip status update if all finalizers are removed (object will be deleted)
		if instance.GetDeletionTimestamp() != nil && len(instance.GetFinalizers()) == 0 {
			log.Info().Msg("skipping status update - all finalizers removed, object will be deleted")
		} else {
			err = updateStatus(ctx, cl, originalCopy, instance, log, generationChanged, sentryTags)
			if err != nil {
				return result, err
			}
		}
	}

	if l.Spreader() != nil && instance.GetDeletionTimestamp().IsZero() {
		original := instance.DeepCopyObject().(client.Object)
		removed := l.Spreader().RemoveRefreshLabelIfExists(instance)
		if removed {
			updateErr := cl.Patch(ctx, instance, client.MergeFrom(original))
			if updateErr != nil {
				return HandleClientError("failed to update instance", log, err, generationChanged, sentryTags)
			}
		}
	}

	log.Info().Msg("end reconcile")
	return result, nil
}

func reconcileSubroutine(ctx context.Context, instance runtimeobject.RuntimeObject, s subroutine.Subroutine, cl client.Client, l api.Lifecycle, log *logger.Logger, generationChanged bool, sentryTags map[string]string) (ctrl.Result, bool, error) {
	subroutineLogger := log.ChildLogger("subroutine", s.GetName())
	ctx = logger.SetLoggerInContext(ctx, subroutineLogger)
	subroutineLogger.Debug().Msg("start subroutine")

	ctx, span := otel.Tracer(l.Config().OperatorName).Start(ctx, fmt.Sprintf("%s.reconcileSubroutine.%s", l.Config().ControllerName, s.GetName()))
	defer span.End()
	var result ctrl.Result
	var err errors.OperatorError
	if terminator, ok := s.(subroutine.Terminator); ok && instance.GetDeletionTimestamp() != nil {
		subroutineLogger.Debug().Msg("terminating instance")
		result, err = terminator.Terminate(ctx, instance)
		subroutineLogger.Debug().Any("result", result).Bool("err_is_nil", err == nil).Msg("terminated instance")
		if err != nil {
			if err.Sentry() {
				sentry.CaptureError(err.Err(), sentryTags)
			}
			subroutineLogger.Error().Err(err.Err()).Bool("retry", err.Retry()).Msg("terminator ended with error")
		}
	} else if instance.GetDeletionTimestamp() != nil && containsFinalizer(instance, s.Finalizers(instance)) {
		subroutineLogger.Debug().Msg("finalizing instance")
		result, err = s.Finalize(ctx, instance)
		subroutineLogger.Debug().Any("result", result).Msg("finalized instance")
		if err == nil {
			// Remove finalizers unless requeue is requested
			err = removeFinalizerIfNeeded(ctx, instance, s, result, l.Config().ReadOnly, cl)
		}
	} else if initializer, ok := s.(subroutine.Initializer); ok && instance.GetDeletionTimestamp() == nil {
		subroutineLogger.Debug().Msg("initializing instance")
		result, err = initializer.Initialize(ctx, instance)
		subroutineLogger.Debug().Any("result", result).Bool("err_is_nil", err == nil).Msg("initialized instance")
		if err != nil {
			if err.Sentry() {
				sentry.CaptureError(err.Err(), sentryTags)
			}
			subroutineLogger.Error().Err(err.Err()).Bool("retry", err.Retry()).Msg("initializer ended with error")
		}
	} else if instance.GetDeletionTimestamp() == nil {
		subroutineLogger.Debug().Msg("processing instance")
		result, err = s.Process(ctx, instance)
		subroutineLogger.Debug().Any("result", result).Msg("processed instance")
	}

	if err != nil {
		if generationChanged && err.Sentry() {
			sentry.CaptureError(err.Err(), sentryTags)
		}
		subroutineLogger.Error().Err(err.Err()).Bool("retry", err.Retry()).Msg("subroutine ended with error")
		return result, err.Retry(), err.Err()
	}

	subroutineLogger.Debug().Msg("end subroutine")
	return result, false, nil
}

func containsFinalizer(o client.Object, subroutineFinalizers []string) bool {
	for _, subroutineFinalizer := range subroutineFinalizers {
		if controllerutil.ContainsFinalizer(o, subroutineFinalizer) {
			return true
		}
	}
	return false
}

func removeFinalizerIfNeeded(ctx context.Context, instance runtimeobject.RuntimeObject, s subroutine.Subroutine, result ctrl.Result, readonly bool, cl client.Client) errors.OperatorError {
	if readonly {
		return nil
	}

	if result.RequeueAfter == 0 {
		update := false
		original := instance.DeepCopyObject().(client.Object)
		for _, f := range s.Finalizers(instance) {
			needsUpdate := controllerutil.RemoveFinalizer(instance, f)
			if needsUpdate {
				update = true
			}
		}
		if update {
			err := cl.Patch(ctx, instance, client.MergeFrom(original))
			if err != nil {
				return errors.NewOperatorError(errors.Wrap(err, "failed to update instance"), true, false)
			}
		}
	}

	return nil
}

func removeTerminator(ctx context.Context, instance runtimeobject.RuntimeObject, cl client.Client, terminator string) error {
	if terminator == "" {
		return nil
	}

	original := instance.DeepCopyObject().(client.Object)

	currentUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(instance)
	if err != nil {
		return fmt.Errorf("failed to convert instance to unstructured: %w", err)
	}

	terminators, ok, err := unstructured.NestedStringSlice(currentUn, "status", "terminators")
	if err != nil || !ok || len(terminators) == 0 {
		return nil
	}

	newTerminators := slices.DeleteFunc(terminators, func(t string) bool {
		return t == terminator
	})
	if len(newTerminators) == len(terminators) {
		return nil
	}

	if err := unstructured.SetNestedStringSlice(currentUn, newTerminators, "status", "terminators"); err != nil {
		return fmt.Errorf("failed to set terminators: %w", err)
	}

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(currentUn, instance); err != nil {
		return fmt.Errorf("failed to convert unstructured to instance: %w", err)
	}

	if err := cl.Status().Patch(ctx, instance.(client.Object), client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to patch instance status: %w", err)
	}

	return nil
}

func removeInitializer(ctx context.Context, instance runtimeobject.RuntimeObject, cl client.Client, initializer string) error {
	if initializer == "" {
		return nil
	}

	original := instance.DeepCopyObject().(client.Object)

	currentUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(instance)
	if err != nil {
		return fmt.Errorf("failed to convert instance to unstructured: %w", err)
	}

	initializers, ok, err := unstructured.NestedStringSlice(currentUn, "status", "initializers")
	if err != nil || !ok || len(initializers) == 0 {
		return nil
	}

	newInitializers := slices.DeleteFunc(initializers, func(i string) bool {
		return i == initializer
	})
	if len(newInitializers) == len(initializers) {
		return nil
	}

	if err := unstructured.SetNestedStringSlice(currentUn, newInitializers, "status", "initializers"); err != nil {
		return fmt.Errorf("failed to set initializers: %w", err)
	}

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(currentUn, instance); err != nil {
		return fmt.Errorf("failed to convert unstructured to instance: %w", err)
	}

	if err := cl.Status().Patch(ctx, instance.(client.Object), client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to patch instance status: %w", err)
	}

	return nil
}

func updateStatus(ctx context.Context, cl client.Client, original runtime.Object, current runtimeobject.RuntimeObject, log *logger.Logger, generationChanged bool, sentryTags sentry.Tags) error {
	currentUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(current)
	if err != nil {
		return err
	}

	originalUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(original)
	if err != nil {
		return err
	}

	currentStatus, _, err := unstructured.NestedFieldCopy(currentUn, "status")
	if err != nil {
		return err
	}

	originalStatus, _, err := unstructured.NestedFieldCopy(originalUn, "status")
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(currentStatus, originalStatus) {
		log.Info().Msg("skipping status update, since they are equal")
		return nil
	}

	log.Info().Msg("updating resource status")
	err = cl.Status().Update(ctx, current)
	if err != nil {
		if kerrors.IsConflict(err) {
			log.Warn().Err(err).Msg("cannot update reconciliation Conditions, kubernetes client error")
		} else {
			log.Error().Err(err).Msg("cannot update status, kubernetes client error")
			if generationChanged {
				sentry.CaptureError(err, sentryTags, sentry.Extras{"message": "Updating of instance status failed"})
			}
		}
		return err
	}

	return nil
}

func HandleClientError(msg string, log *logger.Logger, err error, generationChanged bool, sentryTags sentry.Tags) (ctrl.Result, error) {
	log.Error().Err(err).Msg(msg)
	if generationChanged {
		sentry.CaptureError(err, sentryTags)
	}

	return ctrl.Result{}, err
}

func MarkResourceAsFinal(instance runtimeobject.RuntimeObject, log *logger.Logger, conditions []v1.Condition, status v1.ConditionStatus, l api.Lifecycle) {
	if l.Spreader() != nil && instance.GetDeletionTimestamp().IsZero() {
		instanceStatusObj := util.MustToInterface[api.RuntimeObjectSpreadReconcileStatus](instance, log)
		l.Spreader().SetNextReconcileTime(instanceStatusObj, log)
		l.Spreader().UpdateObservedGeneration(instanceStatusObj, log)
	}

	if l.ConditionsManager() != nil {
		l.ConditionsManager().SetInstanceConditionReady(&conditions, instance.GetGeneration(), status)
	}
}

func AddFinalizersIfNeeded(ctx context.Context, cl client.Client, instance runtimeobject.RuntimeObject, subroutines []subroutine.Subroutine, readonly bool) error {
	if readonly {
		return nil
	}

	if !instance.GetDeletionTimestamp().IsZero() {
		return nil
	}

	update := false
	original := instance.DeepCopyObject().(client.Object)
	for _, s := range subroutines {
		if len(s.Finalizers(instance)) > 0 {
			needsUpdate := AddFinalizerIfNeeded(instance, s)
			if needsUpdate {
				update = true
			}
		}
	}
	if update {
		err := cl.Patch(ctx, instance, client.MergeFrom(original))
		if err != nil {
			return err
		}
	}
	return nil
}

func AddFinalizerIfNeeded(instance runtimeobject.RuntimeObject, s subroutine.Subroutine) bool {
	update := false
	for _, f := range s.Finalizers(instance) {
		needsUpdate := controllerutil.AddFinalizer(instance, f)
		if needsUpdate {
			update = true
		}
	}
	return update
}

func HandleOperatorError(ctx context.Context, operatorError errors.OperatorError, msg string, generationChanged bool, log *logger.Logger) (ctrl.Result, error) {
	log.Error().Bool("retry", operatorError.Retry()).Bool("sentry", operatorError.Sentry()).Err(operatorError.Err()).Msg(msg)
	if generationChanged && operatorError.Sentry() {
		sentry.CaptureError(operatorError.Err(), sentry.GetSentryTagsFromContext(ctx))
	}

	if operatorError.Retry() {
		return ctrl.Result{}, operatorError.Err()
	}

	return ctrl.Result{}, nil
}

func ValidateInterfaces(instance runtimeobject.RuntimeObject, log *logger.Logger, l api.Lifecycle) error {
	if l.Spreader() != nil {
		_, err := util.ToInterface[api.RuntimeObjectSpreadReconcileStatus](instance, log)
		if err != nil {
			return err
		}
	}
	if l.ConditionsManager() != nil {
		_, err := util.ToInterface[api.RuntimeObjectConditions](instance, log)
		if err != nil {
			return err
		}
	}
	return nil
}
