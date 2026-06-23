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

package filter

import (
	"fmt"
	"strings"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// Operator represents the comparison operator for dynamic filters
type Operator string

const (
	OpEquals    Operator = "="
	OpNotEquals Operator = "!="
	OpContains  Operator = "~"
	OpIn        Operator = "in"
)

// DynamicFieldFilter filters items based on project field values
type DynamicFieldFilter struct {
	FieldName string
	Operator  Operator
	Value     string
	Values    []string // For "in" operator
}

// ParseFieldFilter parses a field filter expression like "Type=Epic" or "Status!=Done"
func ParseFieldFilter(expr string) (*DynamicFieldFilter, error) {
	// Try to parse != first (longer operator)
	if idx := strings.Index(expr, "!="); idx > 0 {
		return &DynamicFieldFilter{
			FieldName: strings.TrimSpace(expr[:idx]),
			Operator:  OpNotEquals,
			Value:     strings.TrimSpace(expr[idx+2:]),
		}, nil
	}

	// Try to parse ~
	if idx := strings.Index(expr, "~"); idx > 0 {
		return &DynamicFieldFilter{
			FieldName: strings.TrimSpace(expr[:idx]),
			Operator:  OpContains,
			Value:     strings.TrimSpace(expr[idx+1:]),
		}, nil
	}

	// Try to parse =
	if idx := strings.Index(expr, "="); idx > 0 {
		fieldName := strings.TrimSpace(expr[:idx])
		value := strings.TrimSpace(expr[idx+1:])

		// Check if it's a multi-value (comma-separated)
		if strings.Contains(value, ",") {
			values := strings.Split(value, ",")
			for i := range values {
				values[i] = strings.TrimSpace(values[i])
			}
			return &DynamicFieldFilter{
				FieldName: fieldName,
				Operator:  OpIn,
				Values:    values,
			}, nil
		}

		return &DynamicFieldFilter{
			FieldName: fieldName,
			Operator:  OpEquals,
			Value:     value,
		}, nil
	}

	return nil, fmt.Errorf("invalid filter expression: %q (expected format: Field=Value, Field!=Value, Field~Value, or Field=Value1,Value2)", expr)
}

// NewDynamicFieldFilter creates a new dynamic field filter
func NewDynamicFieldFilter(fieldName string, op Operator, value string) *DynamicFieldFilter {
	return &DynamicFieldFilter{
		FieldName: fieldName,
		Operator:  op,
		Value:     value,
	}
}

// Matches checks if the item's field value matches the filter criteria
func (f *DynamicFieldFilter) Matches(item *models.ProjectItem) bool {
	// Get the field value from the item
	fieldValue, ok := item.FieldValues[f.FieldName]

	// If field doesn't exist, check common fallback fields
	if !ok {
		fieldValue = f.getFallbackValue(item)
	}

	switch f.Operator {
	case OpEquals:
		return strings.EqualFold(fieldValue, f.Value)

	case OpNotEquals:
		return !strings.EqualFold(fieldValue, f.Value)

	case OpContains:
		return strings.Contains(strings.ToLower(fieldValue), strings.ToLower(f.Value))

	case OpIn:
		for _, v := range f.Values {
			if strings.EqualFold(fieldValue, v) {
				return true
			}
		}
		return false
	}

	return false
}

// getFallbackValue checks common built-in fields if not found in FieldValues
func (f *DynamicFieldFilter) getFallbackValue(item *models.ProjectItem) string {
	switch strings.ToLower(f.FieldName) {
	case "state":
		return item.State
	case "type":
		return string(item.Type)
	case "repository", "repo":
		return item.Repository.FullName()
	case "author":
		return item.Author
	case "milestone":
		if item.Milestone != nil {
			return item.Milestone.Title
		}
	}
	return ""
}

// Name returns the name of the filter
func (f *DynamicFieldFilter) Name() string {
	return fmt.Sprintf("field:%s", f.FieldName)
}

// String returns a string representation of the filter
func (f *DynamicFieldFilter) String() string {
	switch f.Operator {
	case OpIn:
		return fmt.Sprintf("%s=%s", f.FieldName, strings.Join(f.Values, ","))
	default:
		return fmt.Sprintf("%s%s%s", f.FieldName, f.Operator, f.Value)
	}
}

// ParseFieldFilters parses multiple field filter expressions
func ParseFieldFilters(exprs []string) ([]*DynamicFieldFilter, error) {
	filters := make([]*DynamicFieldFilter, 0, len(exprs))
	for _, expr := range exprs {
		f, err := ParseFieldFilter(expr)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

// OrgFilterMode determines how org filtering works
type OrgFilterMode int

const (
	// OrgFilterAll includes all items regardless of org
	OrgFilterAll OrgFilterMode = iota
	// OrgFilterInternal includes only items from the project's org
	OrgFilterInternal
	// OrgFilterExternal includes only items from external orgs
	OrgFilterExternal
	// OrgFilterSpecific includes only items from specified orgs
	OrgFilterSpecific
)

// OrgFilter filters items based on their repository's organization
type OrgFilter struct {
	ProjectOrg string        // The project's organization
	Mode       OrgFilterMode // Filter mode
	Orgs       []string      // Specific orgs to include (for OrgFilterSpecific mode)
}

// NewOrgFilter creates a new org filter
func NewOrgFilter(projectOrg string, mode OrgFilterMode, orgs []string) *OrgFilter {
	return &OrgFilter{
		ProjectOrg: projectOrg,
		Mode:       mode,
		Orgs:       orgs,
	}
}

// NewInternalOrgFilter creates a filter for items from the project's org only
func NewInternalOrgFilter(projectOrg string) *OrgFilter {
	return &OrgFilter{
		ProjectOrg: projectOrg,
		Mode:       OrgFilterInternal,
	}
}

// NewExternalOrgFilter creates a filter for items from external orgs only
func NewExternalOrgFilter(projectOrg string) *OrgFilter {
	return &OrgFilter{
		ProjectOrg: projectOrg,
		Mode:       OrgFilterExternal,
	}
}

// NewSpecificOrgFilter creates a filter for items from specific orgs
func NewSpecificOrgFilter(orgs []string) *OrgFilter {
	return &OrgFilter{
		Mode: OrgFilterSpecific,
		Orgs: orgs,
	}
}

// Matches checks if the item's org matches the filter criteria
func (f *OrgFilter) Matches(item *models.ProjectItem) bool {
	itemOrg := item.Repository.Owner

	switch f.Mode {
	case OrgFilterAll:
		return true

	case OrgFilterInternal:
		return strings.EqualFold(itemOrg, f.ProjectOrg)

	case OrgFilterExternal:
		return !strings.EqualFold(itemOrg, f.ProjectOrg)

	case OrgFilterSpecific:
		for _, org := range f.Orgs {
			if strings.EqualFold(itemOrg, org) {
				return true
			}
		}
		return false
	}

	return true
}

// Name returns the name of the filter
func (f *OrgFilter) Name() string {
	switch f.Mode {
	case OrgFilterInternal:
		return "org:internal"
	case OrgFilterExternal:
		return "org:external"
	case OrgFilterSpecific:
		return fmt.Sprintf("org:%s", strings.Join(f.Orgs, ","))
	default:
		return "org:all"
	}
}

// IsExternal returns true if the item is from an external org
func IsExternal(item *models.ProjectItem, projectOrg string) bool {
	return !strings.EqualFold(item.Repository.Owner, projectOrg)
}
