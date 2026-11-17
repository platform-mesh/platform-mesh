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

package directory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"go.platform-mesh.io/golang-commons/idpdirectory"
	iamconfig "go.platform-mesh.io/iam-service/pkg/config"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

type Querier interface {
	Query(context.Context, idpdirectory.Query) (idpdirectory.Result, error)
}

type Service struct {
	client Querier
}

func New(client Querier) (*Service, error) {
	if client == nil {
		return nil, errors.New("identity directory client is required")
	}
	return &Service{client: client}, nil
}

func NewFromConfig(cfg iamconfig.IdentityDirectoryConfig) (*Service, error) {
	if cfg.Endpoint == "" || cfg.ClientCertFile == "" || cfg.ClientKeyFile == "" || cfg.Timeout <= 0 {
		return nil, errors.New("identity directory endpoint, client certificate/key and positive timeout are required")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load identity directory client certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate},
	}
	if cfg.CAFile != "" {
		caData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read identity directory CA bundle: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificate pool: %w", err)
		}
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(caData) {
			return nil, errors.New("identity directory CA bundle contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	client, err := idpdirectory.NewClient(cfg.Endpoint, tlsConfig, cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("configure identity directory client: %w", err)
	}
	return New(client)
}

func (s *Service) LookupEmail(ctx context.Context, email string) (*graph.User, error) {
	tenant, err := getTenant(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.client.Query(ctx, idpdirectory.Query{
		Tenant: tenant, Operation: idpdirectory.LookupEmail, Email: email,
	})
	if err != nil {
		return nil, err
	}
	if len(result.Users) != 1 {
		return nil, errors.New("identity directory email lookup returned an invalid result")
	}
	if result.Users[0].Email != email {
		return nil, errors.New("identity directory returned a different email address")
	}
	return convert(result.Users[0])
}

func (s *Service) ListUsers(ctx context.Context) ([]*graph.User, error) {
	tenant, err := getTenant(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*graph.User, 0)
	pageToken := ""
	seenTokens := make(map[string]struct{})
	for {
		result, err := s.client.Query(ctx, idpdirectory.Query{
			Tenant: tenant, Operation: idpdirectory.List,
			PageSize: idpdirectory.MaxPageSize, PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, user := range result.Users {
			converted, err := convert(user)
			if err != nil {
				return nil, err
			}
			users = append(users, converted)
		}
		if result.NextPageToken == "" {
			return users, nil
		}
		if _, ok := seenTokens[result.NextPageToken]; ok {
			return nil, errors.New("identity directory repeated a pagination token")
		}
		seenTokens[result.NextPageToken] = struct{}{}
		pageToken = result.NextPageToken
	}
}

func (s *Service) EnrichUserRoles(ctx context.Context, userRoles []*graph.UserRoles) error {
	usersByEmail := make(map[string]*graph.User)
	var usersMu sync.Mutex
	emails := make(map[string]struct{})
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, userRole := range userRoles {
		if userRole == nil || userRole.User == nil || userRole.User.Email == "" {
			continue
		}
		email := userRole.User.Email
		if _, exists := emails[email]; exists {
			continue
		}
		emails[email] = struct{}{}
		group.Go(func() error {
			user, err := s.LookupEmail(ctx, email)
			if errors.Is(err, idpdirectory.ErrNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("lookup directory user for role enrichment: %w", err)
			}
			usersMu.Lock()
			usersByEmail[email] = user
			usersMu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	for _, userRole := range userRoles {
		if userRole == nil || userRole.User == nil {
			continue
		}
		if directoryUser, ok := usersByEmail[userRole.User.Email]; ok {
			userRole.User.UserID = directoryUser.UserID
			userRole.User.FirstName = directoryUser.FirstName
			userRole.User.LastName = directoryUser.LastName
		}
	}
	return nil
}

func getTenant(ctx context.Context) (idpdirectory.Tenant, error) {
	kcpContext, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		return idpdirectory.Tenant{}, fmt.Errorf("get identity tenant from request context: %w", err)
	}
	if kcpContext.IdentityTenant == nil {
		return idpdirectory.Tenant{}, errors.New("identity tenant is unavailable in request context")
	}
	return *kcpContext.IdentityTenant, nil
}

func convert(user idpdirectory.User) (*graph.User, error) {
	if _, err := user.Principal.Username(); err != nil {
		return nil, fmt.Errorf("validate identity directory principal: %w", err)
	}
	result := &graph.User{UserID: user.Email, Email: user.Email}
	if user.GivenName != "" {
		result.FirstName = &user.GivenName
	}
	if user.FamilyName != "" {
		result.LastName = &user.FamilyName
	}
	return result, nil
}
