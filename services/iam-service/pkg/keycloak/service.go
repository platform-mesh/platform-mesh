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

package keycloak

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/sync/errgroup"
	"k8s.io/utils/ptr"

	"go.platform-mesh.io/iam-service/pkg/cache"
	"go.platform-mesh.io/iam-service/pkg/config"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/graph"
	keycloakClient "go.platform-mesh.io/iam-service/pkg/keycloak/client"
	"go.platform-mesh.io/iam-service/pkg/metrics"
)

// sanitizeEmail returns a sanitized version of the email for logging (first 3 chars + ***)
// to avoid logging PII information
func sanitizeEmail(email string) string {
	if len(email) <= 3 {
		return email
	}
	return email[:3] + "***"
}

type Service struct {
	cfg            *config.ServiceConfig
	keycloakClient KeycloakClientInterface
	userCache      *cache.UserCache
}

func New(ctx context.Context, cfg *config.ServiceConfig) (*Service, error) {
	log := logger.LoadLoggerFromContext(ctx)
	issuer := fmt.Sprintf("%s/realms/master", cfg.Keycloak.BaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OIDC provider for issuer %s", issuer)
	}

	cCfg := clientcredentials.Config{
		ClientID:     cfg.Keycloak.ClientID,
		ClientSecret: cfg.Keycloak.ClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
	}
	httpClient := cCfg.Client(ctx)

	// Create Keycloak client with the authenticated HTTP client
	kcClient, err := keycloakClient.NewClientWithResponses(
		cfg.Keycloak.BaseURL,
		keycloakClient.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Keycloak client: %w", err)
	}

	// Initialize cache if enabled
	var userCache *cache.UserCache
	if cfg.Keycloak.Cache.Enabled {
		userCache = cache.NewUserCache(cfg.Keycloak.Cache.TTL)
		log.Info().Dur("ttl", cfg.Keycloak.Cache.TTL).Msg("Keycloak user cache enabled")
	} else {
		log.Info().Msg("Keycloak user cache disabled")
	}

	return &Service{
		cfg:            cfg,
		keycloakClient: kcClient,
		userCache:      userCache,
	}, nil
}

func (s *Service) UserByMail(ctx context.Context, userID string) (*graph.User, error) {
	start := time.Now()
	defer func() {
		metrics.KeycloakDuration.WithLabelValues("user_by_mail").Observe(time.Since(start).Seconds())
	}()

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("user_by_mail", "error").Inc()
		return nil, errors.Wrap(err, "failed to get KCP user context")
	}

	realm := kctx.IDMTenant

	// Try cache first if enabled
	if s.userCache != nil {
		if cachedUser := s.userCache.Get(realm, userID); cachedUser != nil {
			return cachedUser, nil
		}
	}

	// Cache miss - fetch from Keycloak
	user, err := s.fetchUserFromKeycloak(ctx, realm, userID)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("user_by_mail", "error").Inc()
		return nil, errors.Wrap(err, "failed to fetch user from Keycloak for email %s", userID)
	}

	// Store in cache if user found and cache enabled
	if user != nil && s.userCache != nil {
		s.userCache.Set(realm, userID, user)
	}

	metrics.KeycloakRequests.WithLabelValues("user_by_mail", "success").Inc()
	return user, nil
}

func (s *Service) GetUsers(ctx context.Context) ([]*graph.User, error) {
	log := logger.LoadLoggerFromContext(ctx)

	start := time.Now()
	defer func() {
		metrics.KeycloakDuration.WithLabelValues("get_users").Observe(time.Since(start).Seconds())
	}()

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("get_users", "error").Inc()
		return nil, errors.Wrap(err, "failed to get KCP user context")
	}

	realm := kctx.IDMTenant

	log.Debug().
		Str("realm", realm).
		Msg("Fetching all users from Keycloak")

	// Fetch all users with pagination and caching
	users, err := s.fetchAllUsers(ctx, realm)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("get_users", "error").Inc()
		return nil, errors.Wrap(err, "failed to fetch all users from Keycloak for realm %s", realm)
	}

	log.Debug().
		Int("user_count", len(users)).
		Str("realm", realm).
		Msg("Successfully fetched all users from Keycloak")

	metrics.KeycloakRequests.WithLabelValues("get_users", "success").Inc()
	return users, nil
}

// fetchUserFromKeycloak fetches a single user from Keycloak by email
func (s *Service) fetchUserFromKeycloak(ctx context.Context, realm, email string) (*graph.User, error) {
	log := logger.LoadLoggerFromContext(ctx)

	// Configure search parameters
	params := &keycloakClient.GetUsersParams{
		Email:               &email,
		Max:                 ptr.To[int32](1),
		BriefRepresentation: ptr.To(true),
		Exact:               ptr.To(true),
	}

	// Query users using the generated client
	resp, err := s.keycloakClient.GetUsersWithResponse(ctx, realm, params)
	if err != nil { // coverage-ignore
		log.Err(err).Str("email", sanitizeEmail(email)).Msg("Failed to query user")
		return nil, errors.Wrap(err, "failed to query Keycloak API for user %s in realm %s", sanitizeEmail(email), realm)
	}

	if resp.StatusCode() != http.StatusOK {
		log.Error().Int("status_code", resp.StatusCode()).Str("email", sanitizeEmail(email)).Msg("Non-200 response from Keycloak")
		return nil, errors.New("keycloak API returned status %d for user %s", resp.StatusCode(), sanitizeEmail(email))
	}

	if resp.JSON200 == nil {
		return nil, nil
	}

	users := *resp.JSON200
	if len(users) == 0 {
		return nil, nil
	}

	if len(users) != 1 {
		log.Info().Str("email", sanitizeEmail(email)).Int("count", len(users)).Msg("unexpected user count")
		return nil, errors.New("expected 1 user, got %d for email %s", len(users), sanitizeEmail(email))
	}

	user := users[0]
	result := &graph.User{
		UserID:    *user.Id,
		Email:     *user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
	}

	return result, nil
}

func (s *Service) GetUsersByEmails(ctx context.Context, emails []string) (map[string]*graph.User, error) {
	log := logger.LoadLoggerFromContext(ctx)

	start := time.Now()
	defer func() {
		metrics.KeycloakDuration.WithLabelValues("get_users_by_emails").Observe(time.Since(start).Seconds())
	}()

	if len(emails) == 0 {
		return map[string]*graph.User{}, nil
	}

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("get_users_by_emails", "error").Inc()
		return nil, errors.Wrap(err, "failed to get KCP user context")
	}

	realm := kctx.IDMTenant
	result := make(map[string]*graph.User)

	var missingEmails []string

	// Check cache first if enabled
	if s.userCache != nil {
		cached, missing := s.userCache.GetMany(realm, emails)

		// Add cached users to result
		for email, user := range cached {
			result[email] = user
		}

		missingEmails = missing

		log.Debug().
			Int("requested", len(emails)).
			Int("cached_hits", len(cached)).
			Int("cache_misses", len(missing)).
			Msg("Cache lookup completed")
	} else {
		// No cache - need to fetch all
		missingEmails = emails
	}

	// Fetch missing users in parallel
	if len(missingEmails) > 0 {
		fetchedUsers, err := s.fetchUsersInParallel(ctx, realm, missingEmails)
		if err != nil {
			metrics.KeycloakRequests.WithLabelValues("get_users_by_emails", "error").Inc()
			return nil, errors.Wrap(err, "failed to fetch users in parallel for realm %s", realm)
		}

		// Add fetched users to result and cache
		for email, user := range fetchedUsers {
			result[email] = user

			// Store in cache if enabled
			if s.userCache != nil {
				s.userCache.Set(realm, email, user)
			}
		}
	}

	log.Info().
		Int("requested_emails", len(emails)).
		Int("returned_users", len(result)).
		Int("api_calls", len(missingEmails)).
		Msg("Completed user lookup with cache")

	metrics.KeycloakRequests.WithLabelValues("get_users_by_emails", "success").Inc()
	return result, nil
}

// fetchAllUsers retrieves all users from Keycloak using pagination
// Caches individual users by email and uses best effort error handling
func (s *Service) fetchAllUsers(ctx context.Context, realm string) ([]*graph.User, error) {
	log := logger.LoadLoggerFromContext(ctx)

	allUsers := make([]*graph.User, 0)
	var failedPages []int
	pageSize := s.cfg.Keycloak.PageSize
	var currentPage = 0

	log.Debug().
		Str("realm", realm).
		Int("page_size", pageSize).
		Msg("Starting to fetch all users from Keycloak")

	for {
		// Calculate offset for current page
		first := currentPage * pageSize

		// Configure pagination parameters
		firstArg := int32(first)
		maxArg := int32(pageSize)
		params := &keycloakClient.GetUsersParams{
			First:               &firstArg,
			Max:                 &maxArg,
			BriefRepresentation: ptr.To(true),
		}

		log.Debug().
			Int("page", currentPage).
			Int("first", first).
			Int("max", pageSize).
			Msg("Fetching users page")

		// Query users for current page
		resp, err := s.keycloakClient.GetUsersWithResponse(ctx, realm, params)
		if err != nil {
			log.Err(err).
				Int("page", currentPage).
				Msg("Failed to fetch users page, continuing with next page")
			failedPages = append(failedPages, currentPage)
			currentPage++
			continue
		}

		if resp.StatusCode() != http.StatusOK {
			log.Error().
				Int("status_code", resp.StatusCode()).
				Int("page", currentPage).
				Msg("Non-200 response from Keycloak, continuing with next page")
			failedPages = append(failedPages, currentPage)
			currentPage++
			continue
		}

		if resp.JSON200 == nil {
			log.Debug().Int("page", currentPage).Msg("No users returned, pagination complete")
			break
		}

		users := *resp.JSON200
		if len(users) == 0 {
			log.Debug().Int("page", currentPage).Msg("Empty page returned, pagination complete")
			break
		}

		log.Debug().
			Int("page", currentPage).
			Int("users_on_page", len(users)).
			Msg("Processing users from page")

		// Process users from current page
		for _, user := range users {
			if user.Id == nil || user.Email == nil {
				log.Warn().
					Interface("user", user).
					Msg("Skipping user with missing ID or email")
				continue
			}

			graphUser := &graph.User{
				UserID:    *user.Id,
				Email:     *user.Email,
				FirstName: user.FirstName,
				LastName:  user.LastName,
			}

			allUsers = append(allUsers, graphUser)

			// Cache individual user by email if cache is enabled
			if s.userCache != nil {
				s.userCache.Set(realm, *user.Email, graphUser)
			}
		}

		// If we got fewer users than page size, we've reached the end
		if len(users) < int(pageSize) {
			log.Debug().
				Int("page", currentPage).
				Int("users_on_page", len(users)).
				Msg("Last page reached (partial page)")
			break
		}

		currentPage++
	}

	log.Debug().
		Int("total_users", len(allUsers)).
		Int("failed_pages", len(failedPages)).
		Int("pages_processed", currentPage).
		Msg("Completed fetching all users from Keycloak")

	if len(failedPages) > 0 {
		log.Warn().
			Interface("failed_pages", failedPages).
			Msg("Some pages failed to fetch, returning partial results")
	}

	return allUsers, nil
}

// fetchUsersInParallel fetches multiple users from Keycloak in parallel using errgroup
// Fails fast on the first encountered error
func (s *Service) fetchUsersInParallel(ctx context.Context, realm string, emails []string) (map[string]*graph.User, error) {
	// Use errgroup with context for fail-fast behavior
	g, gCtx := errgroup.WithContext(ctx)

	// Thread-safe map to store results
	var mu sync.Mutex
	userMap := make(map[string]*graph.User)

	// Launch goroutines for each email using errgroup
	for _, email := range emails {
		email := email // capture loop variable
		g.Go(func() error {
			user, err := s.fetchUserFromKeycloak(gCtx, realm, email)
			if err != nil {
				// Return error immediately to trigger fail-fast behavior
				// Only log first few characters of email to avoid PII exposure
				return fmt.Errorf("failed to fetch user %s: %w", sanitizeEmail(email), err)
			}

			mu.Lock()
			defer mu.Unlock()
			if user != nil {
				userMap[email] = user
			}

			return nil
		})
	}

	// Wait for all goroutines to complete or first error
	if err := g.Wait(); err != nil {
		return nil, errors.Wrap(err, "error group failed during user fetching")
	}

	return userMap, nil
}

// EnrichUserRoles enriches user roles with complete user information from Keycloak
// Updates the UserRoles slice in-place with FirstName, LastName, and UserID from Keycloak
func (s *Service) EnrichUserRoles(ctx context.Context, userRoles []*graph.UserRoles) error {
	start := time.Now()
	defer func() {
		metrics.KeycloakDuration.WithLabelValues("enrich_user_roles").Observe(time.Since(start).Seconds())
	}()

	if len(userRoles) == 0 {
		return nil
	}

	// Extract unique email addresses from user roles
	emailSet := make(map[string]bool)
	var emails []string

	for _, userRole := range userRoles {
		if userRole.User != nil && userRole.User.Email != "" {
			if !emailSet[userRole.User.Email] {
				emailSet[userRole.User.Email] = true
				emails = append(emails, userRole.User.Email)
			}
		}
	}

	if len(emails) == 0 {
		return nil
	}

	// Batch call to get all users at once
	userMap, err := s.GetUsersByEmails(ctx, emails)
	if err != nil {
		metrics.KeycloakRequests.WithLabelValues("enrich_user_roles", "error").Inc()
		return errors.Wrap(err, "failed to get users by emails for enrichment")
	}

	// Update user roles with Keycloak data using the lookup map
	for _, userRole := range userRoles {
		if userRole.User != nil && userRole.User.Email != "" {
			if keycloakUser, exists := userMap[userRole.User.Email]; exists {
				// Update the user with complete information from Keycloak
				userRole.User.UserID = keycloakUser.UserID
				userRole.User.FirstName = keycloakUser.FirstName
				userRole.User.LastName = keycloakUser.LastName
				// Email is already set from OpenFGA
			}
		}
	}

	metrics.KeycloakRequests.WithLabelValues("enrich_user_roles", "success").Inc()
	return nil
}
