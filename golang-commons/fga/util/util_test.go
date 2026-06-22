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

package util

import (
	"testing"
)

func TestConvertToTypeName(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		singular string
		expected string
	}{
		{
			name:     "basic conversion",
			group:    "apps",
			singular: "deployment",
			expected: "apps_deployment",
		},
		{
			name:     "empty group defaults to core",
			group:    "",
			singular: "pod",
			expected: "core_pod",
		},
		{
			name:     "group with dots gets replaced with underscores",
			group:    "networking.k8s.io",
			singular: "ingress",
			expected: "networking_k8s_io_ingress",
		},
		{
			name:     "single character group and singular",
			group:    "a",
			singular: "b",
			expected: "a_b",
		},
		{
			name:     "numeric group and singular",
			group:    "v1",
			singular: "service",
			expected: "v1_service",
		},
		{
			name:     "mixed case should be lowercased",
			group:    "Apps",
			singular: "Deployment",
			expected: "apps_deployment",
		},
		{
			name:     "short group and singular within limits",
			group:    "batch",
			singular: "job",
			expected: "batch_job",
		},
		{
			name:     "handles special characters in group",
			group:    "group-with-dashes",
			singular: "kind",
			expected: "group-with-dashes_kind",
		},
		{
			name:     "both group and singular empty",
			group:    "",
			singular: "",
			expected: "core_",
		},
		{
			name:     "dots in group get replaced properly",
			group:    "apps.v1",
			singular: "deployment",
			expected: "apps_v1_deployment",
		},
		{
			name:     "multiple dots get replaced",
			group:    "foo.bar.baz",
			singular: "resource",
			expected: "foo_bar_baz_resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToTypeName(tt.group, tt.singular)
			if result != tt.expected {
				t.Errorf("ConvertToTypeName(%q, %q) = %q, want %q", tt.group, tt.singular, result, tt.expected)
			}
		})
	}
}

func TestCapGroupSingularLength(t *testing.T) {
	tests := []struct {
		name      string
		group     string
		singular  string
		maxLength int
		expected  string
	}{
		{
			name:      "group and singular within max length",
			group:     "apps",
			singular:  "deployment",
			maxLength: 50,
			expected:  "apps_deployment",
		},
		{
			name:      "empty group with capGroupSingularLength",
			group:     "",
			singular:  "pod",
			maxLength: 50,
			expected:  "_pod",
		},
		{
			name:      "short group and singular that stays within limits",
			group:     "batch",
			singular:  "job",
			maxLength: 30,
			expected:  "batch_job",
		},
		{
			name:      "exact max length boundary",
			group:     "test",
			singular:  "resource",
			maxLength: 20,
			expected:  "est_resource", // "create_test_resources" = 21 chars, truncate 1: "est_resource"
		},
		{
			name:      "successful truncation case",
			group:     "verylonggroup",
			singular:  "job",
			maxLength: 20,
			expected:  "onggroup_job", // "create_verylonggroup_jobs" = 25 chars, truncate 5: "onggroup_job"
		},
		{
			name:      "truncation case with very long singular",
			group:     "short",
			singular:  "verylongkindnamethatexceedseverything",
			maxLength: 10,
			expected:  "ng", // Additional "s" in relation makes it one char shorter
		},
		{
			name:      "maxLength zero returns original groupKind",
			group:     "apps",
			singular:  "deployment",
			maxLength: 0,
			expected:  "apps_deployment",
		},
		{
			name:      "truncation within group length",
			group:     "exactlength",
			singular:  "test",
			maxLength: 15,
			expected:  "th_test", // "create_exactlength_tests" = 24 chars, truncate 9: "th_test"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := capGroupSingularLength(tt.group, tt.singular, tt.maxLength)
			if result != tt.expected {
				t.Errorf("capGroupSingularLength(%q, %q, %d) = %q, want %q", tt.group, tt.singular, tt.maxLength, result, tt.expected)
			}
		})
	}
}
