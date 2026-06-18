/*
Copyright 2024.

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

// Command release cuts release tags for the components declared in the
// repository-root release.yaml.
//
// Each component has its own tag namespace and an independent version line
// (e.g. account-operator/v<X.Y.Z>). The tool finds the component's latest
// existing tag, bumps it (patch by default), and creates + pushes the new tag —
// the per-component workflow under .github/workflows/<name>.yml does the rest.
//
// Usage:
//
//	release <component|all> [flags]
//
//	release account-operator              # bump account-operator/v* patch and push
//	release security-operator --minor     # bump the minor
//	release account-operator --tag v0.2.0 # explicit version
//	release all --dry-run                 # preview every component's next tag
//
// Flags:
//
//	--config <path>  release config (default: <repo-root>/release.yaml)
//	--tag <vX.Y.Z>   set the exact version (single component only)
//	--minor          bump the minor
//	--major          bump the major
//	--rc             cut a release-candidate prerelease (-rcN) instead of a release
//	--ref <commit>   commit/ref to tag (default: HEAD)
//	--dry-run        print the plan, create nothing
//	-y, --yes        don't prompt for confirmation before pushing
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// component is one releasable line, as declared in release.yaml.
type component struct {
	// Name is the component selector passed on the command line.
	Name string `yaml:"name"`
	// Prefix is the tag prefix; the version is appended directly, e.g. prefix
	// "account-operator/v" + "1.2.3" = "account-operator/v1.2.3".
	Prefix string `yaml:"prefix"`
	// Triggers is a one-line summary of what cutting the tag sets in motion —
	// shown in the plan so a dry-run makes the downstream effect obvious.
	Triggers string `yaml:"triggers"`
}

// config is the parsed release.yaml. Component order is preserved from the file
// and used for `all`.
type config struct {
	DefaultBump string      `yaml:"defaultBump"`
	Components  []component `yaml:"components"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type options struct {
	config string // path to release.yaml ("" -> auto-discover from repo root)
	tag    string // explicit version override (e.g. "v0.2.0")
	bump   string // "patch" | "minor" | "major" ("" -> config defaultBump)
	ref    string // commit/ref to tag
	rc     bool   // cut a release-candidate prerelease (-rcN) instead of a release
	dryRun bool
	yes    bool
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		usage()
		return nil
	}
	target := args[0]
	opts := options{ref: "HEAD"}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return fmt.Errorf("--config needs a value")
			}
			opts.config = args[i]
		case "--tag":
			i++
			if i >= len(args) {
				return fmt.Errorf("--tag needs a value")
			}
			opts.tag = args[i]
		case "--minor":
			opts.bump = "minor"
		case "--major":
			opts.bump = "major"
		case "--rc":
			opts.rc = true
		case "--ref":
			i++
			if i >= len(args) {
				return fmt.Errorf("--ref needs a value")
			}
			opts.ref = args[i]
		case "--dry-run":
			opts.dryRun = true
		case "-y", "--yes":
			opts.yes = true
		default:
			return fmt.Errorf("unknown flag %q (try --help)", args[i])
		}
	}

	cfg, err := loadConfig(opts.config)
	if err != nil {
		return err
	}
	if opts.bump == "" {
		opts.bump = cfg.DefaultBump
	}
	if opts.bump == "" {
		opts.bump = "patch"
	}
	if opts.rc && opts.tag != "" {
		return fmt.Errorf("--rc cannot be combined with --tag (set the prerelease directly in --tag, e.g. --tag v0.2.0-rc1)")
	}

	byName := make(map[string]component, len(cfg.Components))
	var order []string
	for _, c := range cfg.Components {
		byName[c.Name] = c
		order = append(order, c.Name)
	}

	// Resolve the target component set.
	var names []string
	if target == "all" {
		if opts.tag != "" {
			return fmt.Errorf("--tag cannot be combined with 'all' (each component has its own version)")
		}
		names = order
	} else {
		if _, ok := byName[target]; !ok {
			return fmt.Errorf("unknown component %q; valid: all, %s", target, strings.Join(order, ", "))
		}
		names = []string{target}
	}

	commit, err := gitOut("rev-parse", "--short", opts.ref)
	if err != nil {
		return fmt.Errorf("resolving ref %q: %w", opts.ref, err)
	}
	branch, _ := gitOut("rev-parse", "--abbrev-ref", "HEAD")

	// Build the plan.
	type plan struct{ name, from, fullTag, triggers string }
	var plans []plan
	for _, name := range names {
		comp := byName[name]
		latest, hasLatest, err := latestTag(comp.Prefix)
		if err != nil {
			return err
		}

		var next version
		switch {
		case opts.tag != "":
			v, ok := parseVersion(opts.tag)
			if !ok {
				return fmt.Errorf("invalid --tag %q (want vMAJOR.MINOR.PATCH[-pre])", opts.tag)
			}
			next = v
		case opts.rc:
			next = nextRC(latest, hasLatest, opts.bump)
		case hasLatest:
			next = bump(latest, opts.bump)
		default:
			next = version{0, 0, 1, ""} // first release
		}

		full := comp.Prefix + strings.TrimPrefix(next.String(), "v")
		from := "(none)"
		if hasLatest {
			from = comp.Prefix + strings.TrimPrefix(latest.String(), "v")
		}
		plans = append(plans, plan{name, from, full, comp.Triggers})
	}

	// Show the plan: the version step and what each tag sets in motion.
	fmt.Printf("Tagging commit %s (%s):\n\n", commit, branch)
	for _, p := range plans {
		fmt.Printf("  %-18s %s  ->  %s\n", p.name, p.from, p.fullTag)
		if p.triggers != "" {
			fmt.Printf("  %-18s   ↳ %s\n", "", p.triggers)
		}
	}
	fmt.Println()

	if opts.dryRun {
		fmt.Println("dry-run — would run:")
		for _, p := range plans {
			fmt.Printf("  git tag %s %s && git push origin %s\n", p.fullTag, opts.ref, p.fullTag)
		}
		return nil
	}

	if !opts.yes {
		ok, err := confirm(fmt.Sprintf("Create and push %d tag(s)? [y/N] ", len(plans)))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("aborted.")
			return nil
		}
	}

	// Create the tags locally first; only push once all are created so a bad
	// version doesn't leave a half-pushed set.
	for _, p := range plans {
		if err := gitRun("tag", p.fullTag, opts.ref); err != nil {
			return fmt.Errorf("creating tag %s: %w", p.fullTag, err)
		}
	}
	for _, p := range plans {
		if err := gitRun("push", "origin", p.fullTag); err != nil {
			return fmt.Errorf("pushing tag %s: %w (other tags were created locally; `git push origin <tag>` to retry)", p.fullTag, err)
		}
		fmt.Printf("pushed %s\n", p.fullTag)
	}
	fmt.Println("\nDone — the release workflows will pick these up.")
	return nil
}

// loadConfig reads release.yaml. When path is empty it looks for release.yaml at
// the git repository root, so the tool works from any subdirectory.
func loadConfig(path string) (config, error) {
	if path == "" {
		root, err := gitOut("rev-parse", "--show-toplevel")
		if err != nil {
			return config{}, fmt.Errorf("locating repo root (not a git checkout?): %w", err)
		}
		path = filepath.Join(root, "release.yaml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if len(cfg.Components) == 0 {
		return config{}, fmt.Errorf("config %s declares no components", path)
	}
	for i, c := range cfg.Components {
		if c.Name == "" || c.Prefix == "" {
			return config{}, fmt.Errorf("config %s: component %d is missing name or prefix", path, i)
		}
	}
	return cfg, nil
}

// latestTag returns the highest semver tag carrying prefix, with the prefix
// stripped. hasLatest is false when no matching tag exists.
func latestTag(prefix string) (version, bool, error) {
	out, err := gitOut("tag", "-l", prefix+"*")
	if err != nil {
		return version{}, false, fmt.Errorf("listing tags %q: %w", prefix+"*", err)
	}
	var vs []version
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, prefix) {
			continue
		}
		if v, ok := parseVersion("v" + strings.TrimPrefix(line, prefix)); ok {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 {
		return version{}, false, nil
	}
	sort.Slice(vs, func(i, j int) bool { return less(vs[i], vs[j]) })
	return vs[len(vs)-1], true, nil
}

// version is a parsed semver (vMAJOR.MINOR.PATCH[-prerelease]).
type version struct {
	major, minor, patch int
	pre                 string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre := s, ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core, pre = s[:i], s[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		nums[i] = n
	}
	return version{nums[0], nums[1], nums[2], pre}, true
}

func (v version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

// less orders versions; a release (no prerelease) outranks its prereleases.
func less(a, b version) bool {
	switch {
	case a.major != b.major:
		return a.major < b.major
	case a.minor != b.minor:
		return a.minor < b.minor
	case a.patch != b.patch:
		return a.patch < b.patch
	case a.pre == b.pre:
		return false
	case a.pre == "":
		return false // a is the release, ranks above b's prerelease
	case b.pre == "":
		return true
	default:
		// Both are prereleases. Compare rcN numerically so rc10 outranks rc2;
		// fall back to lexical order for anything that isn't an rc.
		if an, ok := rcNumber(a.pre); ok {
			if bn, ok := rcNumber(b.pre); ok {
				return an < bn
			}
		}
		return a.pre < b.pre
	}
}

// nextRC returns the next -rcN prerelease. If the latest tag is already an rcN
// prerelease it increments N on the same core (e.g. v0.2.0-rc1 -> v0.2.0-rc2),
// so iterating a release candidate stays on one version line and the bump part
// is ignored. Otherwise it bumps the core per part and starts at rc1
// (e.g. v0.1.0 -> patch -> v0.1.1-rc1).
func nextRC(latest version, hasLatest bool, part string) version {
	if hasLatest {
		if n, ok := rcNumber(latest.pre); ok {
			v := latest
			v.pre = fmt.Sprintf("rc%d", n+1)
			return v
		}
		v := bump(latest, part)
		v.pre = "rc1"
		return v
	}
	return version{0, 0, 1, "rc1"}
}

// rcNumber parses an "rcN" prerelease, returning N. ok is false for any other
// (or empty) prerelease string.
func rcNumber(pre string) (int, bool) {
	if !strings.HasPrefix(pre, "rc") {
		return 0, false
	}
	n, err := strconv.Atoi(pre[len("rc"):])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// bump increments part and drops any prerelease, so a release tag follows from
// the latest version's core (e.g. v0.0.1-rc1 -> patch -> v0.0.2).
func bump(v version, part string) version {
	switch part {
	case "major":
		return version{v.major + 1, 0, 0, ""}
	case "minor":
		return version{v.major, v.minor + 1, 0, ""}
	default:
		return version{v.major, v.minor, v.patch + 1, ""}
	}
}

func confirm(prompt string) (bool, error) {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, nil // EOF / no tty -> treat as no
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func gitOut(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	return strings.TrimSpace(string(out)), err
}

func gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func usage() {
	fmt.Print(`release — cut release tags for the components in release.yaml

Usage:
  release <component|all> [flags]

The releasable components are declared in the repository-root release.yaml.
Run 'release all --dry-run' to see them and their next versions.

Flags:
  --config <path>  release config (default: <repo-root>/release.yaml)
  --tag <vX.Y.Z>   set the exact version (single component only)
  --minor          bump the minor
  --major          bump the major
  --rc             cut a release-candidate prerelease (-rcN) instead of a release
  --ref <commit>   commit/ref to tag (default: HEAD)
  --dry-run        print the plan, create nothing
  -y, --yes        skip the confirmation prompt

Examples:
  release account-operator               bump account-operator/v* patch and push
  release security-operator --minor      bump the minor
  release account-operator --rc          cut the next account-operator rc (e.g. v0.1.1-rc1)
  release account-operator --tag v0.2.0  explicit version
  release all --dry-run                  preview every component's next tag
`)
}
