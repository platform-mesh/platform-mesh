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

package migrate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Migration is a change to an installation that an upgrade does not make by
// itself, for example moving data from one database to another.
type Migration struct {
	// Name identifies the migration.
	Name string
	// Version is the cut-off: the first release that needs the migration.
	// An installation older than Version needs it when it is upgraded to
	// Version or later; one at Version or later has it behind it.
	Version string
	// Summary says in a line what the migration does.
	Summary string
	// Before runs on the installed version, before the upgrade. It has
	// to cope with being run again, after a failure or by mistake.
	Before func(ctx context.Context, env *Env) error
	// After runs on the new version, once the upgrade is rolled out. Nil
	// when there is nothing left to do.
	After func(ctx context.Context, env *Env) error
}

// All returns the known migrations, the one with the oldest cut-off first.
// A new migration is added here.
func All() []Migration {
	migrations := []Migration{
		openfgaToCNPG(),
	}
	slices.SortStableFunc(migrations, func(a, b Migration) int {
		return semver.MustParse(a.Version).Compare(semver.MustParse(b.Version))
	})
	return migrations
}

// pending returns the migrations an installation at version from still
// needs: those with a cut-off after it.
func pending(migrations []Migration, from *semver.Version) []Migration {
	var out []Migration
	for _, m := range migrations {
		if from.LessThan(semver.MustParse(m.Version)) {
			out = append(out, m)
		}
	}
	return out
}

// parseVersion reads a Platform Mesh version: v26.10.2, 26.10.2, or with the
// patch left open, v26.10.x or v26.10. An open patch counts as 0; cut-offs
// are releases, so any patch of 26.10 is on the same side of them.
func parseVersion(s string) (*semver.Version, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if base, ok := strings.CutSuffix(trimmed, ".x"); ok {
		trimmed = base
	}
	v, err := semver.NewVersion(trimmed)
	if err != nil || strings.Count(trimmed, ".") < 1 {
		return nil, fmt.Errorf("%q is not a Platform Mesh version like v26.10.2 or v26.10.x", s)
	}
	return v, nil
}
