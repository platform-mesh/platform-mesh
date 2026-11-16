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

package identity

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/cel-go/cel"
)

func TestUsernameAndCEL(t *testing.T) {
	env, err := cel.NewEnv(cel.Variable("claims", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	ast, issues := env.Compile(UsernameExpression)
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		p    Principal
		want string
	}{
		{"ascii", Principal{"https://issuer.example", "alice"}, "oidc:v1:22:https://issuer.examplealice"},
		{"unicode path", Principal{"https://issuer.example/租户/😀", "用户"}, "oidc:v1:27:https://issuer.example/租户/😀用户"},
		{"delimiters", Principal{"https://issuer.example/a:b/", "x:y/z"}, "oidc:v1:27:https://issuer.example/a:b/x:y/z"},
		{"same subject other issuer", Principal{"https://other.example", "alice"}, "oidc:v1:21:https://other.examplealice"},
		{"same issuer other subject", Principal{"https://issuer.example", "bob"}, "oidc:v1:22:https://issuer.examplebob"},
		{"exact trailing slash", Principal{"https://issuer.example/", "alice"}, "oidc:v1:23:https://issuer.example/alice"},
		{"escaped path", Principal{"https://issuer.example/%E2%9C%93", "e\u0301"}, "oidc:v1:32:https://issuer.example/%E2%9C%93e\u0301"},
		{"split one", Principal{"https://issuer.example/a", "bc"}, "oidc:v1:24:https://issuer.example/abc"},
		{"split two", Principal{"https://issuer.example/ab", "c"}, "oidc:v1:25:https://issuer.example/abc"},
	}
	seen := map[string]bool{}
	fgaSeen := map[string]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.Username()
			if err != nil || got != tt.want {
				t.Fatalf("Username() = %q, %v; want %q", got, err, tt.want)
			}
			value, _, err := program.Eval(map[string]any{"claims": map[string]any{"iss": tt.p.Issuer, "sub": tt.p.Subject}})
			if err != nil || value.Value() != got {
				t.Fatalf("CEL = %v, %v; want %q", value, err, got)
			}
			parsed, err := ParseUsername(got)
			if err != nil || parsed != tt.p {
				t.Fatalf("ParseUsername() = %+v, %v; want %+v", parsed, err, tt.p)
			}
			if seen[got] {
				t.Fatalf("duplicate username %q", got)
			}
			seen[got] = true
			fga, err := tt.p.FGAUser()
			if err != nil || len(fga) != len("user:oidc_v1_")+64 || !strings.HasPrefix(fga, "user:oidc_v1_") {
				t.Fatalf("FGAUser() = %q, %v", fga, err)
			}
			if fgaSeen[fga] {
				t.Fatalf("duplicate FGA user %q", fga)
			}
			fgaSeen[fga] = true
		})
	}
	_, _, err = program.Eval(map[string]any{"claims": map[string]any{"iss": 3, "sub": "a"}})
	if err == nil {
		t.Fatal("CEL accepted a non-string issuer")
	}
}

func TestClaimValidationExpression(t *testing.T) {
	env, err := cel.NewEnv(cel.Variable("claims", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	ast, issues := env.Compile(ClaimValidationExpression)
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}

	issuer := "https://issuer.example/租户"
	base, err := (Principal{issuer, "a"}).Username()
	if err != nil {
		t.Fatal(err)
	}
	atLimit := strings.Repeat("a", MaxUsernameBytes-len(base)+1)
	tests := []struct {
		name   string
		claims map[string]any
		want   bool
	}{
		{"valid", map[string]any{"iss": issuer, "sub": "用户:a"}, true},
		{"missing issuer", map[string]any{"sub": "a"}, false},
		{"missing subject", map[string]any{"iss": issuer}, false},
		{"null issuer", map[string]any{"iss": nil, "sub": "a"}, false},
		{"null subject", map[string]any{"iss": issuer, "sub": nil}, false},
		{"numeric issuer", map[string]any{"iss": 3, "sub": "a"}, false},
		{"numeric subject", map[string]any{"iss": issuer, "sub": 3}, false},
		{"empty issuer", map[string]any{"iss": "", "sub": "a"}, false},
		{"empty subject", map[string]any{"iss": issuer, "sub": ""}, false},
		{"newline in issuer", map[string]any{"iss": issuer + "\n", "sub": "a"}, false},
		{"newline in subject", map[string]any{"iss": issuer, "sub": "a\nb"}, false},
		{"C1 control in issuer", map[string]any{"iss": issuer + "\u0085", "sub": "a"}, false},
		{"C1 control in subject", map[string]any{"iss": issuer, "sub": "a\u009fb"}, false},
		{"byte boundary", map[string]any{"iss": issuer, "sub": atLimit}, true},
		{"one byte over", map[string]any{"iss": issuer, "sub": atLimit + "a"}, false},
		{"unicode byte boundary", map[string]any{"iss": issuer, "sub": atLimit[:len(atLimit)-1] + "é"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := program.Eval(map[string]any{"claims": tt.claims})
			if err != nil || got.Value() != tt.want {
				t.Fatalf("CEL = %v, %v; want %t", got, err, tt.want)
			}
			iss, issOK := tt.claims["iss"].(string)
			sub, subOK := tt.claims["sub"].(string)
			if issOK && subOK && ((Principal{iss, sub}).Validate() == nil) != tt.want {
				t.Fatalf("Go validation disagrees for %q / %q", iss, sub)
			}
		})
	}
}

func TestInvalidPrincipals(t *testing.T) {
	validIssuer := "https://issuer.example"
	tests := []Principal{
		{"", "a"},
		{"http://issuer.example", "a"},
		{"https:issuer.example", "a"},
		{"https:///path", "a"},
		{"https://:443/path", "a"},
		{"https://user@issuer.example", "a"},
		{"https://issuer.example?", "a"},
		{"https://issuer.example?q=1", "a"},
		{"https://issuer.example#", "a"},
		{"https://issuer.example#fragment", "a"},
		{"https://issuer.example/%zz", "a"},
		{"https://issuer.example/\xff", "a"},
		{"https://issuer.example/\u0085", "a"},
		{validIssuer, ""},
		{validIssuer, "\xff"},
		{validIssuer, "a\nb"},
		{validIssuer, "a\u0085b"},
	}
	for _, p := range tests {
		t.Run(fmt.Sprintf("%q/%q", p.Issuer, p.Subject), func(t *testing.T) {
			if err := p.Validate(); err == nil {
				t.Fatal("Validate accepted invalid principal")
			}
			if got, err := p.Username(); err == nil || got != "" {
				t.Fatalf("Username() = %q, %v", got, err)
			}
			if got, err := p.FGAUser(); err == nil || got != "" {
				t.Fatalf("FGAUser() = %q, %v", got, err)
			}
		})
	}
}

func TestUsernameSizeBound(t *testing.T) {
	p := Principal{Issuer: "https://issuer.example", Subject: "a"}
	base, err := p.Username()
	if err != nil {
		t.Fatal(err)
	}
	p.Subject = strings.Repeat("a", MaxUsernameBytes-len(base)+1)
	username, err := p.Username()
	if err != nil || len(username) != MaxUsernameBytes {
		t.Fatalf("boundary username length %d: %v", len(username), err)
	}
	if _, err := ParseUsername(username); err != nil {
		t.Fatal(err)
	}
	p.Subject += "é"
	if err := p.Validate(); err == nil {
		t.Fatal("accepted oversized UTF-8 username")
	}
	if _, err := ParseUsername(username + "é"); err == nil {
		t.Fatal("parsed oversized username")
	}
}

func TestParseUsernameRejectsMalformed(t *testing.T) {
	tests := []string{
		"", "oidc:v2:22:https://issuer.examplealice", "OIDC:v1:22:https://issuer.examplealice",
		"oidc:v1:", "oidc:v1::https://issuer.examplealice", "oidc:v1:0:https://issuer.examplealice",
		"oidc:v1:022:https://issuer.examplealice", "oidc:v1:+22:https://issuer.examplealice",
		"oidc:v1:-22:https://issuer.examplealice", "oidc:v1:2x:https://issuer.examplealice",
		"oidc:v1:999999999999999999999999:https://issuer.examplealice",
		"oidc:v1:99:https://issuer.examplealice", "oidc:v1:22:https://issuer.example",
		"oidc:v1:22:http://issuer.example/alice",
		"oidc:v1:22:https://issuer.example\xff",
	}
	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			if p, err := ParseUsername(s); err == nil {
				t.Fatalf("accepted %q as %+v", s, p)
			}
		})
	}
}

func TestValidLengthTamperingIsAnotherPrincipal(t *testing.T) {
	p, err := ParseUsername("oidc:v1:21:https://issuer.examplealice")
	if err != nil || p != (Principal{"https://issuer.exampl", "ealice"}) {
		t.Fatalf("ParseUsername() = %+v, %v", p, err)
	}
}

func TestFGAUserKnownVector(t *testing.T) {
	p := Principal{"https://issuer.example", "alice"}
	got, err := p.FGAUser()
	if err != nil {
		t.Fatal(err)
	}
	const want = "user:oidc_v1_a19eade2f7ac9ed68fcb75ff262f0c42054ae0791c1575f8a7080ddebdad1838"
	if got != want {
		t.Fatalf("FGAUser() = %q; want %q", got, want)
	}
}

func TestPrincipalJSON(t *testing.T) {
	p := Principal{"https://issuer.example", "alice"}
	b, err := json.Marshal(p)
	if err != nil || string(b) != `{"issuer":"https://issuer.example","subject":"alice"}` {
		t.Fatalf("Marshal = %s, %v", b, err)
	}
}

func FuzzUsernameRoundTrip(f *testing.F) {
	f.Add("https://issuer.example", "alice")
	f.Add("https://issuer.example/租户", "a:b")
	env, err := cel.NewEnv(cel.Variable("claims", cel.DynType))
	if err != nil {
		f.Fatal(err)
	}
	ast, issues := env.Compile(UsernameExpression)
	if issues.Err() != nil {
		f.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		f.Fatal(err)
	}
	validationAST, issues := env.Compile(ClaimValidationExpression)
	if issues.Err() != nil {
		f.Fatal(issues.Err())
	}
	validation, err := env.Program(validationAST)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, issuer, subject string) {
		p := Principal{issuer, subject}
		username, err := p.Username()
		if err != nil {
			return
		}
		got, err := ParseUsername(username)
		if err != nil || got != p {
			t.Fatalf("round trip: %+v, %v; want %+v", got, err, p)
		}
		value, _, err := program.Eval(map[string]any{"claims": map[string]any{"iss": issuer, "sub": subject}})
		if err != nil || value.Value() != username {
			t.Fatalf("CEL = %v, %v; want %q", value, err, username)
		}
		value, _, err = validation.Eval(map[string]any{"claims": map[string]any{"iss": issuer, "sub": subject}})
		if err != nil || value.Value() != true {
			t.Fatalf("CEL validation = %v, %v; want true", value, err)
		}
	})
}

func FuzzClaimValidation(f *testing.F) {
	f.Add("alice")
	f.Add("用户\u0085")
	f.Add(strings.Repeat("a", MaxUsernameBytes))
	env, err := cel.NewEnv(cel.Variable("claims", cel.DynType))
	if err != nil {
		f.Fatal(err)
	}
	ast, issues := env.Compile(ClaimValidationExpression)
	if issues.Err() != nil {
		f.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, subject string) {
		if !utf8.ValidString(subject) {
			return
		}
		p := Principal{"https://issuer.example/租户", subject}
		value, _, err := program.Eval(map[string]any{"claims": map[string]any{"iss": p.Issuer, "sub": subject}})
		if err != nil || value.Value() != (p.Validate() == nil) {
			t.Fatalf("CEL validation = %v, %v; Go validation = %v", value, err, p.Validate())
		}
	})
}
