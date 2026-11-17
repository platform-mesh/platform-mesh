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

package pm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"

	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// clients are the clients NewClient has built, by what they connect to and
// as whom.
var clients sync.Map

// NewClient returns a client for cfg with the scheme of the framework
// (scheme.Platform), for the cluster Platform Mesh is installed into or for
// a workspace of its kcp (see Workspace).
//
// Clients are kept and handed out again for the same address and identity:
// a client discovers the APIs of its server on first use, and a helper that
// is called over and over, like a step, should not start from nothing each
// time. A config with a bearer token is the exception: tokens are short
// lived and every login brings a new one, so there is nothing to gain.
func NewClient(cfg *rest.Config) (ctrlruntimeclient.Client, error) {
	cfg = rest.CopyConfig(cfg)
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Minute
	}
	build := func() (ctrlruntimeclient.Client, error) {
		cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme.Platform()})
		if err != nil {
			return nil, fmt.Errorf("creating client for %s: %w", cfg.Host, err)
		}
		return cl, nil
	}
	if cfg.BearerToken != "" || cfg.BearerTokenFile != "" {
		return build()
	}
	key := clientKey(cfg)
	if cl, ok := clients.Load(key); ok {
		return cl.(ctrlruntimeclient.Client), nil
	}
	cl, err := build()
	if err != nil {
		return nil, err
	}
	kept, _ := clients.LoadOrStore(key, cl)
	return kept.(ctrlruntimeclient.Client), nil
}

// Workspace is the config kcp scoped to the workspace at path. kcp is a
// config without a workspace path, like the one of Installation.KCP.
func Workspace(kcp *rest.Config, path string) *rest.Config {
	cfg := rest.CopyConfig(kcp)
	cfg.Host = strings.TrimSuffix(cfg.Host, "/") + "/clusters/" + path
	return cfg
}

// clientKey tells configs apart by where they connect to and as whom. The
// credentials go in hashed: a new certificate is a new client.
func clientKey(cfg *rest.Config) string {
	hash := sha256.New()
	for _, part := range [][]byte{
		cfg.CertData, cfg.KeyData, cfg.CAData,
		[]byte(cfg.CertFile), []byte(cfg.KeyFile), []byte(cfg.CAFile),
		[]byte(cfg.Username), []byte(cfg.Password),
	} {
		hash.Write(part)
		hash.Write([]byte{0})
	}
	return strings.Join([]string{
		cfg.Host,
		cfg.Impersonate.UserName,
		strings.Join(cfg.Impersonate.Groups, ","),
		hex.EncodeToString(hash.Sum(nil)),
	}, "|")
}
