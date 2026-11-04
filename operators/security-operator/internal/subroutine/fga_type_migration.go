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

package subroutine

// Migration of FGA types renamed by platform-mesh#436. Type names used to be
// cut from the front so that create_<group>_<plural> fit 50 characters; they
// are now never cut. Everything in this file only exists to move the tuples of
// existing stores and can be removed once all installations have upgraded.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"google.golang.org/protobuf/proto"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	fgautil "go.platform-mesh.io/golang-commons/fga/util"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/fga"
)

// legacyTypeAnnotation carries the pre-#436 type name of the resource on a
// generated AuthorizationModel whose type was renamed.
const legacyTypeAnnotation = "core.platform-mesh.io/legacy-fga-type"

const (
	maxObjectLength       = 256
	maxMigrationRounds    = 3
	legacyRelationLength  = 50
	legacyRelationPrefix  = "create_"
	rolePrefix            = "role:"
	legacyTypeRemoveIssue = "platform-mesh#436"
)

// legacyTypeName returns the type name generated before platform-mesh#436. It
// reports false where that algorithm produced no name (it panicked).
func legacyTypeName(group, plural, singular string) (string, bool) {
	cut := len(legacyRelationPrefix+group+"_"+plural) - legacyRelationLength
	if group == "" {
		group = "core"
	}
	if cut > len(group) {
		return "", false
	}
	return strings.ReplaceAll(group[max(cut, 0):], ".", "_") + "_" + singular, true
}

// legacyRename returns the pre-#436 type name of a resource if it differs from
// the current one.
func legacyRename(group, plural, singular string) (string, bool) {
	legacy, ok := legacyTypeName(group, plural, singular)
	if !ok || !isShortenedFrom(legacy, fgautil.ConvertToTypeName(group, singular)) {
		return "", false
	}
	return legacy, true
}

// isShortenedFrom reports whether legacy is typeName cut from the front.
func isShortenedFrom(legacy, typeName string) bool {
	return len(legacy) < len(typeName) && strings.HasSuffix(typeName, legacy)
}

// typeRenames maps a legacy type name to the current names it was derived
// from. More than one current name means the legacy type was shared by
// several APIs.
type typeRenames map[string][]string

func (r typeRenames) add(legacy, current string) {
	if !slices.Contains(r[legacy], current) {
		r[legacy] = append(r[legacy], current)
	}
}

// addAuthorizationModel records the rename annotated on a generated module.
func (r typeRenames) addAuthorizationModel(model pmcorev1alpha1.AuthorizationModel) error {
	legacy, ok := model.Annotations[legacyTypeAnnotation]
	if !ok {
		return nil
	}

	mdl, extended, err := language.TransformModularDSLToProto(model.Spec.Model)
	if err != nil {
		return fmt.Errorf("parsing authorization model %s: %w", model.Name, err)
	}

	var types []string
	for _, td := range mdl.GetTypeDefinitions() {
		if _, ok := extended[td.GetType()]; !ok {
			types = append(types, td.GetType())
		}
	}
	if len(types) != 1 {
		return fmt.Errorf("authorization model %s has annotation %s but defines %d types", model.Name, legacyTypeAnnotation, len(types))
	}
	if !isShortenedFrom(legacy, types[0]) {
		return fmt.Errorf("authorization model %s: annotation %s %q is not a shortened form of type %s", model.Name, legacyTypeAnnotation, legacy, types[0])
	}

	r.add(legacy, types[0])
	return nil
}

// migrateRenamedTypes moves every tuple that references a renamed type to the
// new name: it writes a transition model that defines old and new types, copies
// the tuples, and only then deletes the originals. Writing the final model
// without the old types is left to the caller. Each step is idempotent, so an
// interrupted run is completed by the next reconcile.
func (a *authorizationModelSubroutine) migrateRenamedTypes(ctx context.Context, storeID string, current, final *openfgav1.AuthorizationModel, renames typeRenames) error {
	if current == nil {
		return nil
	}

	finalTypes := map[string]*openfgav1.TypeDefinition{}
	for _, td := range final.GetTypeDefinitions() {
		finalTypes[td.GetType()] = td
	}

	// An old name that is the current type of another API is not left over.
	renames = maps.Clone(renames)
	maps.DeleteFunc(renames, func(legacy string, _ []string) bool {
		return finalTypes[legacy] != nil
	})

	// Any model written before the migration finished still defines an old
	// type, at least the built-in ones present in every org store.
	if !slices.ContainsFunc(current.GetTypeDefinitions(), func(td *openfgav1.TypeDefinition) bool {
		_, ok := renames[td.GetType()]
		return ok
	}) {
		return nil
	}

	log := logger.LoadLoggerFromContext(ctx).MustChildLoggerWithAttributes("store_id", storeID)

	var transitionID string
	for range maxMigrationRounds {
		reader := fga.NewTupleManager(a.fga, storeID, fga.AuthorizationModelIDLatest, log)
		tuples, err := reader.ListWithFilter(ctx, func(t pmcorev1alpha1.Tuple) bool {
			return referencedLegacyType(t, renames) != ""
		})
		if err != nil {
			return fmt.Errorf("listing tuples of renamed types: %w", err)
		}

		copies, originals, err := planTypeMigration(tuples, renames, finalTypes, log)
		if err != nil {
			return err
		}
		if len(originals) == 0 {
			return nil
		}

		if transitionID == "" {
			transitionID, err = a.writeTransitionModel(ctx, storeID, final, finalTypes, renames, originals)
			if err != nil {
				return err
			}
		}

		tm := fga.NewTupleManager(a.fga, storeID, transitionID, log)
		if err := tm.ApplyChunked(ctx, copies); err != nil {
			return fmt.Errorf("writing tuples under the new type names: %w", err)
		}
		if err := tm.DeleteChunked(ctx, originals); err != nil {
			return fmt.Errorf("deleting tuples under the old type names: %w", err)
		}
		log.Info().Int("count", len(originals)).Msg("moved tuples of renamed FGA types")
	}

	return fmt.Errorf("tuples of renamed types are still being written after %d migration rounds", maxMigrationRounds)
}

// planTypeMigration returns the rewritten copies and the originals to delete.
// Tuples on an old type whose relation the new type no longer defines grant
// nothing under the new model and are left in place.
func planTypeMigration(tuples []pmcorev1alpha1.Tuple, renames typeRenames, finalTypes map[string]*openfgav1.TypeDefinition, log *logger.Logger) ([]pmcorev1alpha1.Tuple, []pmcorev1alpha1.Tuple, error) {
	var errs []error
	var copies, originals []pmcorev1alpha1.Tuple
	for _, t := range tuples {
		legacy := referencedLegacyType(t, renames)
		targets := renames[legacy]
		switch {
		case len(targets) > 1:
			errs = append(errs, fmt.Errorf("type %s was shared by %s, cannot attribute its tuples", legacy, strings.Join(targets, " and ")))
			continue
		case finalTypes[targets[0]] == nil:
			errs = append(errs, fmt.Errorf("type %s is not defined by the model, cannot move the tuples of %s", targets[0], legacy))
			continue
		}

		c := pmcorev1alpha1.Tuple{
			Object:   renameKey(t.Object, renames),
			Relation: t.Relation,
			User:     renameKey(t.User, renames),
		}
		if objectType(c.Object) != objectType(t.Object) {
			if _, ok := finalTypes[targets[0]].GetRelations()[t.Relation]; !ok {
				log.Warn().Str("object", t.Object).Str("relation", t.Relation).Str("user", t.User).
					Msg("relation no longer defined on the renamed type, leaving tuple in place")
				continue
			}
		}
		if len(c.Object) > maxObjectLength {
			errs = append(errs, fmt.Errorf("object %s exceeds %d characters under the new type name", c.Object, maxObjectLength))
			continue
		}

		copies = append(copies, c)
		originals = append(originals, t)
	}

	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("migrating renamed FGA types (%s): %w", legacyTypeRemoveIssue, errors.Join(errs...))
	}
	return copies, originals, nil
}

func (a *authorizationModelSubroutine) writeTransitionModel(ctx context.Context, storeID string, final *openfgav1.AuthorizationModel, finalTypes map[string]*openfgav1.TypeDefinition, renames typeRenames, originals []pmcorev1alpha1.Tuple) (string, error) {
	legacy := map[string]bool{}
	for _, t := range originals {
		legacy[referencedLegacyType(t, renames)] = true
	}

	transition := proto.Clone(final).(*openfgav1.AuthorizationModel)
	for _, old := range slices.Sorted(maps.Keys(legacy)) {
		oldTD := proto.Clone(finalTypes[renames[old][0]]).(*openfgav1.TypeDefinition)
		oldTD.Type = old
		transition.TypeDefinitions = append(transition.TypeDefinitions, oldTD)
	}

	res, err := a.fga.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         storeID,
		TypeDefinitions: transition.TypeDefinitions,
		SchemaVersion:   transition.SchemaVersion,
		Conditions:      transition.Conditions,
	})
	if err != nil {
		return "", fmt.Errorf("writing transition model for renamed FGA types: %w", err)
	}
	return res.GetAuthorizationModelId(), nil
}

// referencedLegacyType returns the old type a tuple references as object or
// user type, or inside a role id (role:<type>/...), or "".
func referencedLegacyType(t pmcorev1alpha1.Tuple, renames typeRenames) string {
	for _, key := range []string{t.Object, t.User} {
		if old := legacyTypeOfKey(key, renames); old != "" {
			return old
		}
	}
	return ""
}

func legacyTypeOfKey(key string, renames typeRenames) string {
	if typ := objectType(key); renames[typ] != nil {
		return typ
	}
	if rest, ok := strings.CutPrefix(key, rolePrefix); ok {
		if typ, _, found := strings.Cut(rest, "/"); found && renames[typ] != nil {
			return typ
		}
	}
	return ""
}

func renameKey(key string, renames typeRenames) string {
	old := legacyTypeOfKey(key, renames)
	if old == "" {
		return key
	}
	if objectType(key) == old {
		return renames[old][0] + strings.TrimPrefix(key, old)
	}
	return rolePrefix + renames[old][0] + strings.TrimPrefix(key, rolePrefix+old)
}

func objectType(key string) string {
	typ, _, _ := strings.Cut(key, ":")
	return typ
}
