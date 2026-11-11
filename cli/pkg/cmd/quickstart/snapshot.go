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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"

	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// This file is "pmctl quickstart --dev": it builds the checkout into
// a local registry. No OCM library is involved; every step runs a command,
// and the code here only picks the commands, their order and their inputs.
// By hand, from the root of the checkout, it is roughly:
//
//	# 1. The version of this build.
//	next=$(go run ./hack/release/get-version next)          # v26.11
//	export PM_VERSION=${next#v}.0-snapshot.$(date +%s).g$(git rev-parse --short=7 HEAD)
//	export COMMIT=$(git rev-parse HEAD)
//
//	# 2. A plain-HTTP registry next to the kind node (registry.go).
//	docker run -d --name <cluster>-registry --network kind \
//	  -v <cluster>-registry-data:/var/lib/registry -p 127.0.0.1::5000 registry:2.8.3
//	export OCM_REPO=http://<registry IP>:5000/platform-mesh
//
//	# 3. The image of every component in ocm/components.yaml, several at a time,
//	#    but those of --skip-components; for those, step 5 runs with
//	#    SKIP_COMPONENTS=<name>,... and builds neither their charts nor
//	#    the references to them.
//	task images-<component> REGISTRY=127.0.0.1:<port> REPO=platform-mesh/platform-mesh \
//	  VERSION=v$PM_VERSION PUSH=false PLATFORMS=linux/<arch>
//	docker push 127.0.0.1:<port>/platform-mesh/platform-mesh/<image>:v$PM_VERSION
//
//	# 4. The pinned tools and the release commands of the checkout, a signing
//	#    key, and the ocm configuration that holds it.
//	task tools:ocm tools:helm tools:yq
//	go build -o <state>/bin/ ./hack/release/ocm-build-image ./hack/release/ocm-build-third-party \
//	  ./hack/release/ocm-build-chart ./hack/release/ocm-build-aggregate
//	export OCM_CONFIG=<state>/ocmconfig SIGNATURE=pmctl-quickstart
//	hack/charts/resolve-dependencies.sh
//
//	# 5. The OCM components, in the order a release publishes them. Each
//	#    command fills the ${...} of a component-constructor.yaml from the
//	#    environment, adds it with "ocm add component-versions" and signs it.
//	ocm-build-image <component dir> <state>/sbom           # per image component
//	ocm-build-third-party third_party/<name>               # per third-party wrapper
//	ocm transfer component-version ghcr.io/platform-mesh//<name>:<version> $OCM_REPO
//	                                                       # per component built elsewhere
//	ocm-build-chart charts/<name>                          # per chart: helm push + components
//	ocm-build-aggregate                                    # github.com/platform-mesh/platform-mesh
//	ocm verify component-version --signature $SIGNATURE $OCM_REPO//github.com/platform-mesh/platform-mesh:$PM_VERSION
//
// What comes back is a source for install.go: the two charts in the checkout,
// pointed at the registry, and the certificate of the signing key. <state> is
// stateDir; build.log there has every command as it was run, with its output.

const (
	// imageRepo is the path of the images in the registry, as in a
	// release: <registry>/platform-mesh/platform-mesh/<image>:v<version>.
	imageRepo = "platform-mesh/platform-mesh"
	// publishedOCMRepo holds the components a snapshot refers to but does
	// not build: images from other repositories, pinned wrappers.
	publishedOCMRepo = "ghcr.io/platform-mesh"
	// snapshotSignature names the throwaway signature of a snapshot. It
	// must equal the common name of the signing certificate.
	snapshotSignature = "pmctl-quickstart"
	// aggregateComponent is the root component of Platform Mesh.
	aggregateComponent = "github.com/platform-mesh/platform-mesh"
	// unversioned is the Chart.yaml appVersion of charts whose image is
	// built in this repository; the release commands give them the
	// version of the build.
	unversioned = "0.0.0"
)

// releaseCommands are built from the checkout; they publish the OCM
// components exactly as a release does.
var releaseCommands = []string{"ocm-build-image", "ocm-build-third-party", "ocm-build-chart", "ocm-build-aggregate"}

// snapshot builds the checkout at root into a local registry: the images,
// the charts and the OCM component graph, at a version of its own. It drives
// the checkout's own tooling (task, the commands under hack/release), so the
// result is what a release of that tree would be.
type snapshot struct {
	o *Options
	// root is the checkout.
	root string
	// state holds what the steps hand to each other and what the next run
	// reuses: the signing key, the ocm configuration, tools, logs.
	state string
	// log receives the output of every command run.
	log io.Writer

	version  string
	commit   string
	registry localRegistry
}

// component is an entry of ocm/components.yaml.
type component struct {
	Path     string `json:"path"`
	Image    string `json:"image"`
	Optional bool   `json:"optional"`
}

// stateDir is where the snapshot state of a cluster is kept.
func stateDir(cluster string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "pmctl", "quickstart", cluster), nil
}

// buildSnapshot builds and publishes the checkout and returns what to
// install. The kind cluster must exist: the registry joins its network.
func (o *Options) buildSnapshot(ctx context.Context) (*source, error) {
	state, err := stateDir(o.Name)
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{"bin", "home", "sbom"} {
		if err := os.MkdirAll(filepath.Join(state, dir), 0o750); err != nil {
			return nil, err
		}
	}
	logFile, err := os.Create(filepath.Join(state, "build.log"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = logFile.Close() }()

	// Builds run side by side and share the log.
	log := &lockedWriter{w: logFile}
	if klog.V(1).Enabled() {
		log.w = io.MultiWriter(logFile, o.ErrOut)
	}
	s := &snapshot{o: o, root: o.GitRoot, state: state, log: log}

	if err := s.resolveVersion(ctx); err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(o.ErrOut, "Building Platform Mesh %s from %s ...\n", s.version, s.root)

	err = o.step("Starting registry", func() error {
		if s.registry, err = s.ensureRegistry(ctx); err != nil {
			return err
		}
		return o.configureNodes(s.registry)
	})
	if err != nil {
		return nil, s.withLog(err)
	}

	components, err := s.imageComponents()
	if err != nil {
		return nil, err
	}
	if err := s.buildImages(ctx, components); err != nil {
		return nil, s.withLog(err)
	}
	cert, err := s.publishComponents(ctx, components)
	if err != nil {
		return nil, s.withLog(err)
	}

	return &source{
		version:       s.version,
		prerequisites: filepath.Join(s.root, "charts", prerequisitesChart),
		operator:      filepath.Join(s.root, "charts", operatorChart),
		ocmRepository: s.ocmRepo(),
		// The chart in git has appVersion 0.0.0, so the operator image is
		// named explicitly.
		image: &imageValues{
			Registry:   s.registry.host(),
			Repository: imageRepo + "/platform-mesh-operator",
			Tag:        "v" + s.version,
		},
		signature:   snapshotSignature,
		signingCert: cert,
		details: []string{
			"OCM repository  " + s.ocmRepo(),
			"OCM config      " + s.ocmConfig(),
			"Build log       " + filepath.Join(state, "build.log"),
		},
	}, nil
}

func (s *snapshot) ocmRepo() string   { return s.registry.url + "/platform-mesh" }
func (s *snapshot) ocmConfig() string { return filepath.Join(s.state, "ocmconfig") }
func (s *snapshot) bin() string       { return filepath.Join(s.state, "bin") }

// withLog points an error at the log with the output of the failed command.
func (s *snapshot) withLog(err error) error {
	return fmt.Errorf("%w (output in %s)", err, filepath.Join(s.state, "build.log"))
}

// resolveVersion sets the version of this build:
//
//	<next release>-snapshot.<time>.g<sha>, e.g. 26.11.0-snapshot.1730000000.g1a2b3c4
//
// One version for the images, the charts and the component graph, as in a
// release. "snapshot" sorts above "rc" and "dev", so a snapshot installs over
// the latest release candidate or prerelease as an upgrade (the
// ocm-k8s-toolkit denies downgrades), and below the final release. The time
// makes every build a version of its own: published versions are immutable,
// and the tree may have changed without a commit.
func (s *snapshot) resolveVersion(ctx context.Context) error {
	// The checkout need not be where pmctl runs.
	ctx = releaseutils.WithGitDir(ctx, s.root)

	version, err := pmversions.Next(ctx)
	if err != nil {
		return fmt.Errorf("deriving the next release from %s: %w", s.root, err)
	}
	commit, err := releaseutils.Git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	s.commit = strings.TrimSpace(commit)

	version.Pre = fmt.Sprintf("snapshot.%d.g%s", time.Now().Unix(), s.commit[:7])
	// Charts and components carry the version without the v of the tag.
	s.version = strings.TrimPrefix(version.String(), "v")
	return nil
}

// imageComponents returns the components of ocm/components.yaml that have an
// image, by name, without the ones SkipComponents leaves out.
func (s *snapshot) imageComponents() (map[string]component, error) {
	path := filepath.Join(s.root, "ocm", "components.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Components map[string]component `json:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, name := range s.o.SkipComponents {
		c, ok := doc.Components[name]
		switch {
		case !ok:
			return nil, fmt.Errorf("--skip-components: %q is not a component in %s", name, path)
		case !c.Optional:
			return nil, fmt.Errorf("--skip-components: %q is not optional in %s and cannot be left out", name, path)
		}
		delete(doc.Components, name)
	}
	for name, c := range doc.Components {
		if c.Image == "" {
			delete(doc.Components, name)
		}
	}
	if len(doc.Components) == 0 {
		return nil, fmt.Errorf("no image components in %s", path)
	}
	return doc.Components, nil
}

// extraImages returns the further images of a component, one per
// <path>/images/<name>/Dockerfile, by name with the build context.
func (s *snapshot) extraImages(c component) (map[string]string, error) {
	dockerfiles, err := filepath.Glob(filepath.Join(s.root, c.Path, "images", "*", "Dockerfile"))
	if err != nil {
		return nil, err
	}
	images := make(map[string]string, len(dockerfiles))
	for _, dockerfile := range dockerfiles {
		dir := filepath.Dir(dockerfile)
		images[filepath.Base(dir)] = dir
	}
	return images, nil
}

func (s *snapshot) imageRef(image string) string {
	return fmt.Sprintf("%s/%s/%s:v%s", s.registry.pushHost, imageRepo, image, s.version)
}

// buildImages builds the image of every component the way the release does
// (task images-<component>), for the architecture of the container runtime,
// and pushes it to the registry.
func (s *snapshot) buildImages(ctx context.Context, components map[string]component) error {
	arch, err := s.output(ctx, "", nil, "docker", "version", "-f", "{{.Server.Arch}}")
	if err != nil {
		return fmt.Errorf("asking docker for its architecture: %w", err)
	}
	platform := "linux/" + arch

	// One step for all of them: they build side by side, and each is
	// reported as it finishes.
	return s.o.step(fmt.Sprintf("Building %d images, %d at a time", len(components), s.o.ParallelBuilds), func() error {
		group, ctx := errgroup.WithContext(ctx)
		group.SetLimit(s.o.ParallelBuilds)
		for _, name := range sortedKeys(components) {
			group.Go(func() error {
				// Not started once another build failed.
				if err := ctx.Err(); err != nil {
					return err
				}
				t := s.o.begin("Building image " + name)
				err := s.buildImage(ctx, t, name, components[name], platform)
				t.end(err)
				return err
			})
		}
		return group.Wait()
	})
}

// buildImage builds and pushes the image of a component, and its further images.
func (s *snapshot) buildImage(ctx context.Context, t *task, name string, c component, platform string) error {
	err := s.runTo(ctx, t, s.root, nil, "task", "images-"+name,
		"REGISTRY="+s.registry.pushHost, "REPO="+imageRepo, "VERSION=v"+s.version,
		"PUSH=false", "PLATFORMS="+platform)
	if err != nil {
		return err
	}
	refs := []string{s.imageRef(c.Image)}

	extra, err := s.extraImages(c)
	if err != nil {
		return err
	}
	for _, image := range sortedKeys(extra) {
		ref := s.imageRef(image)
		if err := s.runTo(ctx, t, s.root, nil, "docker", "buildx", "build", "--platform", platform, "--load", "--tag", ref, extra[image]); err != nil {
			return err
		}
		refs = append(refs, ref)
	}

	for _, ref := range refs {
		_, _ = fmt.Fprintf(t, "pushing %s\n", ref)
		if err := s.run(ctx, "", nil, "docker", "push", "-q", ref); err != nil {
			return err
		}
	}
	return nil
}

// publishComponents publishes the OCM component graph of the snapshot with
// the release commands, in the order the release runs them: an image
// component per image, the third-party wrappers, the charts and their
// components, and the aggregate. Everything is signed with a throwaway key;
// the certificate to verify it with is returned.
func (s *snapshot) publishComponents(ctx context.Context, components map[string]component) ([]byte, error) {
	var cert []byte
	err := s.o.step("Preparing release tooling", func() error {
		var err error
		if cert, err = s.prepareTooling(ctx); err != nil {
			return err
		}
		// ocm-build-chart resolves chart dependencies only when a chart
		// has none on disk; the two charts installed from the checkout
		// need theirs either way.
		return s.runTool(ctx, filepath.Join(s.root, "hack", "charts", "resolve-dependencies.sh"))
	})
	if err != nil {
		return nil, err
	}

	err = s.o.step("Publishing image components", func() error {
		for _, name := range sortedKeys(components) {
			c := components[name]
			extra, err := s.extraImages(c)
			if err != nil {
				return err
			}
			for _, image := range append([]string{c.Image}, sortedKeys(extra)...) {
				if err := s.placeholderSBOMs(image); err != nil {
					return err
				}
			}
			if err := s.runTool(ctx, "ocm-build-image", c.Path, filepath.Join(s.state, "sbom")); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = s.o.step("Publishing third-party components", func() error {
		constructors, err := filepath.Glob(filepath.Join(s.root, "third_party", "*", "component-constructor.yaml"))
		if err != nil {
			return err
		}
		for _, constructor := range constructors {
			dir := filepath.Join("third_party", filepath.Base(filepath.Dir(constructor)))
			if err := s.runTool(ctx, "ocm-build-third-party", dir); err != nil {
				return err
			}
		}
		return s.copyReferencedComponents(ctx)
	})
	if err != nil {
		return nil, err
	}

	err = s.o.step("Publishing charts", func() error {
		charts, err := filepath.Glob(filepath.Join(s.root, "charts", "*", "Chart.yaml"))
		if err != nil {
			return err
		}
		for _, chart := range charts {
			dir := filepath.Join("charts", filepath.Base(filepath.Dir(chart)))
			if err := s.runTool(ctx, "ocm-build-chart", dir); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = s.o.step("Publishing Platform Mesh component", func() error {
		if err := s.runTool(ctx, "ocm-build-aggregate"); err != nil {
			return err
		}
		return s.runTool(ctx, "ocm", "verify", "component-version", "--signature", snapshotSignature, s.ocmRepo()+"//"+aggregateComponent+":"+s.version)
	})
	return cert, err
}

// prepareTooling puts the pinned ocm, helm and yq of the checkout and its
// release commands into the state directory, and writes the signing key and
// the ocm configuration. It returns the signing certificate.
func (s *snapshot) prepareTooling(ctx context.Context) ([]byte, error) {
	for _, tool := range []string{"ocm", "helm", "yq"} {
		// The task installs the version pinned in tools/Taskfile.yaml
		// and prints where.
		path, err := s.output(ctx, s.root, []string{"UGET_PRINT_PATH=absolute"}, "task", "tools:"+tool)
		if err != nil {
			return nil, fmt.Errorf("installing %s: %w", tool, err)
		}
		// The release commands call the tools by their plain names.
		link := filepath.Join(s.bin(), tool)
		if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err := os.Symlink(path, link); err != nil {
			return nil, err
		}
	}

	args := []string{"build", "-o", s.bin() + string(filepath.Separator)}
	for _, command := range releaseCommands {
		args = append(args, "./hack/release/"+command)
	}
	if err := s.run(ctx, s.root, nil, "go", args...); err != nil {
		return nil, fmt.Errorf("building the release commands: %w", err)
	}

	key, cert, err := s.signingKey()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.ocmConfig(), ocmConfig(key, cert, os.Getenv("GITHUB_TOKEN")), 0o600); err != nil {
		return nil, err
	}
	return cert, nil
}

// signingKey returns the key the snapshots of this cluster are signed with
// and its certificate, both PEM. They are created once and kept, so that the
// certificate in the cluster verifies later builds too.
func (s *snapshot) signingKey() (key, cert []byte, err error) {
	keyFile := filepath.Join(s.state, "signing.key")
	certFile := filepath.Join(s.state, "signing.crt")

	key, keyErr := os.ReadFile(keyFile)
	cert, certErr := os.ReadFile(certFile)
	if keyErr == nil && certErr == nil {
		return key, cert, nil
	}

	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: snapshotSignature},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		return nil, nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, nil, err
	}
	key = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	cert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	if err := os.WriteFile(keyFile, key, 0o600); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(certFile, cert, 0o644); err != nil { //nolint:gosec // a public certificate
		return nil, nil, err
	}
	return key, cert, nil
}

// ocmConfig renders the configuration of the ocm CLI: the signing key, and
// credentials for the published repository when there is a token (anonymous
// access works, with lower rate limits). hack/release/ocm-config.sh writes
// the same for the release.
func ocmConfig(key, cert []byte, token string) []byte {
	credentials := func(identity, properties map[string]any) map[string]any {
		return map[string]any{
			"type": "credentials.config.ocm.software",
			"consumers": []any{map[string]any{
				"identity":    identity,
				"credentials": []any{properties},
			}},
		}
	}

	configurations := []any{
		credentials(
			map[string]any{"type": "RSA/v1alpha1", "algorithm": "RSASSA-PSS", "signature": snapshotSignature},
			map[string]any{"type": "Credentials/v1", "properties": map[string]any{
				"private_key_pem": string(key),
				"public_key_pem":  string(cert),
			}},
		),
	}
	if token != "" {
		host, _, _ := strings.Cut(publishedOCMRepo, "/")
		configurations = append(configurations, credentials(
			map[string]any{"type": "OCIRegistry", "scheme": "https", "hostname": host},
			map[string]any{"type": "Credentials", "properties": map[string]any{"username": "github", "password": token}},
		))
	}

	out, err := yaml.Marshal(map[string]any{
		"type":           "generic.config.ocm.software/v1",
		"configurations": configurations,
	})
	if err != nil {
		// Maps of strings always marshal.
		panic(err)
	}
	return out
}

// placeholderSBOMs writes the SBOM files the image constructors attach. The
// release generates them with syft; a snapshot is not scanned, the files only
// have to exist.
func (s *snapshot) placeholderSBOMs(image string) error {
	dir := filepath.Join(s.state, "sbom", image)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	cyclonedx := fmt.Sprintf(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"type":"container","name":%q}},"components":[]}`+"\n", image)
	spdx := fmt.Sprintf(`{"spdxVersion":"SPDX-2.3","dataLicense":"CC0-1.0","SPDXID":"SPDXRef-DOCUMENT","name":%q,"documentNamespace":"https://platform-mesh.io/sbom/placeholder/%s","creationInfo":{"created":%q,"creators":["Tool: pmctl quickstart"]},"packages":[]}`+"\n",
		image, image, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "sbom-cyclonedx.json"), []byte(cyclonedx), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sbom-spdx.json"), []byte(spdx), 0o600)
}

// copyReferencedComponents copies every component a chart refers to at a
// version other than the snapshot's from the published repository: those are
// not built here (images from other repositories, pinned wrappers). The
// registry keeps them, so each is copied once.
func (s *snapshot) copyReferencedComponents(ctx context.Context) error {
	constructors, err := filepath.Glob(filepath.Join(s.root, "charts", "*", "component-constructor.yaml"))
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, constructor := range constructors {
		var doc struct {
			Components []struct {
				ComponentReferences []struct {
					ComponentName string `json:"componentName"`
					Version       string `json:"version"`
				} `json:"componentReferences"`
			} `json:"components"`
		}
		if err := readYAML(constructor, &doc); err != nil {
			return err
		}
		var chart struct {
			AppVersion string `json:"appVersion"`
		}
		if err := readYAML(filepath.Join(filepath.Dir(constructor), "Chart.yaml"), &chart); err != nil {
			return err
		}
		// As ocm-build-chart resolves it.
		appVersion := chart.AppVersion
		if appVersion == unversioned {
			appVersion = "v" + s.version
		}

		for _, c := range doc.Components {
			for _, ref := range c.ComponentReferences {
				version := strings.NewReplacer("${PM_VERSION}", s.version, "${APP_VERSION}", appVersion).Replace(ref.Version)
				cv := ref.ComponentName + ":" + version
				if version == s.version || done[cv] {
					continue
				}
				done[cv] = true
				if s.runTool(ctx, "ocm", "get", "component-version", s.ocmRepo()+"//"+cv, "-o", "json") == nil {
					continue
				}
				if err := s.runTool(ctx, "ocm", "transfer", "component-version", publishedOCMRepo+"//"+cv, s.ocmRepo()); err != nil {
					return fmt.Errorf("copying %s from %s: %w", cv, publishedOCMRepo, err)
				}
			}
		}
	}
	return nil
}

func readYAML(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

// runTool runs a release command, or one of the tools they use, from the
// checkout with the environment the release commands read their inputs from.
//
// HOME is a directory of its own: ocm consults the Docker credential helpers
// of $HOME/.docker/config.json for every registry it resolves a digest from,
// and on macOS the keychain helper then prompts. Credentials come from the
// ocm configuration only.
func (s *snapshot) runTool(ctx context.Context, name string, args ...string) error {
	env := []string{
		"PATH=" + s.bin() + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + filepath.Join(s.state, "home"),
		"OCM_CONFIG=" + s.ocmConfig(),
		"PM_VERSION=" + s.version,
		"OCM_REPO=" + s.ocmRepo(),
		"SIGNATURE=" + snapshotSignature,
		"COMMIT=" + s.commit,
		// The release commands leave out the charts and the references of
		// the skipped components, as the images are not built.
		"SKIP_COMPONENTS=" + strings.Join(s.o.SkipComponents, ","),
	}
	// exec resolves the command in the PATH of this process, not env's.
	if !strings.ContainsRune(name, filepath.Separator) {
		name = filepath.Join(s.bin(), name)
	}
	return s.run(ctx, s.root, env, name, args...)
}

// run runs a command, with its output going to the log.
func (s *snapshot) run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	return s.runTo(ctx, io.Discard, dir, env, name, args...)
}

// runTo runs a command, with its output going to the log and to out.
func (s *snapshot) runTo(ctx context.Context, out io.Writer, dir string, env []string, name string, args ...string) error {
	_, _ = fmt.Fprintf(s.log, "\n$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = io.MultiWriter(s.log, out)
	cmd.Stderr = cmd.Stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", filepath.Base(name), strings.Join(args, " "), err)
	}
	return nil
}

// output runs a command and returns what it prints, trimmed; what it writes
// to stderr goes to the log.
func (s *snapshot) output(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	_, _ = fmt.Fprintf(s.log, "\n$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = s.log
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", filepath.Base(name), strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// lockedWriter is a Writer for several goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// runQuiet runs a command and drops its output.
func runQuiet(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
