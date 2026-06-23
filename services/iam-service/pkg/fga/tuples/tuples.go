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

package tuples

import (
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/fga/util"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

func GenerateContextualTuples(rctx *graph.ResourceContext, ai *accountsv1alpha1.AccountInfo) *openfgav1.ContextualTupleKeys {
	tuples := &openfgav1.ContextualTupleKeys{}

	accFGATypeName := util.ConvertToTypeName("core.platform-mesh.io", "Account")
	accObject := fmt.Sprintf("%s:%s/%s", accFGATypeName, ai.Spec.Account.OriginClusterId, ai.Spec.Account.Name)

	var nsObject string
	if rctx.Resource.Namespace != nil {
		nsFGATypeName := util.ConvertToTypeName("", "Namespace")
		nsObject = fmt.Sprintf("%s:%s/%s", nsFGATypeName, ai.Spec.Account.GeneratedClusterId, *rctx.Resource.Namespace)

		// Add namespace contextual tuple
		namespaceTuple := &openfgav1.TupleKey{
			Object:   nsObject,
			Relation: "parent",
			User:     accObject,
		}
		tuples.TupleKeys = append(tuples.TupleKeys, namespaceTuple)
	}

	if !managedTuple(rctx.Group, rctx.Kind) {
		resFGATypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)
		var resObject string
		if rctx.Resource.Namespace != nil {
			resObject = fmt.Sprintf("%s:%s/%s/%s", resFGATypeName, ai.Spec.Account.GeneratedClusterId, *rctx.Resource.Namespace, rctx.Resource.Name)
		} else {
			resObject = fmt.Sprintf("%s:%s/%s", resFGATypeName, ai.Spec.Account.GeneratedClusterId, rctx.Resource.Name)
		}

		resTuple := &openfgav1.TupleKey{
			Object:   resObject,
			Relation: "parent",
		}
		if rctx.Resource.Namespace != nil {
			resTuple.User = nsObject
		} else {
			resTuple.User = accObject
		}
		tuples.TupleKeys = append(tuples.TupleKeys, resTuple)
	}

	return tuples
}

func managedTuple(group, kind string) bool {
	switch strings.ToLower(group) {
	case "core.platform-mesh.io":
		switch strings.ToLower(kind) {
		case "account":
			return true
		}
	}
	return false
}
