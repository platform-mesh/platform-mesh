package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	subroutinemetrics "go.platform-mesh.io/subroutines/metrics"
	"go.platform-mesh.io/subroutines/spread"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
)

var tracer = otel.Tracer("go.platform-mesh.io/subroutines/lifecycle")

const (
	// statusFieldInitializers is the status field name for initializer markers.
	statusFieldInitializers = "initializers"
	// statusFieldTerminators is the status field name for terminator markers.
	statusFieldTerminators = "terminators"
)

// Lifecycle orchestrates subroutine execution for a Kubernetes controller.
type Lifecycle struct {
	mgr            mcmanager.Manager
	newObj         func() client.Object
	controllerName string
	subroutines    []subroutines.Subroutine

	conditions     ConditionManager
	spread         SpreadManager
	errorReporters []ErrorReporter
	prepareCtx     func(ctx context.Context, obj client.Object) (context.Context, error)
	readOnly       bool
	specPatch      bool
	initializer    string
	terminator     string
}

// New creates a Lifecycle for the given controller.
func New(mgr mcmanager.Manager, controllerName string, newObj func() client.Object, subs ...subroutines.Subroutine) *Lifecycle {
	return &Lifecycle{
		mgr:            mgr,
		controllerName: controllerName,
		newObj:         newObj,
		subroutines:    subs,
	}
}

// WithConditions enables condition management.
// Panics if the object produced by newObj does not implement conditions.ConditionAccessor.
func (l *Lifecycle) WithConditions(cm ConditionManager) *Lifecycle {
	l.mustImplement("conditions.ConditionAccessor", func(obj client.Object) bool {
		_, ok := obj.(conditions.ConditionAccessor)
		return ok
	})
	l.conditions = cm
	return l
}

// WithSpread enables reconciliation spreading.
// Panics if the object produced by newObj does not implement spread.SpreadReconcileStatus.
func (l *Lifecycle) WithSpread(sm SpreadManager) *Lifecycle {
	l.mustImplement("spread.SpreadReconcileStatus", func(obj client.Object) bool {
		_, ok := obj.(spread.SpreadReconcileStatus)
		return ok
	})
	l.spread = sm
	return l
}

// WithErrorReporters adds one or more error reporters.
func (l *Lifecycle) WithErrorReporters(reporters ...ErrorReporter) *Lifecycle {
	l.errorReporters = append(l.errorReporters, reporters...)
	return l
}

// WithPrepareContext sets a function to enrich the context before subroutines run.
func (l *Lifecycle) WithPrepareContext(fn func(ctx context.Context, obj client.Object) (context.Context, error)) *Lifecycle {
	l.prepareCtx = fn
	return l
}

// WithReadOnly disables all patches.
func (l *Lifecycle) WithReadOnly() *Lifecycle {
	l.readOnly = true
	return l
}

// WithSpecPatch enables spec patching when spec changes are detected.
func (l *Lifecycle) WithSpecPatch() *Lifecycle {
	l.specPatch = true
	return l
}

// WithInitializer enables initializer support. When the given name is present in
// status.initializers (set by kcp), subroutines implementing Initializer will run
// Initialize instead of Process. The marker is removed from status once all
// subroutines complete successfully.
func (l *Lifecycle) WithInitializer(name string) *Lifecycle {
	l.initializer = name
	return l
}

// WithTerminator enables terminator support. When the given name is present in
// status.terminators (set by kcp), subroutines implementing Terminator will run
// Terminate instead of Finalize during deletion. The marker is removed from status
// once all subroutines complete successfully.
func (l *Lifecycle) WithTerminator(name string) *Lifecycle {
	l.terminator = name
	return l
}

// Reconcile implements mcreconcile.Reconciler.
func (l *Lifecycle) Reconcile(ctx context.Context, req mcreconcile.Request) (reconcile.Result, error) {
	ctx = mccontext.WithCluster(ctx, req.ClusterName)
	ctx, span := tracer.Start(ctx, fmt.Sprintf("%s/reconcile", l.controllerName))
	defer span.End()

	c, err := l.mgr.ClusterFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("resolving cluster client: %w", err)
	}
	cl := c.GetClient()
	ctx = subroutines.WithClient(ctx, cl)

	obj := l.newObj()
	if err := cl.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("fetching object: %w", err)
	}

	original := obj.DeepCopyObject().(client.Object)
	isDeleting := obj.GetDeletionTimestamp() != nil
	generation := obj.GetGeneration()

	logger := log.FromContext(ctx).WithValues(
		"controller", l.controllerName,
		"cluster", req.ClusterName,
		"name", obj.GetName(),
		"namespace", obj.GetNamespace(),
		"generation", generation,
	)
	ctx = log.IntoContext(ctx, logger)

	span.SetAttributes(
		attribute.String("controller", l.controllerName),
		attribute.String("cluster", req.ClusterName.String()),
		attribute.String("name", obj.GetName()),
		attribute.String("namespace", obj.GetNamespace()),
		attribute.Int64("generation", generation),
		attribute.Bool("deleting", isDeleting),
	)

	// Spread check.
	if l.spread != nil && !isDeleting {
		if !l.spread.ReconcileRequired(obj) {
			logger.V(1).Info("skipping reconciliation, not yet due")
			return reconcile.Result{RequeueAfter: l.spread.RequeueDelay(obj)}, nil
		}
	}

	// Add finalizers — if any were added, stop and let the watch event trigger the next reconciliation.
	if !isDeleting && !l.readOnly {
		added, err := l.addFinalizers(ctx, cl, obj)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("adding finalizers: %w", err)
		}
		if added {
			logger.V(1).Info("finalizers added, waiting for next reconciliation")
			return reconcile.Result{}, nil
		}
	}

	// Prepare context.
	if l.prepareCtx != nil {
		ctx, err = l.prepareCtx(ctx, obj)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("preparing context: %w", err)
		}
	}

	// Init conditions.
	subNames := make([]string, 0, len(l.subroutines))
	for _, sub := range l.subroutines {
		subNames = append(subNames, sub.GetName())
	}
	if l.conditions != nil {
		l.conditions.InitUnknownConditions(obj, subNames)
	}

	// Build execution order.
	subs := slices.Clone(l.subroutines)
	if isDeleting {
		slices.Reverse(subs)
	}

	// Execute subroutines.
	var (
		subroutineErr error
		hasPending    bool
		hasStopped    bool
		hasSkipAll    bool
		skipAllReady  bool
		minRequeue    time.Duration
	)

	for i, sub := range subs {
		action, actionName := l.resolveAction(ctx, sub, obj, isDeleting)
		if action == nil {
			continue
		}

		subCtx, subSpan := tracer.Start(ctx, fmt.Sprintf("%s/%s/%s", l.controllerName, sub.GetName(), actionName),
			trace.WithAttributes(attribute.String("subroutine", sub.GetName()), attribute.String("action", actionName.String())),
		)

		start := time.Now()
		result, err := action(subCtx, obj)
		duration := time.Since(start)
		subSpan.End()

		subroutinemetrics.Record(l.controllerName, sub.GetName(), actionName.String(), result, err, duration)

		if err != nil {
			logger.Error(err, "subroutine failed", "subroutine", sub.GetName(), "action", actionName)
			if l.conditions != nil {
				l.conditions.SetSubroutineCondition(obj, sub.GetName(), result, err, actionName.IsFinalize())
			}
			for _, reporter := range l.errorReporters {
				reporter.Report(ctx, err, ErrorInfo{
					Subroutine: sub.GetName(),
					Object:     obj,
					Action:     actionName,
				})
			}
			subroutineErr = err
			break
		}

		if l.conditions != nil {
			l.conditions.SetSubroutineCondition(obj, sub.GetName(), result, nil, actionName.IsFinalize())
		}

		// Track min requeue across all subroutines — the shortest duration wins
		// regardless of result type (OK, Pending, or StopWithRequeue).
		if d := result.Requeue(); d > 0 {
			if minRequeue == 0 || d < minRequeue {
				minRequeue = d
			}
		}

		// Remove finalizers after successful finalize with no requeue.
		if actionName == ActionFinalize && result.IsContinue() && result.Requeue() == 0 {
			if finalizer, ok := sub.(subroutines.Finalizer); ok {
				for _, f := range finalizer.Finalizers(obj) {
					controllerutil.RemoveFinalizer(obj, f)
				}
			}
		}

		if result.IsPending() {
			hasPending = true
		}

		if result.IsStopWithRequeue() || result.IsStop() {
			hasStopped = true
			logger.Info("subroutine stopped the chain", "subroutine", sub.GetName(), "action", actionName, "message", result.Message())
			break
		}

		if result.IsSkipAll() {
			hasSkipAll = true
			skipAllReady = result.Ready()
			logger.Info("subroutine skipped remaining chain", "subroutine", sub.GetName(), "action", actionName, "message", result.Message(), "ready", result.Ready())
			if l.conditions != nil {
				var remaining []string
				for _, r := range subs[i+1:] {
					remaining = append(remaining, r.GetName())
				}
				if len(remaining) > 0 {
					l.conditions.SetSkippedConditions(obj, remaining, skipAllReady, result.Message())
				}
			}
			break
		}
	}

	// Remove initializer/terminator from status if all succeeded with no requeue.
	if subroutineErr == nil && !hasStopped && !hasSkipAll && !hasPending && minRequeue == 0 {
		if l.initializer != "" && !isDeleting {
			if removeMarkerFromStatus(ctx, obj, statusFieldInitializers, l.initializer) {
				logger.V(1).Info("removed initializer from status", "initializer", l.initializer)
			}
		}
		if l.terminator != "" && isDeleting {
			if removeMarkerFromStatus(ctx, obj, statusFieldTerminators, l.terminator) {
				logger.V(1).Info("removed terminator from status", "terminator", l.terminator)
			}
		}
	}

	// Set Ready condition.
	if l.conditions != nil {
		var readyReason string
		switch {
		case subroutineErr != nil:
			readyReason = conditions.ReasonError
		case hasStopped:
			readyReason = conditions.ReasonStopped
		case hasSkipAll && !skipAllReady:
			readyReason = conditions.ReasonStopped
		case hasPending:
			readyReason = conditions.ReasonPending
		default:
			readyReason = conditions.ReasonComplete
		}
		l.conditions.SetReadyCondition(obj, readyReason)
	}

	// Update spread state — only when the full chain completed without stop or pending requeue.
	if l.spread != nil && !isDeleting && subroutineErr == nil && !hasStopped && minRequeue == 0 {
		l.spread.UpdateObservedGeneration(obj)
		l.spread.SetNextReconcileTime(obj)
		if l.spread.RemoveRefreshLabel(obj) {
			logger.V(1).Info("removed refresh label")
		}
	}

	// Patch changes.
	if !l.readOnly {
		patchErr := l.patchChanges(ctx, cl, original, obj)
		if patchErr != nil {
			if apierrors.IsConflict(patchErr) {
				logger.V(1).Info("conflict during patch, requeueing")
				return reconcile.Result{RequeueAfter: time.Second}, nil
			}
			return reconcile.Result{}, errors.Join(subroutineErr, patchErr)
		}
	}

	return reconcile.Result{RequeueAfter: minRequeue}, subroutineErr
}

type actionFunc func(ctx context.Context, obj client.Object) (subroutines.Result, error)

func (l *Lifecycle) resolveAction(ctx context.Context, sub subroutines.Subroutine, obj client.Object, isDeleting bool) (actionFunc, Action) {
	if isDeleting {
		// Terminator takes precedence when configured and marker is in status.
		if l.terminator != "" && hasMarkerInStatus(ctx, obj, statusFieldTerminators, l.terminator) {
			if t, ok := sub.(subroutines.Terminator); ok {
				return t.Terminate, ActionTerminate
			}
		}
		// Finalizer.
		if f, ok := sub.(subroutines.Finalizer); ok {
			if hasAnyFinalizer(obj, f.Finalizers(obj)) {
				return f.Finalize, ActionFinalize
			}
		}
		return nil, ""
	}

	// Initializer takes precedence when configured and marker is in status.
	if l.initializer != "" && hasMarkerInStatus(ctx, obj, statusFieldInitializers, l.initializer) {
		if i, ok := sub.(subroutines.Initializer); ok {
			return i.Initialize, ActionInitialize
		}
	}
	// Processor.
	if p, ok := sub.(subroutines.Processor); ok {
		return p.Process, ActionProcess
	}
	return nil, ""
}

func (l *Lifecycle) addFinalizers(ctx context.Context, cl client.Client, obj client.Object) (bool, error) {
	var missing []string
	current := obj.GetFinalizers()
	for _, sub := range l.subroutines {
		f, ok := sub.(subroutines.Finalizer)
		if !ok {
			continue
		}
		for _, fin := range f.Finalizers(obj) {
			if !slices.Contains(current, fin) {
				missing = append(missing, fin)
			}
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	obj.SetFinalizers(append(current, missing...))
	return true, cl.Patch(ctx, obj, patch)
}

func (l *Lifecycle) patchChanges(ctx context.Context, cl client.Client, original, current client.Object) error {
	logger := log.FromContext(ctx)

	origData, err := toUnstructuredMap(original)
	if err != nil {
		return fmt.Errorf("converting original to unstructured: %w", err)
	}
	currData, err := toUnstructuredMap(current)
	if err != nil {
		return fmt.Errorf("converting current to unstructured: %w", err)
	}

	// Single object patch for metadata + spec changes.
	needsObjectPatch := !equality.Semantic.DeepEqual(getMap(origData, "metadata"), getMap(currData, "metadata"))
	if l.specPatch {
		needsObjectPatch = needsObjectPatch || !equality.Semantic.DeepEqual(getMap(origData, "spec"), getMap(currData, "spec"))
	}
	if needsObjectPatch {
		logger.V(1).Info("patching object")
		patch := client.MergeFrom(original)
		if err := cl.Patch(ctx, current, patch); err != nil {
			return fmt.Errorf("patching object: %w", err)
		}
		// Update resourceVersion on original so the status MergeFrom patch
		// does not conflict, and recompute unstructured maps so the status
		// diff comparison accounts for server-side mutations (webhooks, defaults).
		original.SetResourceVersion(current.GetResourceVersion())
		origData, err = toUnstructuredMap(original)
		if err != nil {
			return fmt.Errorf("converting original to unstructured after patch: %w", err)
		}
		currData, err = toUnstructuredMap(current)
		if err != nil {
			return fmt.Errorf("converting current to unstructured after patch: %w", err)
		}
	}

	// Status patch — skip when the object will be garbage-collected imminently
	// (deletion timestamp set and no finalizers remaining). A status update would
	// race against deletion and serve no purpose.
	if current.GetDeletionTimestamp() != nil && len(current.GetFinalizers()) == 0 {
		return nil
	}
	if !equality.Semantic.DeepEqual(getMap(origData, "status"), getMap(currData, "status")) {
		logger.V(1).Info("patching status")
		patch := client.MergeFrom(original)
		if err := cl.Status().Patch(ctx, current, patch); err != nil {
			return fmt.Errorf("patching status: %w", err)
		}
	}

	return nil
}

func toUnstructuredMap(obj client.Object) (map[string]any, error) {
	return runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
}

func getMap(data map[string]any, key string) map[string]any {
	if v, ok := data[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func (l *Lifecycle) mustImplement(iface string, check func(client.Object) bool) {
	obj := l.newObj()
	if !check(obj) {
		panic(fmt.Sprintf("lifecycle %q: object type %T does not implement %s", l.controllerName, obj, iface))
	}
}

func hasAnyFinalizer(obj client.Object, fins []string) bool {
	current := obj.GetFinalizers()
	for _, f := range fins {
		if slices.Contains(current, f) {
			return true
		}
	}
	return false
}

// hasMarkerInStatus checks if a named marker exists in obj.status[field].
// Uses unstructured conversion to avoid type assertions.
func hasMarkerInStatus(ctx context.Context, obj client.Object, field, name string) bool {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to convert object to unstructured for marker check", "field", field, "name", name)
		return false
	}
	status, ok := data["status"].(map[string]any)
	if !ok {
		return false
	}
	markers, ok := status[field]
	if !ok {
		return false
	}
	// Markers can be a string or a slice of strings.
	switch v := markers.(type) {
	case string:
		return v == name
	case []any:
		for _, m := range v {
			if s, ok := m.(string); ok && s == name {
				return true
			}
		}
	}
	return false
}

// removeMarkerFromStatus removes a named marker from obj.status[field] using
// unstructured conversion. Returns true if the marker was found and removed.
func removeMarkerFromStatus(ctx context.Context, obj client.Object, field, name string) bool {
	logger := log.FromContext(ctx)
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		logger.Error(err, "failed to convert object to unstructured for marker removal", "field", field, "name", name)
		return false
	}
	status, ok := data["status"].(map[string]any)
	if !ok {
		return false
	}
	markers, ok := status[field]
	if !ok {
		return false
	}

	modified := false
	switch v := markers.(type) {
	case string:
		if v == name {
			delete(status, field)
			modified = true
		}
	case []any:
		var filtered []any
		for _, m := range v {
			if s, ok := m.(string); ok && s == name {
				modified = true
				continue
			}
			filtered = append(filtered, m)
		}
		if modified {
			if len(filtered) == 0 {
				delete(status, field)
			} else {
				status[field] = filtered
			}
		}
	}

	if !modified {
		return false
	}

	data["status"] = status
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(data, obj); err != nil {
		logger.Error(err, "failed to convert unstructured back to object after marker removal", "field", field, "name", name)
		return false
	}
	return true
}
