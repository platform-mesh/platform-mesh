/*
Copyright 2026 The kcp Authors.

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

package server

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"

	genericapiserver "k8s.io/apiserver/pkg/server"
	kubeoptions "k8s.io/kubernetes/pkg/kubeapiserver/options"
)

// Authentication enables anonymous, client certificate, OIDC and request
// header authentication, following the same pattern as kcp's front-proxy.
//
// Its OIDC configuration must match kcp's: the access graph indexes RBAC
// subjects verbatim, so a username that differs by an issuer prefix resolves
// to no workspaces at all.
type Authentication struct {
	BuiltInOptions *kubeoptions.BuiltInAuthenticationOptions
}

// NewAuthentication returns Authentication with the supported methods enabled.
func NewAuthentication() *Authentication {
	return &Authentication{
		BuiltInOptions: kubeoptions.NewBuiltInAuthenticationOptions().
			WithAnonymous().
			WithClientCert().
			WithOIDC().
			WithRequestHeader(),
	}
}

// AddFlags registers the --oidc-*, --authentication-config, --requestheader-*
// and --client-ca-file flags.
func (c *Authentication) AddFlags(fs *pflag.FlagSet) {
	c.BuiltInOptions.AddFlags(fs)
}

// Validate reports configuration errors in the enabled methods.
func (c *Authentication) Validate() []error {
	return c.BuiltInOptions.Validate()
}

// OIDCEnabled reports whether a JWT authenticator is configured.
func (c *Authentication) OIDCEnabled() bool {
	if c.BuiltInOptions == nil {
		return false
	}
	if c.BuiltInOptions.AuthenticationConfigFile != "" {
		return true
	}
	return c.BuiltInOptions.OIDC != nil && c.BuiltInOptions.OIDC.IssuerURL != ""
}

// RequestHeaderEnabled reports whether identity headers from a trusted proxy
// are accepted.
func (c *Authentication) RequestHeaderEnabled() bool {
	return c.BuiltInOptions != nil &&
		c.BuiltInOptions.RequestHeader != nil &&
		c.BuiltInOptions.RequestHeader.ClientCAFile != ""
}

// ApplyTo builds the union authenticator and advertises the client and
// requestheader CAs on the serving side.
//
// BuiltInAuthenticationOptions.ApplyTo is intentionally not called: it expects
// kube-apiserver infrastructure this component does not have.
func (c *Authentication) ApplyTo(
	ctx context.Context,
	authenticationInfo *genericapiserver.AuthenticationInfo,
	servingInfo *genericapiserver.SecureServingInfo,
) error {
	authenticatorConfig, err := c.BuiltInOptions.ToAuthenticationConfig()
	if err != nil {
		return err
	}

	if authenticatorConfig.ClientCAContentProvider != nil {
		if err := authenticationInfo.ApplyClientCert(authenticatorConfig.ClientCAContentProvider, servingInfo); err != nil {
			return fmt.Errorf("unable to load client CA file: %w", err)
		}
	}

	// Advertised in the TLS handshake, not only used for verification: without
	// this the front-proxy sends no client certificate and its identity headers
	// are dropped.
	if authenticatorConfig.RequestHeaderConfig != nil && authenticatorConfig.RequestHeaderConfig.CAContentProvider != nil {
		if err := authenticationInfo.ApplyClientCert(authenticatorConfig.RequestHeaderConfig.CAContentProvider, servingInfo); err != nil {
			return fmt.Errorf("unable to load requestheader CA file: %w", err)
		}
	}

	authenticationInfo.APIAudiences = c.BuiltInOptions.APIAudiences

	authenticationInfo.Authenticator, _, _, _, err = authenticatorConfig.New(ctx)
	if err != nil {
		return fmt.Errorf("build authenticator: %w", err)
	}

	return nil
}
