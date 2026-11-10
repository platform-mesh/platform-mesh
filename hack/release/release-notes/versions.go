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

package main

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// unversioned is the Chart.yaml appVersion of charts built from this repository.
	unversioned = "0.0.0"
	// absent marks a version that did not exist at one end of the range.
	absent = "–"
	// removed marks a version that was dropped by the end of the range.
	removed = "removed"
	// constructorFile declares a third-party component and its pinned resources.
	constructorFile = "component-constructor.yaml"
)

// versionRow is one line of a version table.
type versionRow struct {
	Name    string
	From    string
	To      string
	Changed bool
}

// showFile returns a file at a commit, and false when it does not exist there.
func showFile(ctx context.Context, commit, path string) ([]byte, bool, error) {
	entry, err := git(ctx, "ls-tree", commit, "--", path)
	if err != nil {
		return nil, false, fmt.Errorf("looking up %s at %s: %w", path, commit, err)
	}
	if fields := strings.Fields(entry); len(fields) < 2 || fields[1] != "blob" {
		return nil, false, nil
	}
	out, err := git(ctx, "show", commit+":"+path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s at %s: %w", path, commit, err)
	}
	return []byte(out), true, nil
}

// chartNames lists the charts under charts/ at a commit.
func chartNames(ctx context.Context, commit string) ([]string, error) {
	return dirsWithFile(ctx, commit, "charts", "Chart.yaml")
}

// dirsWithFile lists the direct subdirectories of dir at a commit that
// contain file.
func dirsWithFile(ctx context.Context, commit, dir, file string) ([]string, error) {
	out, err := git(ctx, "ls-tree", "-r", "--name-only", commit, dir+"/")
	if err != nil {
		return nil, fmt.Errorf("listing %s/ at %s: %w", dir, commit, err)
	}
	var names []string
	for _, path := range strings.Fields(out) {
		parts := strings.Split(path, "/")
		if len(parts) == 3 && parts[0] == dir && parts[2] == file {
			names = append(names, parts[1])
		}
	}
	return names, nil
}

// appVersion returns the appVersion of a chart at a commit,
// empty when the chart does not exist there.
func appVersion(ctx context.Context, commit, chart string) (string, error) {
	data, ok, err := showFile(ctx, commit, "charts/"+chart+"/Chart.yaml")
	if err != nil || !ok {
		return "", err
	}
	var file struct {
		AppVersion string `yaml:"appVersion"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return "", fmt.Errorf("parsing charts/%s/Chart.yaml at %s: %w", chart, commit, err)
	}
	return file.AppVersion, nil
}

// thirdPartyVersions returns the pinned resource versions of every
// third_party/<dir>/component-constructor.yaml at a commit, keyed
// <dir>/<resource>.
func thirdPartyVersions(ctx context.Context, commit string) (map[string]string, error) {
	dirs, err := dirsWithFile(ctx, commit, "third_party", constructorFile)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, dir := range dirs {
		path := "third_party/" + dir + "/" + constructorFile
		data, ok, err := showFile(ctx, commit, path)
		if err != nil || !ok {
			return nil, err
		}
		var file struct {
			Components []struct {
				Resources []struct {
					Name    string `yaml:"name"`
					Version string `yaml:"version"`
				} `yaml:"resources"`
			} `yaml:"components"`
		}
		if err := yaml.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("parsing %s at %s: %w", path, commit, err)
		}
		for _, c := range file.Components {
			for _, r := range c.Resources {
				versions[dir+"/"+r.Name] = r.Version
			}
		}
	}
	return versions, nil
}

// shippedComponents names the components built and published from this
// repository, all at the release version.
func shippedComponents(comps []component) []string {
	var names []string
	for _, c := range comps {
		if c.Library || c.Image == "" {
			continue
		}
		names = append(names, c.Name)
	}
	return names
}

// otherComponentRows lists the charts of components built in other
// repositories with their appVersion at both commits.
func otherComponentRows(ctx context.Context, start, end string) ([]versionRow, error) {
	charts, err := chartNames(ctx, end)
	if err != nil {
		return nil, err
	}
	var rows []versionRow
	for _, chart := range charts {
		to, err := appVersion(ctx, end, chart)
		if err != nil {
			return nil, err
		}
		if to == "" || to == unversioned {
			continue
		}
		from, err := appVersion(ctx, start, chart)
		if err != nil {
			return nil, err
		}
		if from == "" || from == unversioned {
			from = absent
		}
		rows = append(rows, versionRow{
			Name:    chart,
			From:    from,
			To:      to,
			Changed: from != to,
		})
	}
	return rows, nil
}

// thirdPartyRows lists the pins that differ between the two maps.
func thirdPartyRows(from, to map[string]string) []versionRow {
	all := map[string]bool{}
	for k := range from {
		all[k] = true
	}
	for k := range to {
		all[k] = true
	}
	var rows []versionRow
	for _, k := range slices.Sorted(maps.Keys(all)) {
		before, inFrom := from[k]
		after, inTo := to[k]
		if inFrom && inTo && before == after {
			continue
		}
		if !inFrom {
			before = absent
		}
		if !inTo {
			after = removed
		}
		rows = append(rows, versionRow{
			Name:    k,
			From:    before,
			To:      after,
			Changed: true,
		})
	}
	return rows
}
