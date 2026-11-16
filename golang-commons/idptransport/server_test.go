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

package idptransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerRequiresBoundedMutuallyAuthenticatedTLS(t *testing.T) {
	invalid := []*tls.Config{
		nil,
		{MinVersion: tls.VersionTLS12},
		{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert},
		{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: x509.NewCertPool()},
	}
	for _, config := range invalid {
		require.Error(t, Serve(t.Context(), "127.0.0.1:0", time.Second, config, http.NotFoundHandler()))
	}
}

func TestServerStopsWithItsOwningContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Serve(ctx, "127.0.0.1:0", time.Second, &tls.Config{
		MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: x509.NewCertPool(), Certificates: []tls.Certificate{{}},
	}, http.NotFoundHandler())
	require.NoError(t, err)
}
