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

package pmversions

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
)

var (
	tagPattern           = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-(.+))?$`)
	releaseBranchPattern = regexp.MustCompile(`^release-(\d+)\.(\d+)$`)
)

// Version is a release tag vYY.MM.P with an optional prerelease suffix.
type Version struct {
	Year  int
	Month int
	Patch int
	// Pre is the part after "-", empty for a final release.
	Pre string
}

// ParseTag parses a release tag vYY.MM.P or vYY.MM.P-<pre>.
func ParseTag(tag string) (Version, error) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a release tag vYY.MM.P[-pre]", tag)
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if month < 1 || month > 12 {
		return Version{}, fmt.Errorf("%q has month %d outside 1..12", tag, month)
	}
	v := Version{
		Year:  year,
		Month: month,
		Patch: patch,
		Pre:   m[4],
	}
	return v, nil
}

// ParseReleaseBranch parses the year and month out of a release branch
// in the form `release-YY.MM`.
func ParseReleaseBranch(branch string) (Version, bool) {
	m := releaseBranchPattern.FindStringSubmatch(branch)
	if m == nil {
		return Version{}, false
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	if month < 1 || month > 12 {
		return Version{}, false
	}
	v := Version{
		Year:  year,
		Month: month,
	}
	return v, true
}

// String returns the version in the form vYY.MM.P[-pre].
func (version Version) String() string {
	s := fmt.Sprintf("%s.%d", version.Minor(), version.Patch)
	if version.Pre != "" {
		s += "-" + version.Pre
	}
	return s
}

// LibraryString returns the version in the go library form of v0.YYMM.P[-pre].
func (version Version) LibraryString() string {
	s := fmt.Sprintf("v0.%d%02d.%d", version.Year, version.Month, version.Patch)
	if version.Pre != "" {
		s += "-" + version.Pre
	}
	return s
}

// Final reports whether the version has no prerelease suffix.
func (version Version) Final() bool {
	return version.Pre == ""
}

// Minor returns the version in the form vYY.MM.
func (version Version) Minor() string {
	return fmt.Sprintf("v%d.%02d", version.Year, version.Month)
}

// SameMinor reports whether both versions belong to the same feature release.
func (version Version) SameMinor(other Version) bool {
	return version.Year == other.Year && version.Month == other.Month
}

// NextMinor returns the first release of the feature release one month later.
func (version Version) NextMinor() Version {
	if version.Month == 12 {
		return Version{
			Year:  version.Year + 1,
			Month: 1,
		}
	}
	return Version{
		Year:  version.Year,
		Month: version.Month + 1,
	}
}

// Compare compares version with other and returns:
//
//	-1 if version is less than other,
//	 0 if version equals other,
//	+1 if version is greater than other.
//
// A prerelease is lower than the final release of the same patch.
func (version Version) Compare(other Version) int {
	if c := cmp.Compare(version.Year, other.Year); c != 0 {
		return c
	}
	if c := cmp.Compare(version.Month, other.Month); c != 0 {
		return c
	}
	if c := cmp.Compare(version.Patch, other.Patch); c != 0 {
		return c
	}
	if version.Final() != other.Final() {
		if version.Final() {
			return 1
		}
		return -1
	}
	return cmp.Compare(version.Pre, other.Pre)
}
