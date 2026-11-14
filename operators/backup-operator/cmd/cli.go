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

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/backup-operator/pkg/crypto"
	"go.platform-mesh.io/backup-operator/pkg/storage"

	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// cliConfig are the flags of the backup and restore commands.
type cliConfig struct {
	Context      string
	Namespace    string
	Name         string
	Bucket       string
	Prefix       string
	Endpoint     string
	Region       string
	PathStyle    bool
	KeyFile      string
	EtcdEndpoint string
}

func (c *cliConfig) addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Context, "context", "", "kubeconfig context of the runtime cluster, default is the current context")
	fs.StringVar(&c.Namespace, "namespace", "platform-mesh-system", "namespace of kcp and the etcd clusters")
	fs.StringVar(&c.Name, "name", "", "name of the backup")
	fs.StringVar(&c.Bucket, "bucket", "", "S3 bucket of the archive")
	fs.StringVar(&c.Prefix, "prefix", "", "key prefix of the archive in the bucket")
	fs.StringVar(&c.Endpoint, "endpoint", "", "S3 endpoint, for example http://localhost:4566, empty is AWS S3")
	fs.StringVar(&c.Region, "region", "us-east-1", "S3 region")
	fs.BoolVar(&c.PathStyle, "path-style", false, "use path style S3 URLs, needed by most S3 compatible servers")
	fs.StringVar(&c.KeyFile, "key-file", "", "file with the 32 byte encryption key")
	fs.StringVar(&c.EtcdEndpoint, "etcd-store-endpoint", "", "endpoint of the druid stores, when the one in the Etcd is not reachable from here")
}

func (c *cliConfig) check() error {
	var missing []string
	for flag, v := range map[string]string{"name": c.Name, "bucket": c.Bucket, "key-file": c.KeyFile} {
		if v == "" {
			missing = append(missing, "--"+flag)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing flags: %v", missing)
	}
	return nil
}

func (c *cliConfig) client() (ctrlruntimeclient.Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = defaultCfg.Kubeconfig
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.Context}).ClientConfig()
	if err != nil {
		return nil, err
	}
	return ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme})
}

// opened returns the bucket of the archive. The credentials are read from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
func (c *cliConfig) opened() (*storage.Opened, error) {
	key, err := os.ReadFile(c.KeyFile)
	if err != nil {
		return nil, err
	}
	key = bytes.TrimRight(key, "\r\n")
	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("key file %s: key must be %d bytes, got %d", c.KeyFile, crypto.KeySize, len(key))
	}
	id, secret := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	if id == "" || secret == "" {
		return nil, errors.New("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
	}
	s, err := storage.NewS3(storage.S3Config{
		Endpoint:        c.Endpoint,
		Region:          c.Region,
		Bucket:          c.Bucket,
		AccessKeyID:     id,
		SecretAccessKey: secret,
		ForcePathStyle:  c.PathStyle,
	})
	if err != nil {
		return nil, err
	}
	return &storage.Opened{Store: s, Prefix: c.Prefix, Key: key}, nil
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}
