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
	"errors"
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Argument name constants
const (
	LabelSelectorArg   = "labelselector"
	NameArg            = "name"
	NamespaceArg       = "namespace"
	ObjectArg          = "object"
	SubscribeToAllArg  = "subscribeToAll"
	SortByArg          = "sortBy"
	DryRunArg          = "dryRun"
	ResourceVersionArg = "resourceVersion"
	LimitArg           = "limit"
	ContinueArg        = "continue"
	YamlArg            = "yaml"
)

var (
	NameArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.NewNonNull(graphql.String),
		Description: "The name of the object",
	}

	NamespaceArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.String,
		Description: "The namespace in which to search for the objects",
	}

	LabelSelectorArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.String,
		Description: "A label selector to filter the objects by",
	}

	DryRunArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.Boolean,
		Description: "If true, the operation will be performed in dry-run mode",
	}

	SubscribeToAllArgConfig = &graphql.ArgumentConfig{
		Type:         graphql.Boolean,
		DefaultValue: false,
		Description:  "If true, events will be emitted on every field change",
	}

	ResourceVersionArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.String,
		Description: "If set, subscription will stream changes starting from this resourceVersion. If omitted will return all",
	}

	SortByArgConfig = &graphql.ArgumentConfig{
		Type:         graphql.String,
		Description:  "The field to sort the results by",
		DefaultValue: "metadata.name",
	}

	LimitArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.Int,
		Description: "Maximum number of items to return (server may return fewer)",
	}

	ContinueArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.String,
		Description: "Continue token from a previous list call to retrieve the next page",
	}

	YamlArgConfig = &graphql.ArgumentConfig{
		Type:        graphql.NewNonNull(graphql.String),
		Description: "YAML manifest to apply (single document only)",
	}
)

// ItemArgs returns arguments for single item queries (name + optional namespace)
func ItemArgs(scope apiextensionsv1.ResourceScope) graphql.FieldConfigArgument {
	args := graphql.FieldConfigArgument{
		NameArg: NameArgConfig,
	}
	if isResourceNamespaceScoped(scope) {
		args[NamespaceArg] = NamespaceArgConfig
	}
	return args
}

// ListArgs returns arguments for list queries
func ListArgs(scope apiextensionsv1.ResourceScope) graphql.FieldConfigArgument {
	args := graphql.FieldConfigArgument{
		LabelSelectorArg: LabelSelectorArgConfig,
		SortByArg:        SortByArgConfig,
		LimitArg:         LimitArgConfig,
		ContinueArg:      ContinueArgConfig,
	}
	if isResourceNamespaceScoped(scope) {
		args[NamespaceArg] = NamespaceArgConfig
	}
	return args
}

// SubscriptionItemArgs returns arguments for single item subscriptions
func SubscriptionItemArgs(scope apiextensionsv1.ResourceScope) graphql.FieldConfigArgument {
	args := ItemArgs(scope)
	args[SubscribeToAllArg] = SubscribeToAllArgConfig
	args[ResourceVersionArg] = ResourceVersionArgConfig
	return args
}

// SubscriptionListArgs returns arguments for list subscriptions
func SubscriptionListArgs(scope apiextensionsv1.ResourceScope) graphql.FieldConfigArgument {
	args := ListArgs(scope)
	args[SubscribeToAllArg] = SubscribeToAllArgConfig
	args[ResourceVersionArg] = ResourceVersionArgConfig
	return args
}

// CreateArgs returns arguments for create mutations
func CreateArgs(scope apiextensionsv1.ResourceScope, inputType *graphql.InputObject) graphql.FieldConfigArgument {
	args := graphql.FieldConfigArgument{
		ObjectArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(inputType),
			Description: "The object to create or update",
		},
		DryRunArg: DryRunArgConfig,
	}
	if isResourceNamespaceScoped(scope) {
		args[NamespaceArg] = NamespaceArgConfig
	}
	return args
}

// UpdateArgs returns arguments for update mutations
func UpdateArgs(scope apiextensionsv1.ResourceScope, inputType *graphql.InputObject) graphql.FieldConfigArgument {
	args := CreateArgs(scope, inputType)
	args[NameArg] = NameArgConfig
	return args
}

// DeleteArgs returns arguments for delete mutations
func DeleteArgs(scope apiextensionsv1.ResourceScope) graphql.FieldConfigArgument {
	args := ItemArgs(scope)
	args[DryRunArg] = DryRunArgConfig
	return args
}

// ApplyYamlArgs returns arguments for the applyYaml mutation
func ApplyYamlArgs() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		YamlArg: YamlArgConfig,
	}
}

// Extractable defines types that can be extracted from GraphQL arguments
type Extractable interface {
	string | bool | int
}

// ListResult represents the response structure for list queries.
type ListResult struct {
	ResourceVersion    string           `json:"resourceVersion"`
	Items              []map[string]any `json:"items"`
	Continue           string           `json:"continue"`
	RemainingItemCount *int64           `json:"remainingItemCount"`
}

// ListResultFields returns GraphQL field definitions for ListResult.
// The resourceType parameter is used for the items field type.
func ListResultFields(resourceType *graphql.Object) graphql.Fields {
	return graphql.Fields{
		"resourceVersion":    &graphql.Field{Type: graphql.String},
		"items":              &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType)))},
		"continue":           &graphql.Field{Type: graphql.String},
		"remainingItemCount": &graphql.Field{Type: graphql.Int},
	}
}

// GetArg extracts a typed argument from the args map.
// Returns the zero value if the argument is not present and not required.
// Returns an error if required argument is missing or has wrong type.
func GetArg[T Extractable](args map[string]any, key string, required bool) (T, error) {
	var zero T

	val, exists := args[key]
	if !exists || val == nil {
		if required {
			return zero, fmt.Errorf("missing required argument: %s", key)
		}
		return zero, nil
	}

	typedVal, ok := val.(T)
	if !ok {
		return zero, fmt.Errorf("invalid type for argument: %s", key)
	}

	// For strings, check empty value when required
	if required {
		if str, isString := any(typedVal).(string); isString && str == "" {
			return zero, fmt.Errorf("empty value for argument: %s", key)
		}
	}

	return typedVal, nil
}

func isResourceNamespaceScoped(resourceScope apiextensionsv1.ResourceScope) bool {
	return resourceScope == apiextensionsv1.NamespaceScoped
}

func validateSortBy(items []unstructured.Unstructured, fieldPath string) error {
	if len(items) == 0 {
		return nil // No items to validate against, assume valid
	}

	sample := items[0]
	segments := strings.Split(fieldPath, ".")

	_, found, err := unstructured.NestedFieldNoCopy(sample.Object, segments...)
	if !found {
		return errors.New("specified sortBy field does not exist")
	}
	if err != nil {
		return errors.Join(errors.New("error accessing specified sortBy field"), err)
	}

	return nil
}
