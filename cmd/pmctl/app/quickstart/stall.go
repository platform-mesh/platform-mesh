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

package quickstart

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// dependencyNotReady is the reason of a HelmRelease that waits for another
// one: not a failure of its own.
const dependencyNotReady = "DependencyNotReady"

// stallDetector notices HelmReleases that keep failing, so that a rollout
// that cannot finish is reported with its cause instead of running into the
// timeout.
//
// A failing release is normal for a while: charts are installed before the
// CRDs or sources they need exist, and Flux retries. So a release only
// counts as stalled once it has been failing for the whole grace period
// without becoming ready in between. The time is kept here: the transition
// time of the condition starts over with every retry.
type stallDetector struct {
	client dynamic.Interface
	grace  time.Duration
	// failing is when each release was first seen failing.
	failing map[string]time.Time
}

func newStallDetector(client dynamic.Interface, grace time.Duration) *stallDetector {
	return &stallDetector{client: client, grace: grace, failing: map[string]time.Time{}}
}

// check returns an error naming the stalled releases and what they report,
// if there are any. A grace period of zero turns it off.
func (s *stallDetector) check(ctx context.Context) error {
	if s.grace <= 0 {
		return nil
	}
	releases, err := s.client.Resource(helmReleaseGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// Not what this looks for; the wait it runs in reports API errors.
		return nil //nolint:nilerr // see above
	}

	now := time.Now()
	var stalled []string
	for _, release := range releases.Items {
		name := release.GetName()
		status, text := condition(&release, "Ready")
		switch {
		case status == string(metav1.ConditionTrue):
			delete(s.failing, name)
		case status == string(metav1.ConditionFalse) && !strings.HasPrefix(text, dependencyNotReady):
			since, seen := s.failing[name]
			if !seen {
				s.failing[name] = now
				continue
			}
			if now.Sub(since) >= s.grace {
				stalled = append(stalled, fmt.Sprintf("%s: %s", name, firstLine(text)))
			}
		}
		// Unknown is a retry in progress: neither ready nor a new failure.
	}
	if len(stalled) == 0 {
		return nil
	}
	slices.Sort(stalled)
	return fmt.Errorf("HelmReleases failing for more than %s:\n  %s", s.grace, strings.Join(stalled, "\n  "))
}

// firstLine cuts a multi-line message to its first line, and that to a
// length that reads in a terminal.
func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return runewidth.Truncate(s, 300, "…")
}
