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

package models

import "time"

// ProjectSchema represents the full schema of a GitHub ProjectV2
type ProjectSchema struct {
	Project   ProjectInfo   `json:"project"`
	Fields    []FieldSchema `json:"fields"`
	FetchedAt time.Time     `json:"fetched_at"`
}

// ProjectInfo contains basic project information
type ProjectInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Organization string `json:"organization"`
	Number       int    `json:"number"`
	Description  string `json:"description,omitempty"`
	Public       bool   `json:"public"`
	URL          string `json:"url"`
}

// FieldSchema represents a project field definition
type FieldSchema struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	DataType   FieldDataType   `json:"data_type"`
	Options    []SelectOption  `json:"options,omitempty"`    // For SINGLE_SELECT
	Iterations []IterationInfo `json:"iterations,omitempty"` // For ITERATION
}

// FieldDataType represents the type of a project field
type FieldDataType string

const (
	FieldTypeText         FieldDataType = "TEXT"
	FieldTypeNumber       FieldDataType = "NUMBER"
	FieldTypeDate         FieldDataType = "DATE"
	FieldTypeSingleSelect FieldDataType = "SINGLE_SELECT"
	FieldTypeIteration    FieldDataType = "ITERATION"
	FieldTypeAssignees    FieldDataType = "ASSIGNEES"
	FieldTypeLabels       FieldDataType = "LABELS"
	FieldTypeMilestone    FieldDataType = "MILESTONE"
	FieldTypeRepository   FieldDataType = "REPOSITORY"
	FieldTypeReviewers    FieldDataType = "REVIEWERS"
	FieldTypeLinkedPRs    FieldDataType = "LINKED_PULL_REQUESTS"
	FieldTypeTracks       FieldDataType = "TRACKS"
	FieldTypeTrackedBy    FieldDataType = "TRACKED_BY"
)

// SelectOption represents an option in a single-select field
type SelectOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
}

// IterationInfo represents an iteration in an iteration field
type IterationInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	StartDate string `json:"start_date"`
	Duration  int    `json:"duration"` // in days
}

// GetFieldByName returns a field by name (case-insensitive)
func (s *ProjectSchema) GetFieldByName(name string) *FieldSchema {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	return nil
}

// GetSelectOptions returns option names for a single-select field
func (f *FieldSchema) GetSelectOptions() []string {
	if f.DataType != FieldTypeSingleSelect {
		return nil
	}
	options := make([]string, len(f.Options))
	for i, opt := range f.Options {
		options[i] = opt.Name
	}
	return options
}

// GetIterationTitles returns iteration titles for an iteration field
func (f *FieldSchema) GetIterationTitles() []string {
	if f.DataType != FieldTypeIteration {
		return nil
	}
	titles := make([]string, len(f.Iterations))
	for i, iter := range f.Iterations {
		titles[i] = iter.Title
	}
	return titles
}
