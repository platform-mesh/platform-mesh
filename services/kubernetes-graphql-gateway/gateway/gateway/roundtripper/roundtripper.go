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

package roundtripper

import (
	"net/http"

	"k8s.io/client-go/rest"
)

// unauthorizedRoundTripper always returns 401 Unauthorized responses.
type unauthorizedRoundTripper struct{}

// NewUnauthorizedRoundTripper returns a RoundTripper that always returns 401 Unauthorized.
func NewUnauthorizedRoundTripper() http.RoundTripper {
	return &unauthorizedRoundTripper{}
}

func (u *unauthorizedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Request:    req,
		Body:       http.NoBody,
	}, nil
}

// NewBaseRoundTripper creates a base HTTP transport with only TLS configuration (no authentication).
func NewBaseRoundTripper(tlsConfig rest.TLSClientConfig) (http.RoundTripper, error) {
	return rest.TransportFor(&rest.Config{
		TLSClientConfig: rest.TLSClientConfig{
			Insecure:   tlsConfig.Insecure,
			ServerName: tlsConfig.ServerName,
			CAData:     tlsConfig.CAData,
		},
	})
}
