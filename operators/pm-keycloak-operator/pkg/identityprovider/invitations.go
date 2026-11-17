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

package identityprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type InvitationBackend interface {
	EnsureInvitationUser(context.Context, string, string, string, string) (string, error)
	DispatchInvitation(context.Context, string, string, string, string, string, int) error
}

type invitationUser struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Enabled bool   `json:"enabled"`
}

func validNativeSubject(subject string) bool {
	return subject != "" && subject != "." && subject != ".." && utf8.ValidString(subject) &&
		!strings.ContainsAny(subject, "/\\%?#") && strings.IndexFunc(subject, unicode.IsControl) < 0
}

func validateInvitationUser(user invitationUser, email string) error {
	if !validNativeSubject(user.ID) || !strings.EqualFold(user.Email, email) {
		return errors.New("provider user does not match the requested invitation contact")
	}
	if !user.Enabled {
		return errors.New("invitation user is disabled; administrative recovery is required")
	}
	return nil
}

// EnsureInvitationUser provisions a delivery target, not an accepted identity
// or authorization grant. An existing subject must never be silently replaced.
func (b *NativeBackend) EnsureInvitationUser(ctx context.Context, realm, owner, email, existingSubject string) (string, error) {
	if _, err := b.ownedRealm(ctx, realm, owner); err != nil {
		return "", err
	}
	if email == "" {
		return "", errors.New("invitation contact is required")
	}
	if existingSubject != "" {
		if !validNativeSubject(existingSubject) {
			return "", errors.New("invalid native invitation subject")
		}
		var user invitationUser
		if err := b.request(ctx, b.admin, http.MethodGet, b.realmURL(realm)+"/users/"+url.PathEscape(existingSubject), nil, &user); err != nil {
			return "", err
		}
		if user.ID != existingSubject {
			return "", errors.New("provider changed the invitation user's identity")
		}
		if err := validateInvitationUser(user, email); err != nil {
			return "", err
		}
		return user.ID, nil
	}
	find := func() ([]invitationUser, error) {
		var users []invitationUser
		query := url.Values{"email": {email}, "exact": {"true"}, "max": {"2"}}
		err := b.request(ctx, b.admin, http.MethodGet, b.realmURL(realm)+"/users?"+query.Encode(), nil, &users)
		return users, err
	}
	users, err := find()
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		body := map[string]any{
			"username": email, "email": email, "enabled": true, "emailVerified": false,
			"requiredActions": []string{"UPDATE_PASSWORD", "VERIFY_EMAIL"},
		}
		if err := b.request(ctx, b.admin, http.MethodPost, b.realmURL(realm)+"/users", body, nil); err != nil {
			return "", err
		}
		users, err = find()
		if err != nil {
			return "", err
		}
	}
	if len(users) != 1 {
		return "", errors.New("invitation contact lookup is missing or ambiguous")
	}
	if err := validateInvitationUser(users[0], email); err != nil {
		return "", err
	}
	return users[0].ID, nil
}

func (b *NativeBackend) DispatchInvitation(ctx context.Context, realm, owner, subject, clientID, redirect string, lifespan int) error {
	if _, err := b.ownedRealm(ctx, realm, owner); err != nil {
		return err
	}
	if !validNativeSubject(subject) || clientID == "" || redirect == "" || lifespan <= 0 {
		return errors.New("invitation subject, client, redirect and positive lifespan are required")
	}
	target, err := url.Parse(redirect)
	if err != nil {
		return errors.New("invalid invitation redirect")
	}
	loopback := target.Scheme == "http" && net.ParseIP(target.Hostname()).IsLoopback()
	if target.Hostname() == "" || target.User != nil || target.Fragment != "" || (target.Scheme != "https" && !loopback) {
		return errors.New("invitation redirect must be an HTTPS callback or an explicit loopback callback")
	}
	query := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "lifespan": {strconv.Itoa(lifespan)},
	}
	if err := b.request(ctx, b.admin, http.MethodPut,
		b.realmURL(realm)+"/users/"+url.PathEscape(subject)+"/execute-actions-email?"+query.Encode(),
		[]string{"UPDATE_PASSWORD", "VERIFY_EMAIL"}, nil); err != nil {
		return fmt.Errorf("dispatch invitation actions email: %w", err)
	}
	return nil
}
