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

package scratchrepo

import (
	"testing"
)

// T is a Repo whose failures end the test and which is closed with it.
type T struct {
	*Repo
	tb testing.TB
}

// NewT creates a repository for a test. Not for parallel tests.
func NewT(tb testing.TB) *T {
	tb.Helper()
	repo, err := New(tb.Context())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := repo.Close(); err != nil {
			tb.Error(err)
		}
	})
	return &T{
		Repo: repo,
		tb:   tb,
	}
}

// Git runs git in the repository and returns its trimmed output.
func (t *T) Git(args ...string) string {
	t.tb.Helper()
	out, err := t.Repo.Git(t.tb.Context(), args...)
	if err != nil {
		t.tb.Fatal(err)
	}
	return out
}

// Commit adds an empty commit.
func (t *T) Commit(message string) {
	t.tb.Helper()
	if err := t.Repo.Commit(t.tb.Context(), message); err != nil {
		t.tb.Fatal(err)
	}
}

// Tag tags HEAD.
func (t *T) Tag(name string) {
	t.tb.Helper()
	if err := t.Repo.Tag(t.tb.Context(), name); err != nil {
		t.tb.Fatal(err)
	}
}

// Head returns the full hash of HEAD.
func (t *T) Head() string {
	t.tb.Helper()
	head, err := t.Repo.Head(t.tb.Context())
	if err != nil {
		t.tb.Fatal(err)
	}
	return head
}
