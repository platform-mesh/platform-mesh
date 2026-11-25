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
	"net"
	"strconv"

	iamapp "go.platform-mesh.io/iam-service/pkg/app"
	iamconfig "go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/idm"
)

// IAMServiceName is the name of the IAMService component.
const IAMServiceName = "iam-service"

// IAMService is the GraphQL API behind the members and roles pages: who has
// which role on an account, kept as tuples in OpenFGA. Browsers reach it
// through Ingress, at /iam/graphql of an organization's host.
//
// It runs without a user directory: there is no Keycloak to look people up
// in, so a user is the e-mail address of their token and giving a role to
// an address nobody has logged in with yet simply takes effect when they do.
func IAMService() Component {
	return Component{
		Name:     IAMServiceName,
		Bindings: []string{coreExport},
		Run: func(ctx context.Context, env Env) error {
			if env.OpenFGA == nil {
				return errors.New("needs OpenFGA")
			}
			log, err := newLogger(IAMServiceName, env.LogLevel)
			if err != nil {
				return err
			}
			svc := iamconfig.NewServiceConfig()
			svc.KCPWorkspace = env.Platform.System.String()
			svc.ListenAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(env.Ports.IAMService))
			svc.OpenFGA.GRPCAddr = env.OpenFGA.Addr
			svc.IDM.Provider = idm.ProviderNone
			// kcp names users by this claim too.
			svc.JWT.UserIDClaim = "email"
			// The roles built into the service.
			svc.Roles.FilePath = ""
			return iamapp.Run(ctx, env.Config, iamapp.Config{Common: quietCommon(), Service: svc}, iamapp.Deps{Logger: log, OpenFGA: env.OpenFGA.Client})
		},
	}
}
