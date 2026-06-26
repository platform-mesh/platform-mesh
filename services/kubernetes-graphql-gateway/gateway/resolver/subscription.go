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

package resolver

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/clock"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrFailedToCastEventObjectToUnstructured = fmt.Errorf("failed to cast event object to unstructured")
)

// Event type constants used in subscription envelopes
const (
	EventTypeAdded    = "ADDED"
	EventTypeModified = "MODIFIED"
	EventTypeDeleted  = "DELETED"
)

const (
	watchReconnectInitialDelay = 800 * time.Millisecond
	watchReconnectMaxDelay     = 30 * time.Second
	watchReconnectFactor       = 2.0
	watchReconnectJitter       = 1.0
	watchReconnectSteps        = 10
	watchReconnectResetAfter   = 2 * time.Minute
)

// SubscriptionEnvelope represents the envelope for a subscription update
type SubscriptionEnvelope struct {
	Type   string `json:"type"`
	Object any    `json:"object"`
}

// SubscriptionObject represents an object with only minimal metadata
type SubscriptionObject struct {
	Metadata SubscriptionMetadata `json:"metadata"`
}

// SubscriptionMetadata represents minimal metadata for an object
type SubscriptionMetadata struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

func (r *Service) SubscribeItem(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		resultChannel := make(chan any)
		go r.runWatch(p, gvk, resultChannel, true, scope)
		return resultChannel, nil
	}
}

func (r *Service) SubscribeItems(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		resultChannel := make(chan any)
		go r.runWatch(p, gvk, resultChannel, false, scope)
		return resultChannel, nil
	}
}

func (r *Service) runWatch(
	p graphql.ResolveParams,
	gvk schema.GroupVersionKind,
	resultChannel chan any,
	singleItem bool,
	scope apiextensionsv1.ResourceScope,
) {
	defer close(resultChannel)

	ctx, span := otel.Tracer("").Start(p.Context, "runWatch", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
	defer span.End()

	logger := log.FromContext(ctx).WithValues(
		"operation", "watch",
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
	)

	sendErr := func(err error) {
		select {
		case <-ctx.Done():
		case resultChannel <- err:
		}
	}

	labelSelector, err := GetArg[string](p.Args, LabelSelectorArg, false)
	if err != nil {
		logger.Error(err, "Failed to get label selector argument")
		sendErr(fmt.Errorf("failed to get label selector argument: %w", err))
		return
	}

	subscribeToAll, err := GetArg[bool](p.Args, SubscribeToAllArg, false)
	if err != nil {
		logger.Error(err, "Failed to get subscribeToAll argument")
		sendErr(fmt.Errorf("failed to get subscribeToAll argument: %w", err))
		return
	}

	// optional resourceVersion to continue subscription from
	resourceVersion, err := GetArg[string](p.Args, ResourceVersionArg, false)
	if err != nil {
		logger.Error(err, "Failed to get resourceVersion argument")
		sendErr(fmt.Errorf("failed to get resourceVersion argument: %w", err))
		return
	}

	fieldsToWatch := extractRequestedFields(p.Info)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
	})

	var opts []ctrlruntimeclient.ListOption

	var namespace string
	if isResourceNamespaceScoped(scope) {
		isNamespaceRequired := singleItem
		namespace, err = GetArg[string](p.Args, NamespaceArg, isNamespaceRequired)
		if err != nil {
			logger.Error(err, "Failed to get namespace argument")
			sendErr(fmt.Errorf("failed to get namespace argument: %w", err))
			return
		}
		if namespace != "" {
			opts = append(opts, ctrlruntimeclient.InNamespace(namespace))
		}
	}

	if labelSelector != "" {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			logger.WithValues(LabelSelectorArg, labelSelector).Error(err, "Invalid label selector")
			sendErr(fmt.Errorf("invalid label selector: %w", err))
			return
		}
		opts = append(opts, ctrlruntimeclient.MatchingLabelsSelector{Selector: selector})
	}

	var name string
	if singleItem {
		name, err = GetArg[string](p.Args, NameArg, true)
		if err != nil {
			logger.Error(err, "Failed to get name argument")
			sendErr(fmt.Errorf("failed to get name argument: %w", err))
			return
		}
		opts = append(opts, ctrlruntimeclient.MatchingFields{"metadata.name": name})
	}

	// If no resourceVersion provided, perform an initial LIST to obtain current items and resourceVersion,
	// If a resourceVersion is provided, start WATCH from that resourceVersion without initial listing.

	// Track last-seen objects for change detection on MODIFIED
	previousObjects := make(map[string]*unstructured.Unstructured)

	lastRV := resourceVersion

	backoff := wait.Backoff{
		Duration: watchReconnectInitialDelay,
		Cap:      watchReconnectMaxDelay,
		Steps:    watchReconnectSteps,
		Factor:   watchReconnectFactor,
		Jitter:   watchReconnectJitter,
	}
	delay := backoff.DelayWithReset(&clock.RealClock{}, watchReconnectResetAfter)

	// NOTE: Currently retries indefinitely until ctx is cancelled.
	// This may be a candidate for a configurable max retry count or timeout
	// if users need bounded retry behavior.
	_ = delay.Until(ctx, true, true, func(_ context.Context) (bool, error) {
		// --- LIST phase (when no resourceVersion is available) ---
		if lastRV == "" {
			if err := r.runtimeClient.List(ctx, list, opts...); err != nil {
				if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
					logger.Error(err, "Permission denied listing resources")
					sendErr(err)
					return true, nil
				}
				logger.Error(err, "Failed to list resources, will retry")
				return false, nil
			}

			previousObjects = make(map[string]*unstructured.Unstructured)
			for i := range list.Items {
				item := list.Items[i]
				key := item.GetNamespace() + "/" + item.GetName()
				previousObjects[key] = item.DeepCopy()

				envelope := SubscriptionEnvelope{
					Type:   EventTypeAdded,
					Object: item.Object,
				}
				select {
				case <-ctx.Done():
					return true, nil
				case resultChannel <- envelope:
				}
			}

			lastRV = list.GetResourceVersion()
		}

		// --- WATCH phase ---
		watchOpts := append([]ctrlruntimeclient.ListOption{}, opts...)
		if lastRV != "" {
			watchOpts = append(watchOpts, &ctrlruntimeclient.ListOptions{
				Raw: &metav1.ListOptions{ResourceVersion: lastRV},
			})
		}

		watcher, err := r.runtimeClient.Watch(ctx, list, watchOpts...)
		if err != nil {
			if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
				logger.Error(err, "Permission denied starting watch")
				sendErr(err)
				return true, nil
			}
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				logger.V(1).Info("Resource version expired on watch creation, will re-list")
				lastRV = ""
				return false, nil
			}
			logger.Error(err, "Failed to start watch, will retry")
			return false, nil
		}
		defer watcher.Stop()

		// --- EVENT LOOP ---
		for {
			select {
			case event, ok := <-watcher.ResultChan():
				if !ok {
					logger.V(1).Info("Watch channel closed, reconnecting")
					return false, nil
				}

				if event.Type == watch.Error {
					statusErr := apierrors.FromObject(event.Object)
					if apierrors.IsForbidden(statusErr) || apierrors.IsUnauthorized(statusErr) {
						logger.Error(statusErr, "Permission denied during watch")
						sendErr(statusErr)
						return true, nil
					}
					if apierrors.IsResourceExpired(statusErr) || apierrors.IsGone(statusErr) {
						logger.V(1).Info("Resource version expired, restarting watch")
						lastRV = ""
						return false, nil
					}
					logger.Error(statusErr, "Watch error event, restarting",
						"reason", string(apierrors.ReasonForError(statusErr)),
					)
					return false, nil
				}

				obj, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					err = ErrFailedToCastEventObjectToUnstructured
					logger.Error(err, "Failed to cast event object to unstructured")
					sendErr(fmt.Errorf("failed to cast event object to unstructured: %w", err))
					return true, nil
				}

				if rv := obj.GetResourceVersion(); rv != "" {
					lastRV = rv
				}

				key := obj.GetNamespace() + "/" + obj.GetName()

				var sendUpdate bool
				var eventType string
				switch event.Type {
				case watch.Added:
					previousObjects[key] = obj.DeepCopy()
					sendUpdate = true
					eventType = EventTypeAdded
				case watch.Modified:
					oldObj := previousObjects[key]
					if subscribeToAll {
						sendUpdate = true
					} else {
						var changed bool
						changed, err = determineFieldChanged(oldObj, obj, fieldsToWatch)
						if err != nil {
							logger.Error(err, "Failed to determine field changes")
							sendErr(fmt.Errorf("failed to determine field changed: %w", err))
							return true, nil
						}
						sendUpdate = changed
					}
					previousObjects[key] = obj.DeepCopy()
					if sendUpdate {
						eventType = EventTypeModified
					}
				case watch.Deleted:
					delete(previousObjects, key)
					sendUpdate = true
					eventType = EventTypeDeleted
				}

				if sendUpdate {
					var payload any = obj.Object

					if m, ok := payload.(map[string]any); !ok || m == nil {
						payload = SubscriptionObject{
							Metadata: SubscriptionMetadata{
								Name:            obj.GetName(),
								Namespace:       obj.GetNamespace(),
								ResourceVersion: obj.GetResourceVersion(),
							},
						}
					}

					envelope := SubscriptionEnvelope{
						Type:   eventType,
						Object: payload,
					}

					select {
					case <-ctx.Done():
						return true, nil
					case resultChannel <- envelope:
					}
				}
			case <-ctx.Done():
				return true, nil
			}
		}
	})
}

// extractRequestedFields uses p.Info to determine the fields requested by the client.
// It returns a slice of strings representing the "paths" of requested fields.
func extractRequestedFields(info graphql.ResolveInfo) []string {
	var fields []string
	for _, fieldAST := range info.FieldASTs {
		if fieldAST.SelectionSet == nil {
			continue
		}
		for _, sel := range fieldAST.SelectionSet.Selections {
			if f, ok := sel.(*ast.Field); ok {
				if f.Name.Value == "object" {
					fields = append(fields, parseSelectionSet(f.SelectionSet, "")...)
				}
			}
		}
	}
	return fields
}

// parseSelectionSet recursively extracts field paths from a selection set.
// If `prefix` is non-empty, it prefixes subfields with `prefix + "."`.
func parseSelectionSet(selectionSet *ast.SelectionSet, prefix string) []string {
	var result []string
	if selectionSet == nil {
		return result
	}

	for _, selection := range selectionSet.Selections {
		switch sel := selection.(type) {
		case *ast.Field:
			fieldName := sel.Name.Value
			fullPath := fieldName
			if prefix != "" {
				fullPath = prefix + "." + fieldName
			}

			// If this field has a sub-selection set, recurse
			if sel.SelectionSet != nil && len(sel.SelectionSet.Selections) > 0 {
				subFields := parseSelectionSet(sel.SelectionSet, fullPath)
				result = append(result, subFields...)
			} else {
				// Leaf field
				result = append(result, fullPath)
			}
		}
	}
	return result
}

func determineFieldChanged(oldObj, newObj *unstructured.Unstructured, fields []string) (bool, error) {
	if oldObj == nil {
		// No previous object, so treat as changed
		return true, nil
	}

	for _, fieldPath := range fields {
		oldValue, foundOld, err := getFieldValue(oldObj, fieldPath)
		if err != nil {
			return false, err
		}
		newValue, foundNew, err := getFieldValue(newObj, fieldPath)
		if err != nil {
			return false, err
		}
		if !foundOld && !foundNew {
			// Field not present in both, consider no change
			continue
		}
		if !foundOld || !foundNew {
			// Field present in one but not the other, so changed
			return true, nil
		}
		if !reflect.DeepEqual(oldValue, newValue) {
			// Field value has changed
			return true, nil
		}
	}

	return false, nil
}

// Helper function to get the value of a field from an unstructured object
func getFieldValue(obj *unstructured.Unstructured, fieldPath string) (any, bool, error) {
	fields := strings.Split(fieldPath, ".")
	var current any = obj.Object

	for i, field := range fields {
		switch v := current.(type) {
		case map[string]any:
			value, found, err := unstructured.NestedFieldNoCopy(v, field)
			if err != nil {
				return nil, false, fmt.Errorf("error accessing field %s: %v", strings.Join(fields[:i+1], "."), err)
			}
			if !found {
				return nil, false, nil
			}
			current = value
		case []any:
			// in case of slice, we return it, and that slice will be compared later using deep equal
			return current, true, nil
		default:
			return nil, false, fmt.Errorf("unexpected type at field %s, expected map[string]interface{} or []interface{}, got %T", strings.Join(fields[:i+1], "."), current)
		}
	}

	return current, true, nil
}

func CreateSubscriptionResolver() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		source := p.Source

		if err, ok := source.(error); ok {
			return nil, err
		}

		return source, nil
	}
}
