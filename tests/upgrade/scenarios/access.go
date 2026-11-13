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
	"errors"
	"fmt"
	"strings"

	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// How the steps reach the installation. There are three ways to be someone:
//
//   - adminClient: the kcp admin, who is allowed everything. For seeding and
//     for looking at what is there.
//   - impersonatingClient: the kcp admin acting as a user. kcp skips the
//     login but authorizes the request as the user, so this checks roles
//     and nothing else. It works for users who cannot log in at all.
//   - userClient: the user, logged in to Keycloak, with their token. This is
//     the whole way a request of a user takes.
//
// The clients are the framework's (pm.NewClient), which keeps them: a step
// is called over and over, and gets the same client each time.

// password is the password of every user the scenarios give one.
const password = "MyPass1234"

// stepFunc is what a Step runs. The step files return it from functions of the
// names a scenario uses.
type stepFunc = func(ctx context.Context, env Environment) error

// adminClient is a client for the workspace at path, as the kcp admin.
func adminClient(ctx context.Context, env Environment, path string) (ctrlruntimeclient.Client, error) {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return nil, err
	}
	return tenant.NewClient(kcp, path)
}

// impersonatingClient is a client for the workspace at path, as the kcp
// admin acting as the user with the email.
func impersonatingClient(ctx context.Context, env Environment, path, email string) (ctrlruntimeclient.Client, error) {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return nil, err
	}
	return tenant.NewClient(tenant.As(kcp, email), path)
}

// kubeClient is a client for the cluster Platform Mesh is installed into.
func kubeClient(ctx context.Context, env Environment) (ctrlruntimeclient.Client, error) {
	cfg, err := env.Kube(ctx)
	if err != nil {
		return nil, err
	}
	return pm.NewClient(cfg)
}

// login logs the user in to the realm of the organization and returns their
// ID token, which is what kcp and the services of Platform Mesh take.
func login(ctx context.Context, env Environment, org, email string) (string, error) {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return "", err
	}
	kc, err := env.Keycloak(ctx)
	if err != nil {
		return "", err
	}
	return tenant.Login(ctx, kcp, kc, org, email, password)
}

// userClient is a client for the workspace at path, as the user with the
// email, logged in to the realm of the organization.
func userClient(ctx context.Context, env Environment, org, email, path string) (ctrlruntimeclient.Client, error) {
	token, err := login(ctx, env, org, email)
	if err != nil {
		return nil, err
	}
	kcp, err := env.KCP(ctx)
	if err != nil {
		return nil, err
	}
	return tenant.NewClient(tenant.WithToken(kcp, token), path)
}

// denied turns the result of a request that has to be denied into the
// result of a step: nil when it was forbidden, a terminal error when it was
// allowed. what says who tried what.
func denied(err error, what string) error {
	switch {
	case err == nil:
		return steps.Terminal("%s: allowed, but must not be", what)
	case apierrors.IsForbidden(err):
		return nil
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// deniedSoon is denied for a request that is denied only once a change went
// through: until then it is called again.
func deniedSoon(err error, what string) error {
	err = denied(err, what)
	if errors.Is(err, steps.ErrTerminal) {
		return errors.New(strings.TrimPrefix(err.Error(), steps.ErrTerminal.Error()+": "))
	}
	return err
}
