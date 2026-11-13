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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
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

const (
	// password is the password of every user the scenarios give one.
	password = "MyPass1234"

	// kubectlClient is the public OIDC client every organization has for
	// kubectl, and kubectlRedirect a redirect URI it accepts. The steps log
	// in through it, like `kubectl oidc-login` does.
	kubectlClient   = "kubectl"
	kubectlRedirect = "http://localhost:8000"

	// accountInfo is the name of the AccountInfo every workspace of an
	// organization or account has.
	accountInfo = "account"
)

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

// login logs the user in to the realm of the organization and returns their
// ID token, which is what kcp and the services of Platform Mesh take.
func login(ctx context.Context, env Environment, org, email string) (string, error) {
	admin, err := adminClient(ctx, env, tenant.OrganizationPath(org))
	if err != nil {
		return "", err
	}
	info := &pmcorev1alpha1.AccountInfo{}
	if err := admin.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountInfo}, info); err != nil {
		return "", fmt.Errorf("the AccountInfo of organization %s: %w", org, err)
	}
	if info.Spec.OIDC == nil || info.Spec.OIDC.Clients[kubectlClient].ClientID == "" {
		return "", fmt.Errorf("the AccountInfo of organization %s names no %s client", org, kubectlClient)
	}
	kc, err := env.Keycloak(ctx)
	if err != nil {
		return "", err
	}
	tokens, err := kc.Login(ctx, org, info.Spec.OIDC.Clients[kubectlClient].ClientID, kubectlRedirect, email, password)
	if err != nil {
		return "", err
	}
	return tokens.IDToken, nil
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

// gone turns the result of a Get into the result of a step that waits for
// the object to be deleted.
func gone(err error, what string) error {
	switch {
	case err == nil:
		return fmt.Errorf("%s is still there", what)
	case apierrors.IsNotFound(err):
		return nil
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// graphQL sends a query to a GraphQL service of the organization, served at
// path under the address of the organization, as the user with the token,
// and decodes the data of the answer into out.
func graphQL(ctx context.Context, env Environment, org, path, token, query string, out any) error {
	portal, client, err := env.Portal(ctx)
	if err != nil {
		return err
	}
	target, err := url.Parse(portal)
	if err != nil {
		return fmt.Errorf("the address of the portal: %w", err)
	}
	target.Host = org + "." + target.Host
	target.Path = path

	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s: %s", target, res.Status, strings.TrimSpace(string(data)))
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return fmt.Errorf("POST %s: decoding the answer: %w", target, err)
	}
	if len(answer.Errors) != 0 {
		messages := make([]string, 0, len(answer.Errors))
		for _, e := range answer.Errors {
			messages = append(messages, e.Message)
		}
		return fmt.Errorf("POST %s: %s", target, strings.Join(messages, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(answer.Data, out)
}
