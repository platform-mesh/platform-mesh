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

package jwt

import "strings"

type rawClaims struct {
	RawAudiences  any    `json:"aud"` // RawAudiences could be a []string or string depending on the serialization in IdP site
	RawEmail      string `json:"email,omitempty"`
	RawMail       string `json:"mail,omitempty"`
	RawGivenName  string `json:"given_name,omitempty"`
	RawFamilyName string `json:"family_name,omitempty"`
}

type rawWebToken struct {
	rawClaims
	IssuerAttributes
	UserAttributes
}

func (r rawWebToken) getMail() (mail string) {
	mail = strings.TrimSpace(r.RawMail)
	if mail == "" {
		mail = strings.TrimSpace(r.RawEmail)
	}
	return
}

func (r rawWebToken) getLastName() (lastName string) {
	lastName = strings.TrimSpace(r.LastName)
	if lastName == "" {
		lastName = strings.TrimSpace(r.RawFamilyName)
	}
	return
}

func (r rawWebToken) getFirstName() (firstName string) {
	firstName = strings.TrimSpace(r.FirstName)
	if firstName == "" {
		firstName = strings.TrimSpace(r.RawGivenName)
	}
	return
}

func (r rawWebToken) getAudiences() (audiences []string) {
	switch audienceList := r.RawAudiences.(type) {
	case string:
		audiences = []string{audienceList}
	case []any:
		for _, val := range audienceList {
			aud, ok := val.(string)
			if ok {
				audiences = append(audiences, aud)
			}
		}
	}

	return
}
