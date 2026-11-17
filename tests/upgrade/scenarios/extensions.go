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

package scenarios

import (
	"context"
	"fmt"
	"strings"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The extensions scenario: a tenant of the release has extended the portal
// with a ContentConfiguration in the workspace of its account, a navigation
// entry of its own. Platform Mesh validates such a configuration and keeps
// the result in its status, which is what the portal serves.
//
// After the upgrade the configuration is Ready, with its result, and the
// owner reads it the way the portal does, through the gateway. Then, After,
// the owner changes the entry, and the result follows: the upgraded
// operator takes up an object the release processed.
//
// The object is in manifests/extensions-contentconfiguration.yaml.
const (
	extensionsOrg     = "upgrade-ui"
	extensionsOwner   = "owner@upgrade-ui.example.com"
	extensionsAccount = "portal"

	// extensionsManifest holds the ContentConfiguration, of the name
	// extensionsName.
	extensionsManifest = "extensions-contentconfiguration.yaml"
	extensionsName     = "upgrade-seed"
	// extensionsEntry is in the content of the manifest, and so has to be
	// in the result; extensionsLabel is what After changes its label to.
	extensionsEntry = `"pathSegment":"upgrade-seed"`
	extensionsLabel = "Changed after the upgrade"
)

// contentConfigurations is the ContentConfiguration API as the schema of
// the gateway spells it.
var contentConfigurations = pmuiv1alpha1.SchemeGroupVersion.WithKind("ContentConfigurations")

// Extensions is the extensions scenario.
func Extensions() Scenario {
	var (
		org     = tenant.OrganizationPath(extensionsOrg)
		account = tenant.AccountPath(extensionsOrg, extensionsAccount)
	)
	return Scenario{
		Name: "extensions",

		// ORDER MATTERS, as in the tenant scenario; the configuration
		// goes into the workspace of the account, so it comes last.
		Seed: []Step{
			{Name: "organization", Run: seedOrganization(extensionsOrg, extensionsOwner)},
			{Name: "password of the owner", Run: seedPassword(extensionsOrg, extensionsOwner)},
			{Name: "account", Run: seedAccount(org, extensionsAccount, extensionsOwner)},
			{Name: "content configuration", Run: seedContentConfiguration(extensionsOrg, extensionsOwner, account)},
		},

		Validate: []Step{
			{Name: "organization", Run: validateOrganization(extensionsOrg, extensionsOwner)},
			{Name: "account", Run: validateAccount(org, extensionsAccount)},
			{Name: "content configuration", Run: validateContentConfiguration(account, extensionsEntry)},
			{Name: "owner reads it through the gateway", Run: validateGateway(extensionsOrg, extensionsOwner, account, contentConfigurations, extensionsName)},
		},

		// ORDER MATTERS: the second step waits for what the first one
		// asked for.
		After: []Step{
			{Name: "change the content configuration", Run: changeContentConfiguration(extensionsOrg, extensionsOwner, account)},
			{Name: "the result follows", Run: validateContentConfiguration(account, extensionsLabel)},
		},
	}
}

// seedContentConfiguration creates the ContentConfiguration of the manifest
// in the workspace at path, as the user, logged in, and waits for Platform
// Mesh to have processed it.
func seedContentConfiguration(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		if err := create(ctx, cl, extensionsManifest); err != nil {
			return err
		}
		_, err = contentConfigurationResult(ctx, env, path)
		return err
	}
}

// contentConfigurationResult returns the result Platform Mesh keeps for the
// ContentConfiguration in the workspace at path, without whitespace, or an
// error while the configuration is not Ready.
func contentConfigurationResult(ctx context.Context, env Environment, path string) (string, error) {
	cl, err := adminClient(ctx, env, path)
	if err != nil {
		return "", err
	}
	objects, err := existing(ctx, cl, extensionsManifest)
	if err != nil {
		return "", err
	}
	if err := wait.CheckObjectReady(objects[0]); err != nil {
		return "", err
	}
	result, _, _ := unstructured.NestedString(objects[0].Object, "status", "configurationResult")
	if result == "" {
		return "", fmt.Errorf("content configuration %s in %s has no result yet", objects[0].GetName(), path)
	}
	return strings.Join(strings.Fields(result), ""), nil
}

// validateContentConfiguration: the ContentConfiguration in the workspace
// at path is Ready, and its result has the text in it, which is compared
// without whitespace. It is called again until then: the result of a
// change takes a moment.
func validateContentConfiguration(path, text string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		result, err := contentConfigurationResult(ctx, env, path)
		if err != nil {
			return err
		}
		if !strings.Contains(result, strings.Join(strings.Fields(text), "")) {
			return fmt.Errorf("the result of the content configuration in %s lacks %s: %s", path, text, result)
		}
		return nil
	}
}

// changeContentConfiguration changes the label of the navigation entry of
// the ContentConfiguration in the workspace at path, as the user, logged
// in.
func changeContentConfiguration(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		objects, err := existing(ctx, cl, extensionsManifest)
		if err != nil {
			return err
		}
		seeded, err := manifest(extensionsManifest)
		if err != nil {
			return err
		}
		content, _, _ := unstructured.NestedString(seeded[0].Object, "spec", "inlineConfiguration", "content")
		changed := strings.Replace(content, "Seeded by tests/upgrade", extensionsLabel, 1)
		if changed == content {
			return steps.Terminal("the manifest %s has no label to change", extensionsManifest)
		}
		if current, _, _ := unstructured.NestedString(objects[0].Object, "spec", "inlineConfiguration", "content"); current == changed {
			return nil
		}
		if err := unstructured.SetNestedField(objects[0].Object, changed, "spec", "inlineConfiguration", "content"); err != nil {
			return err
		}
		if err := cl.Update(ctx, objects[0]); err != nil {
			return fmt.Errorf("%s updating content configuration %s in %s: %w", email, ctrlruntimeclient.ObjectKeyFromObject(objects[0]), path, err)
		}
		return nil
	}
}
