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

package oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type kubernetesTokenSource struct {
	k8s                *http.Client
	audiences          []string
	namespace          string
	serviceAccountName string
	host               string
	tokenExpiration    int64
}

// make sure to implement the interface during compiletime
var _ oauth2.TokenSource = (*kubernetesTokenSource)(nil)

type KubernetesTokenSourceConfig struct {

	// Audiences which should be applied to the generated token.
	Audiences []string

	// ServiceAccount identifies the service account which shall be impersonated when creating the TokenRequest
	ServiceAccount types.NamespacedName

	// Determines how long the credential generated via the TokenRequest API is valid for.
	// Needs to be at least 10 mins. It defaults to 1h.
	TokenExpiration time.Duration
}

var (
	ErrTokenExpirationOutOfRange = errors.New("the KubernetesTokenSourceConfig.TokenExpiration needs to be at least 10m")
	ErrServiceAccountMissing     = errors.New("make sure the ServiceAccount property is set")
)

// Generates a new KubernetesTokenSource which can be used in combination with the golang.org/x/oauth2 package as it implements
// the https://pkg.go.dev/golang.org/x/oauth2#TokenSource interface.
//
// It will take care of automatically refreshing the credential whenever needed, so that the client that consumes this TokenSource
// has always an up to date credential available.
func NewKubernetesTokenSource(cfg *rest.Config, opts *KubernetesTokenSourceConfig) (*kubernetesTokenSource, error) {

	if opts.ServiceAccount.Name == "" || opts.ServiceAccount.Namespace == "" {
		return nil, ErrServiceAccountMissing
	}

	if opts.TokenExpiration == 0 {
		opts.TokenExpiration = 1 * time.Hour
	}

	if opts.TokenExpiration < 10*time.Minute {
		return nil, ErrTokenExpirationOutOfRange
	}

	authenticatedClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}

	return &kubernetesTokenSource{
		k8s:                authenticatedClient,
		audiences:          opts.Audiences,
		namespace:          opts.ServiceAccount.Namespace,
		serviceAccountName: opts.ServiceAccount.Name,
		host:               cfg.Host,
		tokenExpiration:    int64(opts.TokenExpiration.Seconds()),
	}, nil
}

func (k *kubernetesTokenSource) Token() (*oauth2.Token, error) {
	var body bytes.Buffer
	err := json.NewEncoder(&body).Encode(authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         k.audiences,
			ExpirationSeconds: &k.tokenExpiration,
		},
	})
	if err != nil {
		return nil, err
	}

	res, err := k.k8s.Post(
		fmt.Sprintf("%s/api/v1/namespaces/%s/serviceaccounts/%s/token", k.host, k.namespace, k.serviceAccountName),
		"application/json",
		&body,
	)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			fmt.Printf("error closing response body: %v\n", err)
		}
	}()

	var token authenticationv1.TokenRequest
	err = json.NewDecoder(res.Body).Decode(&token)
	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: token.Status.Token,
		Expiry:      token.Status.ExpirationTimestamp.Time,
	}, nil
}
