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

package components

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// IngressName is the name of the Ingress component.
const IngressName = "ingress"

// Ports are the localhost ports of what Ingress routes to. They are picked
// by the process: nobody but Ingress talks to them.
type Ports struct {
	Portal        int
	IAMUI         int
	MarketplaceUI int
	IAMService    int
}

// route sends the requests whose path starts with prefix to a backend on
// localhost, optionally without the prefix.
type route struct {
	prefix string
	port   int
	strip  bool
}

// Ingress is the one address a browser talks to, http://localhost:<portal
// port> and http://<org>.localhost:<portal port>: it routes by path to the
// portal, the UIs and the APIs behind it, as the Gateway of a cluster does
// with its HTTPRoutes. The pages and the APIs they call share an origin
// that way, as they do in a cluster.
func Ingress() Component {
	return Component{
		Name: IngressName,
		Run: func(ctx context.Context, env Env) error {
			routes := []route{
				{prefix: "/gateway/", port: env.GraphQLPort, strip: true},
				{prefix: "/iam/", port: env.Ports.IAMService, strip: true},
				{prefix: "/ui/iam/", port: env.Ports.IAMUI},
				{prefix: "/ui/marketplace/", port: env.Ports.MarketplaceUI},
			}
			portal := proxyTo(env.Ports.Portal, "")
			mux := http.NewServeMux()
			for _, r := range routes {
				strip := ""
				if r.strip {
					strip = strings.TrimSuffix(r.prefix, "/")
				}
				mux.Handle(r.prefix, proxyTo(r.port, strip))
			}
			mux.Handle("/", portal)

			srv := &http.Server{
				Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(env.PortalPort)),
				Handler:           mux,
				ReadHeaderTimeout: 30 * time.Second,
			}
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return fmt.Errorf("--portal-port: %w", err)
			}
			go func() {
				<-ctx.Done()
				_ = srv.Close()
			}()
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
}

// proxyTo forwards to a localhost port, dropping stripPrefix from the path
// and keeping the Host header: the backends take the organization from it.
func proxyTo(port int, stripPrefix string) http.Handler {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			if stripPrefix != "" {
				r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, stripPrefix)
				r.Out.URL.RawPath = ""
			}
			r.SetXForwarded()
		},
		// GraphQL subscriptions are server-sent events.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "pm: the component serving this path is not running: "+err.Error(), http.StatusBadGateway)
		},
	}
}

// PublicURL is the URL of path for the organization the placeholders stand
// for; the portal fills them in per request.
func PublicURL(port int, path string) string {
	return fmt.Sprintf("http://${org-subdomain}%s:%d%s", PortalBaseDomain, port, path)
}
