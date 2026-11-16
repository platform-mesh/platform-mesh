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
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

func Serve(ctx context.Context, address string, timeout time.Duration, config *tls.Config, handler http.Handler) error {
	if address == "" || timeout <= 0 || config == nil || handler == nil ||
		config.ClientAuth != tls.RequireAndVerifyClientCert || config.ClientCAs == nil || len(config.Certificates) == 0 ||
		config.MinVersion < tls.VersionTLS12 {
		return errors.New("identity endpoint requires a listen address, bounded handler, certificate and mutual TLS configuration")
	}
	server := &http.Server{
		Handler: http.TimeoutHandler(handler, timeout, `{"code":"unavailable"}`), ReadHeaderTimeout: timeout, ReadTimeout: timeout,
		WriteTimeout: timeout, IdleTimeout: timeout, TLSConfig: config,
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close() //nolint:errcheck
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	slog.InfoContext(ctx, "identity endpoint listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return errors.Join(err, server.Close())
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func CertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("CA bundle contains no certificates")
	}
	return pool, nil
}
