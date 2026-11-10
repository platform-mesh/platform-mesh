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

package releasenotes

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const (
	// unversioned is the Chart.yaml appVersion of charts built from this repository.
	unversioned = "0.0.0"
	// Absent marks a version that did not exist at one end of the range.
	Absent = "–"
	// removed marks a version that was dropped by the end of the range.
	removed = "removed"
	// constructorFile declares a third-party component and its pinned resources.
	constructorFile = "component-constructor.yaml"
)

// VersionRow is one line of a version table.
type VersionRow struct {
	Name    string
	From    string
	To      string
	Changed bool
	// Source is the GitHub repository a component built elsewhere comes
	// from (Chart.yaml sources); empty when unknown or not applicable.
	Source string
}

// ChartMeta is the part of Chart.yaml the notes read.
type ChartMeta struct {
	AppVersion string   `yaml:"appVersion"`
	Sources    []string `yaml:"sources"`
}

// ShowFile returns a file at a commit, and false when it does not exist there.
func ShowFile(ctx context.Context, commit, path string) ([]byte, bool, error) {
	entry, err := releaseutils.Git(ctx, "ls-tree", commit, "--", path)
	if err != nil {
		return nil, false, fmt.Errorf("looking up %s at %s: %w", path, commit, err)
	}
	if fields := strings.Fields(entry); len(fields) < 2 || fields[1] != "blob" {
		return nil, false, nil
	}
	out, err := releaseutils.Git(ctx, "show", commit+":"+path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s at %s: %w", path, commit, err)
	}
	return []byte(out), true, nil
}

// ChartNames lists the charts under charts/ at a commit.
func ChartNames(ctx context.Context, commit string) ([]string, error) {
	return dirsWithFile(ctx, commit, "charts", "Chart.yaml")
}

// dirsWithFile lists the direct subdirectories of dir at a commit that
// contain file.
func dirsWithFile(ctx context.Context, commit, dir, file string) ([]string, error) {
	out, err := releaseutils.Git(ctx, "ls-tree", "-r", "--name-only", commit, dir+"/")
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

// ReadChart returns Chart.yaml of a chart at a commit,
// and false when the chart does not exist there.
func ReadChart(ctx context.Context, commit, chart string) (ChartMeta, bool, error) {
	data, ok, err := ShowFile(ctx, commit, "charts/"+chart+"/Chart.yaml")
	if err != nil || !ok {
		return ChartMeta{}, false, err
	}
	var meta ChartMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return ChartMeta{}, false, fmt.Errorf("parsing charts/%s/Chart.yaml at %s: %w", chart, commit, err)
	}
	return meta, true, nil
}

// gitHubSource returns the first GitHub repository among a chart's sources.
func gitHubSource(sources []string) string {
	for _, src := range sources {
		src = strings.TrimSuffix(strings.TrimSuffix(src, "/"), ".git")
		if _, _, err := RepoOwnerName(src); err == nil {
			return src
		}
	}
	return ""
}

// ThirdPartyVersions returns the pinned resource versions of every
// third_party/<dir>/component-constructor.yaml at a commit, keyed
// <dir>/<resource>.
func ThirdPartyVersions(ctx context.Context, commit string) (map[string]string, error) {
	dirs, err := dirsWithFile(ctx, commit, "third_party", constructorFile)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, dir := range dirs {
		path := "third_party/" + dir + "/" + constructorFile
		data, ok, err := ShowFile(ctx, commit, path)
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
func shippedComponents(comps []Component) []string {
	var names []string
	for _, c := range comps {
		if c.Library || c.Image == "" {
			continue
		}
		names = append(names, c.Name)
	}
	return names
}

// OtherComponentRows lists the charts of components built in other
// repositories with their appVersion at both commits.
func OtherComponentRows(ctx context.Context, start, end string) ([]VersionRow, error) {
	charts, err := ChartNames(ctx, end)
	if err != nil {
		return nil, err
	}
	var rows []VersionRow
	for _, chart := range charts {
		meta, _, err := ReadChart(ctx, end, chart)
		if err != nil {
			return nil, err
		}
		if meta.AppVersion == "" || meta.AppVersion == unversioned {
			continue
		}
		before, _, err := ReadChart(ctx, start, chart)
		if err != nil {
			return nil, err
		}
		from := before.AppVersion
		if from == "" || from == unversioned {
			from = Absent
		}
		rows = append(rows, VersionRow{
			Name:    chart,
			From:    from,
			To:      meta.AppVersion,
			Changed: from != meta.AppVersion,
			Source:  gitHubSource(meta.Sources),
		})
	}
	return rows, nil
}

// thirdPartyRows lists the pins that differ between the two maps.
func thirdPartyRows(from, to map[string]string) []VersionRow {
	all := map[string]bool{}
	for k := range from {
		all[k] = true
	}
	for k := range to {
		all[k] = true
	}
	var rows []VersionRow
	for _, k := range slices.Sorted(maps.Keys(all)) {
		before, inFrom := from[k]
		after, inTo := to[k]
		if inFrom && inTo && before == after {
			continue
		}
		if !inFrom {
			before = Absent
		}
		if !inTo {
			after = removed
		}
		rows = append(rows, VersionRow{
			Name:    k,
			From:    before,
			To:      after,
			Changed: true,
		})
	}
	return rows
}
