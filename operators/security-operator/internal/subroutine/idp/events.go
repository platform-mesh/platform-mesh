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

package idp

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"
)

var (
	eventsSkippedRealms = []string{"master", "welcome"}
	eventReaderRoles    = []string{"view-events", "view-users"}
)

// ensureEvents switches on the user event store of the realm and grants the
// event reader client read access to its events and users, each when enabled.
func (s *subroutine) ensureEvents(ctx context.Context, realmName string, realmClient *keycloak.AdminClient) error {
	if slices.Contains(eventsSkippedRealms, realmName) {
		return nil
	}
	if s.cfg.IDP.Events.Enabled {
		if err := s.ensureEventStore(ctx, realmName, realmClient); err != nil {
			return err
		}
	}
	if s.cfg.IDP.Events.ReaderClient != "" {
		if err := s.ensureEventReaderRoles(ctx, realmName); err != nil {
			return err
		}
	}
	return nil
}

// ensureEventStore enables the store, adds the configured event types to the
// ones already enabled and raises a shorter retention to the configured one.
func (s *subroutine) ensureEventStore(ctx context.Context, realmName string, realmClient *keycloak.AdminClient) error {
	current, err := realmClient.GetEventsConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get events config of realm %q: %w", realmName, err)
	}

	desired := keycloak.RealmEventsConfig{
		EventsEnabled:    true,
		EventsExpiration: int64(s.cfg.IDP.Events.Expiration / time.Second),
	}
	if !current.EventsEnabled {
		// Types listed for a disabled store are not in use, so they are replaced.
		desired.EnabledEventTypes = s.cfg.IDP.Events.Types
	} else {
		// 0 means events never expire.
		keepExpiration := current.EventsExpiration == 0 || current.EventsExpiration >= desired.EventsExpiration
		if keepExpiration {
			desired.EventsExpiration = current.EventsExpiration
		}
		types := slices.Clone(current.EnabledEventTypes)
		for _, t := range s.cfg.IDP.Events.Types {
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
		if len(types) > len(current.EnabledEventTypes) {
			desired.EnabledEventTypes = types
		} else if keepExpiration {
			return nil
		}
	}

	if err := realmClient.UpdateEventsConfig(ctx, desired); err != nil {
		return fmt.Errorf("failed to update events config of realm %q: %w", realmName, err)
	}
	logger.LoadLoggerFromContext(ctx).Info().Str("realm", realmName).Msg("Realm event store configured")
	return nil
}

// ensureEventReaderRoles grants the reader client's service account the
// eventReaderRoles of the realm's management client in master.
func (s *subroutine) ensureEventReaderRoles(ctx context.Context, realmName string) error {
	master := keycloak.NewAdminClient(s.adminClient, s.keycloakBaseURL, "master")
	readerClientID := s.cfg.IDP.Events.ReaderClient

	reader, err := master.GetClientByClientID(ctx, readerClientID)
	if err != nil {
		return fmt.Errorf("failed to get event reader client %q: %w", readerClientID, err)
	}
	if reader == nil {
		return fmt.Errorf("event reader client %q not found in realm master", readerClientID)
	}
	readerUser, err := master.GetServiceAccountUser(ctx, reader.ID)
	if err != nil {
		return fmt.Errorf("failed to get service account user of event reader client %q: %w", readerClientID, err)
	}

	managementClientID := realmName + "-realm"
	managementClient, err := master.GetClientByClientID(ctx, managementClientID)
	if err != nil {
		return fmt.Errorf("failed to get management client %q: %w", managementClientID, err)
	}
	if managementClient == nil {
		return fmt.Errorf("management client %q not found in realm master", managementClientID)
	}

	granted, err := master.ListUserClientRoles(ctx, readerUser.ID, managementClient.ID)
	if err != nil {
		return fmt.Errorf("failed to get roles of event reader client %q: %w", readerClientID, err)
	}

	var missing []keycloak.RoleInfo
	for _, roleName := range eventReaderRoles {
		if slices.ContainsFunc(granted, func(r keycloak.RoleInfo) bool { return r.Name == roleName }) {
			continue
		}
		role, err := master.GetClientRole(ctx, managementClient.ID, roleName)
		if err != nil {
			return fmt.Errorf("failed to get role %q of client %q: %w", roleName, managementClientID, err)
		}
		if role == nil {
			return fmt.Errorf("role %q not found on client %q", roleName, managementClientID)
		}
		missing = append(missing, *role)
	}
	if len(missing) == 0 {
		return nil
	}

	if err := master.AssignClientRolesToUser(ctx, readerUser.ID, managementClient.ID, missing); err != nil {
		return fmt.Errorf("failed to grant event reader client %q roles on realm %q: %w", readerClientID, realmName, err)
	}
	logger.LoadLoggerFromContext(ctx).Info().Str("realm", realmName).Str("clientID", readerClientID).Msg("Event reader roles granted")
	return nil
}
