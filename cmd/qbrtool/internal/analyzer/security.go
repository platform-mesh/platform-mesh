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

package analyzer

import (
	"regexp"
	"strings"
	"time"

	"go.platform-mesh.io/qbrtool/internal/models"
)

// SecurityAnalyzer detects security related items
type SecurityAnalyzer struct {
	keywords []string
	pattern  *regexp.Regexp
}

// NewSecurityAnalyzer creates a new security analyzer
func NewSecurityAnalyzer() *SecurityAnalyzer {
	keywords := []string{
		"security",
		"secure",
		"securing",
		"vulnerability",
		"vulnerabilities",
		"vulnerable",
		"CVE",
		"RBAC",
		"role based access",
		"role-based access",
		"authentication",
		"authenticate",
		"authn",
		"authorization",
		"authorize",
		"authz",
		"TLS",
		"SSL",
		"HTTPS",
		"certificate",
		"certificates",
		"cert",
		"certs",
		"encryption",
		"encrypt",
		"encrypted",
		"decryption",
		"decrypt",
		"audit",
		"auditing",
		"audit log",
		"penetration",
		"pentest",
		"pen test",
		"hardening",
		"harden",
		"hardened",
		"secrets",
		"secret",
		"secret management",
		"vault",
		"hashicorp vault",
		"credential",
		"credentials",
		"password",
		"passwords",
		"token",
		"tokens",
		"API key",
		"API keys",
		"access control",
		"ACL",
		"IAM",
		"identity",
		"OIDC",
		"OAuth",
		"SAML",
		"SSO",
		"single sign-on",
		"MFA",
		"2FA",
		"two-factor",
		"multi-factor",
		"firewall",
		"network policy",
		"network policies",
		"pod security",
		"PSP",
		"PSA",
		"seccomp",
		"AppArmor",
		"SELinux",
		"OWASP",
		"XSS",
		"CSRF",
		"SQL injection",
		"injection attack",
		"privilege escalation",
		"least privilege",
		"zero trust",
		"defense in depth",
		"threat model",
		"threat modeling",
		"security scan",
		"security scanner",
		"trivy",
		"snyk",
		"grype",
		"falco",
		"kyverno",
		"OPA",
		"gatekeeper",
	}

	// Build pattern
	escapedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		escapedKeywords[i] = regexp.QuoteMeta(kw)
	}
	pattern := regexp.MustCompile(`(?i)\b(` + strings.Join(escapedKeywords, "|") + `)\b`)

	return &SecurityAnalyzer{
		keywords: keywords,
		pattern:  pattern,
	}
}

// Name returns the analyzer name
func (a *SecurityAnalyzer) Name() string {
	return "security"
}

// Analyze searches for security related items
func (a *SecurityAnalyzer) Analyze(items []*models.ProjectItem) *models.AnalysisResult {
	var matches []models.MatchedItem
	keywordCounts := make(map[string]int)

	for _, item := range items {
		matchInfo := a.findSecurityContent(item)
		if matchInfo != nil {
			// Count matched keywords
			for _, match := range strings.Split(matchInfo.MatchedText, ", ") {
				keywordCounts[strings.ToLower(match)]++
			}

			matches = append(matches, models.MatchedItem{
				Item:      *item,
				MatchInfo: *matchInfo,
			})
		}
	}

	return &models.AnalysisResult{
		Type:  "security",
		Items: matches,
		Summary: models.KeywordAnalysis{
			MatchedKeywords: keywordCounts,
			Total:           len(matches),
		},
		Timestamp: time.Now(),
	}
}

// findSecurityContent searches for security keywords
func (a *SecurityAnalyzer) findSecurityContent(item *models.ProjectItem) *models.MatchInfo {
	// Check title (highest priority)
	if matches := a.pattern.FindAllString(item.Title, -1); len(matches) > 0 {
		return &models.MatchInfo{
			MatchedIn:   "title",
			MatchedText: strings.Join(uniqueStrings(matches), ", "),
			Confidence:  1.0,
		}
	}

	// Check labels (high priority)
	for _, label := range item.Labels {
		if matches := a.pattern.FindAllString(label, -1); len(matches) > 0 {
			return &models.MatchInfo{
				MatchedIn:   "labels",
				MatchedText: strings.Join(uniqueStrings(matches), ", "),
				Confidence:  1.0,
			}
		}
	}

	// Check body (lower confidence, might have false positives)
	if matches := a.pattern.FindAllString(item.Body, -1); len(matches) > 0 {
		// Filter out common false positives by requiring at least 2 matches
		// or having a high-confidence keyword
		highConfidenceKeywords := map[string]bool{
			"cve": true, "vulnerability": true, "security": true,
			"penetration": true, "hardening": true, "rbac": true,
		}

		uniqueMatches := uniqueStrings(matches)
		hasHighConfidence := false
		for _, m := range uniqueMatches {
			if highConfidenceKeywords[strings.ToLower(m)] {
				hasHighConfidence = true
				break
			}
		}

		if len(uniqueMatches) >= 2 || hasHighConfidence {
			return &models.MatchInfo{
				MatchedIn:   "body",
				MatchedText: strings.Join(uniqueMatches, ", "),
				Confidence:  0.7,
			}
		}
	}

	return nil
}
