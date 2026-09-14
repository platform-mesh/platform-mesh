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

package util

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const upperHex = "0123456789ABCDEF"

// EncodeObjectIDPart percent-encodes a raw identifier segment before it is
// embedded in an OpenFGA object ID. OpenFGA object IDs cannot contain colons,
// hashes, ASCII spaces, or Unicode control characters.
//
// Two further characters are escaped even though OpenFGA accepts them:
//
//   - '%', so the transformation stays injective. Without it the raw name
//     "a%3Ab" and the encoded form of "a:b" would collide.
//   - '/', because callers join several encoded segments with '/' (cluster,
//     namespace, name). Without it the assembled ID is ambiguous: the pair
//     (name "ns/foo", no namespace) and (name "foo", namespace "ns") would
//     render the same key, so a grant on one resource would authorize the
//     other.
//
// Encoding is therefore injective both per segment and across an assembled,
// '/'-joined object ID. Kubernetes forbids both '%' and '/' in path-segment
// names, so no key derived from a valid Kubernetes name changes shape.
//
// The input must be an unencoded value. Applying this function more than once
// will encode the percent signs introduced by the first call.
func EncodeObjectIDPart(value string) string {
	if !needsObjectIDPartEncoding(value) {
		return value
	}

	var encoded strings.Builder
	encoded.Grow(len(value))

	for offset := 0; offset < len(value); {
		r, size := utf8.DecodeRuneInString(value[offset:])
		if r == utf8.RuneError && size == 1 {
			writePercentEncodedByte(&encoded, value[offset])
			offset++
			continue
		}

		part := value[offset : offset+size]
		if shouldEncodeObjectIDRune(r) {
			for index := range len(part) {
				writePercentEncodedByte(&encoded, part[index])
			}
		} else {
			encoded.WriteString(part)
		}
		offset += size
	}

	return encoded.String()
}

func needsObjectIDPartEncoding(value string) bool {
	for offset := 0; offset < len(value); {
		r, size := utf8.DecodeRuneInString(value[offset:])
		if (r == utf8.RuneError && size == 1) || shouldEncodeObjectIDRune(r) {
			return true
		}
		offset += size
	}
	return false
}

func shouldEncodeObjectIDRune(r rune) bool {
	return r == '%' || r == '/' || r == ':' || r == '#' || r == ' ' || unicode.IsControl(r)
}

func writePercentEncodedByte(builder *strings.Builder, value byte) {
	builder.WriteByte('%')
	builder.WriteByte(upperHex[value>>4])
	builder.WriteByte(upperHex[value&0x0f])
}
