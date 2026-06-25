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

// Command release cuts release tags for platform-mesh monorepo components.
//
// Each component has its own tag namespace and independent version line. The tag
// prefix is the component name (for go-gettable modules it is also the module's
// directory path, so the tag doubles as the Go-module tag):
//
//	golang-commons     golang-commons/v<X.Y.Z>     go-gettable module tag for
//	                                               go.platform-mesh.io/golang-commons (no image)
//	subroutines        subroutines/v<X.Y.Z>        go-gettable module tag for
//	                                               go.platform-mesh.io/subroutines (no image)
//	apis               apis/v<X.Y.Z>               go-gettable module tag for
//	                                               go.platform-mesh.io/apis (no image)
//	golang-commons     golang-commons/v<X.Y.Z>     golang-commons.yml: go-gettable module
//	                                               tag + GitHub release (no image)
//	subroutines        subroutines/v<X.Y.Z>        subroutines.yml: go-gettable module
//	                                               tag + GitHub release (no image)
//	account-operator   account-operator/v<X.Y.Z>   account-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	backup-operator    backup-operator/v<X.Y.Z>    backup-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	extension-manager-operator   extension-manager-operator/v<X.Y.Z>
//	                                               extension-manager-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	kcp-migration-operator   kcp-migration-operator/v<X.Y.Z>
//	                                               kcp-migration-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	resource-sharding-operator   resource-sharding-operator/v<X.Y.Z>
//	                                               resource-sharding-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	search-operator    search-operator/v<X.Y.Z>    search-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	search-service     search-service/v<X.Y.Z>     search-service.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	security-operator  security-operator/v<X.Y.Z>  security-operator.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	terminal-controller-manager   terminal-controller-manager/v<X.Y.Z>
//	                                               terminal-controller-manager.yml: signed image,
//	iam-service   iam-service/v<X.Y.Z>             iam-service.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	                                               GitHub release, chart bump, SBOM, OCM
//	rebac-authz-webhook   rebac-authz-webhook/v<X.Y.Z>
//	                                               rebac-authz-webhook.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//	virtual-workspaces   virtual-workspaces/v<X.Y.Z>
//	                                               virtual-workspaces.yml: signed image,
//	                                               GitHub release, chart bump, SBOM, OCM
//
// It finds the component's latest existing tag, bumps it (patch by default),
// and creates + pushes the new tag — the release workflow does the rest.
//
// Usage:
//
//	release <component|all> [flags]
//
//	release account-operator             # bump account-operator/v* patch and push
//	release apis --minor                 # bump apis/v* minor
//	release account-operator --tag v0.0.1   # explicit version
//	release all --dry-run                # preview every component's next tag
//
// Flags:
//
//	--tag <vX.Y.Z>   set the exact version (single component only)
//	--minor          bump the minor (default: patch)
//	--major          bump the major
//	--rc             cut a release candidate (vX.Y.Z-rcN); repeat to increment rcN
//	--ref <commit>   commit/ref to tag (default: HEAD)
//	--dry-run        print the plan, create nothing
//	-y, --yes        don't prompt for confirmation before pushing
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// The release component registry (component, componentOrder, components,
// libraryComponents) lives in const.go.

// plan is one component's resolved release step: the tag it moves from, the
// full tag to create, the bare version (e.g. v0.0.1), and what cutting it
// triggers downstream.
type plan struct{ name, from, fullTag, ver, triggers string }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type options struct {
	tag    string // explicit version override (e.g. "v0.0.1")
	bump   string // "patch" | "minor" | "major"
	rc     bool   // cut a release candidate (vX.Y.Z-rcN) instead of a final release
	ref    string // commit/ref to tag
	dryRun bool
	yes    bool
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		usage()
		return nil
	}
	target := args[0]
	opts := options{bump: "patch", ref: "HEAD"}

	for i := 1; i < len(args); i++ {
		switch args[i] {
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

	if opts.tag != "" && opts.rc {
		return fmt.Errorf("--rc cannot be combined with --tag (set the prerelease in --tag, e.g. --tag v0.0.2-rc1)")
	}

	// Resolve the target component set.
	var names []string
	if target == "all" {
		if opts.tag != "" {
			return fmt.Errorf("--tag cannot be combined with 'all' (each component has its own version)")
		}
		names = componentOrder
	} else {
		if _, ok := components[target]; !ok {
			return fmt.Errorf("unknown component %q; valid: all, %s", target, strings.Join(componentOrder, ", "))
		}
		names = []string{target}
	}

	commit, err := gitOut("rev-parse", "--short", opts.ref)
	if err != nil {
		return fmt.Errorf("resolving ref %q: %w", opts.ref, err)
	}
	branch, _ := gitOut("rev-parse", "--abbrev-ref", "HEAD")

	// Build the plan.
	var plans []plan
	for _, name := range names {
		comp := components[name]
		latest, hasLatest, err := latestTag(comp.prefix)
		if err != nil {
			return err
		}

		var next version
		if opts.tag != "" {
			v, ok := parseVersion(opts.tag)
			if !ok {
				return fmt.Errorf("invalid --tag %q (want vMAJOR.MINOR.PATCH[-pre])", opts.tag)
			}
			next = v
		} else {
			next = nextVersion(latest, hasLatest, opts.bump, opts.rc)
		}

		full := comp.prefix + strings.TrimPrefix(next.String(), "v")
		from := "(none)"
		if hasLatest {
			from = comp.prefix + strings.TrimPrefix(latest.String(), "v")
		}
		plans = append(plans, plan{name, from, full, next.String(), comp.triggers})
	}

	// Show the plan: the version step and what each tag sets in motion.
	fmt.Printf("Tagging commit %s (%s):\n\n", commit, branch)
	for _, p := range plans {
		fmt.Printf("  %-18s %s  ->  %s\n", p.name, p.from, p.fullTag)
		fmt.Printf("  %-18s   ↳ %s\n", "", p.triggers)
	}
	fmt.Println()

	if opts.dryRun {
		fmt.Println("dry-run — would run:")
		for _, p := range plans {
			fmt.Printf("  git tag %s %s && git push origin %s\n", p.fullTag, opts.ref, p.fullTag)
		}
		printBumpHints(plans)
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
	fmt.Println("\nDone — the release workflow will pick these up.")
	printBumpHints(plans)
	return nil
}

// printBumpHints tells the user how to point the repo's consumers at the
// freshly tagged library modules. cmd/release only moves the tag; bumping the
// dependents is a separate, reviewable commit — so we print the exact
// `task bump-deps` command rather than editing go.mod files here.
func printBumpHints(plans []plan) {
	var hints []string
	for _, p := range plans {
		if libraryComponents[p.name] {
			hints = append(hints, fmt.Sprintf("  task bump-deps -- %s %s", p.name, p.ver))
		}
	}
	if len(hints) == 0 {
		return
	}
	fmt.Println("\nNow point the repo's consumers at the released version(s):")
	for _, h := range hints {
		fmt.Println(h)
	}
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
		// Guard against prefix bleed (e.g. "apis/v" must not match a longer
		// path). parseVersion rejects anything that isn't a bare version.
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
		return a.pre < b.pre
	}
}

// nextVersion computes the version to tag from the latest existing one. Release
// candidates (--rc) and the final release share a core version: an rc is a
// prerelease of the release that follows it.
//
//	latest          --rc            release (no --rc)
//	(none)          v0.0.1-rc1      v0.0.1
//	v0.0.1          v0.0.2-rc1      v0.0.2          (core bumped per --patch/minor/major)
//	v0.0.2-rc1      v0.0.2-rc2      v0.0.2          (rc increments; release promotes, same core)
func nextVersion(latest version, hasLatest bool, part string, rc bool) version {
	if !hasLatest {
		if rc {
			return version{0, 0, 1, "rc1"}
		}
		return version{0, 0, 1, ""}
	}
	if latest.pre != "" {
		// Latest is a prerelease: a release promotes it (drop the prerelease,
		// keep the core); another --rc increments the rc counter on the same
		// core. A non-rc prerelease falls through to a fresh rc1.
		if !rc {
			return version{latest.major, latest.minor, latest.patch, ""}
		}
		if n, ok := rcNumber(latest.pre); ok {
			return version{latest.major, latest.minor, latest.patch, fmt.Sprintf("rc%d", n+1)}
		}
		return version{latest.major, latest.minor, latest.patch, "rc1"}
	}
	// Latest is a release: bump the core, optionally starting a new rc series.
	core := bump(latest, part)
	if rc {
		core.pre = "rc1"
	}
	return core
}

// rcNumber extracts N from an "rcN" prerelease; ok is false for any other form.
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

// bump increments part and drops any prerelease, returning the core release
// version (rc handling lives in nextVersion).
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
	fmt.Print(`release — cut release tags for platform-mesh monorepo components

Usage:
  release <component|all> [flags]

Components:
  golang-commons               golang-commons/v<X.Y.Z>               (go-gettable module tag, no image)
  subroutines                  subroutines/v<X.Y.Z>                  (go-gettable module tag, no image)
  apis                         apis/v<X.Y.Z>                         (go-gettable module tag, no image)
  golang-commons               golang-commons/v<X.Y.Z>               (go-gettable module tag + GitHub release, no image)
  subroutines                  subroutines/v<X.Y.Z>                  (go-gettable module tag + GitHub release, no image)
  account-operator             account-operator/v<X.Y.Z>             (signed image + release + chart + SBOM + OCM)
  backup-operator              backup-operator/v<X.Y.Z>              (signed image + release + chart + SBOM + OCM)
  extension-manager-operator   extension-manager-operator/v<X.Y.Z>   (signed image + release + chart + SBOM + OCM)
  kcp-migration-operator       kcp-migration-operator/v<X.Y.Z>       (signed image + release + chart + SBOM + OCM)
  resource-sharding-operator   resource-sharding-operator/v<X.Y.Z>   (signed image + release + chart + SBOM + OCM)
  security-operator            security-operator/v<X.Y.Z>            (signed image + release + chart + SBOM + OCM)
  search-operator              search-operator/v<X.Y.Z>              (signed image + release + chart + SBOM + OCM)
  search-service               search-service/v<X.Y.Z>               (signed image + release + chart + SBOM + OCM)
  terminal-controller-manager  terminal-controller-manager/v<X.Y.Z>  (signed image + release + chart + SBOM + OCM)
  iam-service                  iam-service/v<X.Y.Z>                  (signed image + release + chart + SBOM + OCM)
  rebac-authz-webhook          rebac-authz-webhook/v<X.Y.Z>          (signed image + release + chart + SBOM + OCM)
  terminal-controller-manager  terminal-controller-manager/v<X.Y.Z>  (signed image + release + chart + SBOM + OCM)
  virtual-workspaces           virtual-workspaces/v<X.Y.Z>           (signed image + release + chart + SBOM + OCM)
  all                          every component                       (independent versions)

Flags:
  --tag <vX.Y.Z>   set the exact version (single component only)
  --minor          bump the minor (default: patch)
  --major          bump the major
  --rc             cut a release candidate (vX.Y.Z-rcN); repeat to increment rcN
  --ref <commit>   commit/ref to tag (default: HEAD)
  --dry-run        print the plan, create nothing
  -y, --yes        skip the confirmation prompt

Examples:
  release account-operator            bump account-operator/v* patch and push
  release apis --minor                bump apis/v* minor
  release account-operator --rc       cut the next rc (e.g. v0.0.2-rc1, then -rc2, ...)
  release account-operator --tag v0.0.1
  release all --dry-run
`)
}
