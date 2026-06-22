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

import (
	"net/http"
	"regexp"
)

var spiffeUriReg = regexp.MustCompile(`URI=([a-z:\/\/\.\-]*)`)

func GetSpiffeUrlValue(header http.Header) *string {
	headervalue := header.Get(HeaderSpiffeValue)
	uriVal := GetURIValue(headervalue)

	if len(uriVal) > 0 {
		return &uriVal
	}
	return nil
}

func GetURIValue(headerVal string) string {
	match := spiffeUriReg.FindSubmatch([]byte(headerVal))
	if len(match) == 2 {
		return string(match[1])
	} else {
		return ""
	}
}
