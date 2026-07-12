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

package migration

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"
)

// interpolate builds a CEL environment and uses it to interpolate expression in the given template.
func interpolate(ctx context.Context, value any, vars map[string]any) (any, error) {
	env, err := celEnv(vars)
	if err != nil {
		return nil, err
	}
	return interpolateValue(ctx, env, value, vars)
}

func interpolateValue(ctx context.Context, env *cel.Env, value any, vars map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		return interpolateString(ctx, env, typed, vars)
	case map[string]any:
		for key, entry := range typed {
			interpolated, err := interpolateValue(ctx, env, entry, vars)
			if err != nil {
				return nil, err
			}
			typed[key] = interpolated
		}
		return typed, nil
	case []any:
		for i, entry := range typed {
			interpolated, err := interpolateValue(ctx, env, entry, vars)
			if err != nil {
				return nil, err
			}
			typed[i] = interpolated
		}
		return typed, nil
	default:
		return value, nil
	}
}

func interpolateString(ctx context.Context, env *cel.Env, s string, vars map[string]any) (any, error) {
	start := strings.Index(s, "${")
	if start < 0 {
		return s, nil
	}

	var out strings.Builder
	rest := s
	first := true
	for {
		idx := strings.Index(rest, "${")
		if idx < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:idx])

		end, err := matchExpression(rest[idx+2:])
		if err != nil {
			return nil, fmt.Errorf("in %q: %w", s, err)
		}
		expression := rest[idx+2 : idx+2+end]

		result, err := evalExpression(ctx, env, expression, vars)
		if err != nil {
			return nil, err
		}

		// The whole string is a single expression: keep the value's type.
		if first && idx == 0 && idx+2+end+1 == len(rest) {
			return result, nil
		}
		first = false

		fmt.Fprintf(&out, "%v", result)
		rest = rest[idx+2+end+1:]
	}

	return out.String(), nil
}

// matchExpression returns the index of the closing brace of the
// expression starting after "${", accounting for nested braces and CEL
// string literals.
func matchExpression(s string) (int, error) {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i, nil
			}
			depth--
		}
	}
	return 0, fmt.Errorf("unterminated expression")
}

func evalExpression(ctx context.Context, env *cel.Env, expression string, vars map[string]any) (any, error) {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling expression %q: %w", expression, issues.Err())
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("building program for expression %q: %w", expression, err)
	}

	value, _, err := program.ContextEval(ctx, vars)
	if err != nil {
		return nil, fmt.Errorf("evaluating expression %q: %w", expression, err)
	}

	native, err := value.ConvertToNative(reflect.TypeOf((*any)(nil)).Elem())
	if err != nil {
		return nil, fmt.Errorf("converting result of expression %q: %w", expression, err)
	}
	return native, nil
}

// celEnv builds a CEL environment with the given variables declared as DynType.
func celEnv(vars map[string]any) (*cel.Env, error) {
	envOpts := make([]cel.EnvOption, 0, len(vars))
	for name := range vars {
		envOpts = append(envOpts, cel.Variable(name, cel.DynType))
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating CEL environment: %w", err)
	}
	return env, nil
}
