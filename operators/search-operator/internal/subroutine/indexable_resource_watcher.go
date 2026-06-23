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

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/errors"
	fgamodel "go.platform-mesh.io/golang-commons/fga/model"
	"go.platform-mesh.io/golang-commons/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	searchv1alpha1 "go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/search-operator/internal/metrics"
	"go.platform-mesh.io/search-operator/internal/opensearch"
)

// IndexableResourceWatcherSubroutine watches IndexableResource resources across workspaces
type IndexableResourceWatcherSubroutine struct {
	mgr           mcmanager.Manager
	allClient     client.Client
	orgsClient    client.Client // scoped to root:orgs for Workspace lookups
	osClient      *opensearch.Client
	apiExportName string
	indexPrefix   string
	rootCfg       *rest.Config // base kcp REST config (path stripped) for workspace-scoped clients
}

// NewIndexableResourceWatcherSubroutine creates a new IndexableResource watcher subroutine.
// localCfg must be the admin kcp REST config
func NewIndexableResourceWatcherSubroutine(mgr mcmanager.Manager, allClient client.Client, orgsClient client.Client, osClient *opensearch.Client, apiExportName string, indexPrefix string, localCfg *rest.Config) (*IndexableResourceWatcherSubroutine, error) {
	rootCfg, err := stripPathFromConfig(localCfg)
	if err != nil {
		return nil, err
	}

	return &IndexableResourceWatcherSubroutine{
		mgr:           mgr,
		allClient:     allClient,
		orgsClient:    orgsClient,
		osClient:      osClient,
		apiExportName: apiExportName,
		indexPrefix:   indexPrefix,
		rootCfg:       rootCfg,
	}, nil
}

var _ lifecyclesubroutine.Subroutine = &IndexableResourceWatcherSubroutine{}

const (
	indexableResourceFinalizer = "search.platform-mesh.io/indexable-resource"
	kcpClusterAnnotation       = "kcp.io/cluster"
)

func (s *IndexableResourceWatcherSubroutine) GetName() string {
	return "IndexableResourceWatcher"
}

// Finalizers return the finalizers this subroutine manages
func (s *IndexableResourceWatcherSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{indexableResourceFinalizer}
}

func (s *IndexableResourceWatcherSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (result ctrl.Result, opErr errors.OperatorError) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if opErr != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(s.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(s.GetName()).Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx)
	resource := instance.(*unstructured.Unstructured)

	clusterID, workspacePath, err := getWorkspaceClusterAndPath(ctx, s.mgr)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	orgName, err := extractOrgFromPath(workspacePath)
	if err != nil {
		log.Debug().Msg("Not in an org workspace, skipping")
		return ctrl.Result{}, nil
	}

	orgID, err := getOrgClusterID(ctx, s.orgsClient, orgName)
	if err != nil {
		log.Debug().Err(err).Msg("org ID not found, will retry")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	consumerCluster, err := s.mgr.GetCluster(ctx, clusterID)
	if err != nil {
		log.Debug().Err(err).Msg("could not get consumer cluster, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	m, err := consumerCluster.GetRESTMapper().RESTMapping(resource.GroupVersionKind().GroupKind())
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve plural resource via RESTMapper, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	pluralResource := m.Resource.Resource

	searchIndex, err := getSearchIndex(ctx, s.orgsClient, orgID, pluralResource, s.indexPrefix)
	if err != nil {
		log.Debug().Err(err).Msg("could not get SearchIndex, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if searchIndex.Status.IndexName == "" {
		log.Debug().Str("orgID", orgID).Msg("SearchIndex has no IndexName yet, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	indexName := searchIndex.Status.IndexName

	resourceClusterID := resolveResourceClusterID(resource, clusterID)
	docID := s.generateDocumentID(resource, resourceClusterID)
	gvk := resource.GroupVersionKind()

	doc := opensearch.NewResourceDocument(
		docID,
		resource.GetKind(),
		resource.GetName(),
		resource.GetNamespace(),
		resourceClusterID,
		workspacePath,
	)
	doc.APIGroup = gvk.Group
	doc.APIVersion = gvk.Version
	doc.OrganizationName = orgName
	doc.OrganizationID = orgID
	doc.CustomFields = extractCustomFields(resource, searchIndex.Spec.DefaultFields)
	semanticFieldValues := extractConfiguredFields(resource, searchIndex.Spec.SemanticFields)

	accountInfo, err := s.getAccountInfo(ctx, workspacePath, gvk, resource)
	if err != nil {
		log.Warn().Err(err).
			Str("workspacePath", workspacePath).
			Msg("AccountInfo path-based lookup failed, requeuing")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// Not all APIResources have an AccountInfo directly associated with them, but there is always a Parent Account or Org that has an AccountInfo
	if accountInfo == nil {
		accountInfo = s.getParentAccountInfo(ctx, log, resource, clusterID, resourceClusterID)
	}

	if accountInfo.Spec.Account.Name == "" || accountInfo.Spec.Account.OriginClusterId == "" ||
		accountInfo.Spec.Organization.Name == "" || accountInfo.Spec.Organization.OriginClusterId == "" {
		log.Warn().Msg("AccountInfo is missing required account/organization origin metadata, requeuing")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	fgaGroup, fgaKind, fgaClusterID := mapResourceToFGAObject(gvk.Group, gvk.Kind, resourceClusterID, accountInfo)
	doc.FGAObject = buildFGAObjectName(fgaGroup, fgaKind, fgaClusterID.String(), resource.GetName(), resource.GetNamespace())

	// Contextual Tuples (Permissions field), build parent hierarchy from AccountInfo
	orgObject := buildFGAObjectName(searchv1alpha1.GroupName, searchv1alpha1.AccountKind, accountInfo.Spec.Organization.OriginClusterId, accountInfo.Spec.Organization.Name, "")
	accountObject := buildFGAObjectName(corev1alpha1.GroupVersion.Group, searchv1alpha1.AccountKind, accountInfo.Spec.Account.OriginClusterId, accountInfo.Spec.Account.Name, "")
	doc.AccountName = accountInfo.Spec.Account.Name
	doc.AccountID = accountInfo.Spec.Account.OriginClusterId

	isOrganization := gvk.Group == searchv1alpha1.GroupName && gvk.Kind == searchv1alpha1.OrganizationKind
	parentObject := accountObject
	if isOrganization {
		parentObject = orgObject
	}

	namespaceClusterID := resourceClusterID
	if generatedClusterID := strings.TrimSpace(accountInfo.Spec.Account.GeneratedClusterId); generatedClusterID != "" {
		namespaceClusterID = multicluster.ClusterName(generatedClusterID)
	}

	if ns := resource.GetNamespace(); ns != "" {
		// Namespaced resource: Resource -> Namespace -> Parent
		nsObject := buildFGAObjectName("", "Namespace", namespaceClusterID.String(), ns, "")
		addParentPermissions(doc, fgamodel.BuildParentTuples(parentObject, doc.FGAObject, &nsObject))
	} else if doc.FGAObject != parentObject {
		// Cluster-scoped resource: direct link to its logical parent (Account or Org)
		addParentPermissions(doc, fgamodel.BuildParentTuples(parentObject, doc.FGAObject, nil))
	}

	payloadRawJSON, payloadText, payloadErr := buildPayload(resource)
	if payloadErr != nil {
		return ctrl.Result{}, errors.NewOperatorError(
			fmt.Errorf("failed to build payload for %s/%s: %w", resource.GetKind(), resource.GetName(), payloadErr),
			true,
			false,
		)
	}
	doc.PayloadRawJSON = payloadRawJSON
	doc.PayloadText = payloadText

	documentBody, err := buildDocumentSource(doc, semanticFieldValues)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(
			fmt.Errorf("failed to build document source for %s: %w", docID, err), true, false,
		)
	}

	if err := s.osClient.IndexDocument(ctx, indexName, docID, documentBody); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(
			fmt.Errorf("failed to index document %s: %w", docID, err), true, false,
		)
	}

	log.Info().
		Str("docID", docID).
		Str("index", indexName).
		Str("kind", resource.GetKind()).
		Msg("indexed document")

	return ctrl.Result{}, nil
}

func (s *IndexableResourceWatcherSubroutine) getParentAccountInfo(ctx context.Context, log *logger.Logger, resource *unstructured.Unstructured, clusterID, resourceClusterID multicluster.ClusterName) *corev1alpha1.AccountInfo {
	accountInfo := corev1alpha1.AccountInfo{}
	accountInfoLookupClusters := resolveAccountInfoLookupClusters(resource, clusterID, resourceClusterID)
	for _, candidateClusterID := range accountInfoLookupClusters {
		cluster, getClusterErr := s.mgr.GetCluster(ctx, candidateClusterID)
		if getClusterErr != nil {
			log.Warn().
				Err(getClusterErr).
				Str("candidateClusterID", candidateClusterID.String()).
				Msg("failed to get candidate cluster for AccountInfo lookup")
			continue
		}

		clusterClient := cluster.GetClient()
		lookupCtx := mccontext.WithCluster(ctx, candidateClusterID)
		getAccountInfoErr := clusterClient.Get(lookupCtx, client.ObjectKey{Name: "account"}, &accountInfo)
		if getAccountInfoErr == nil {
			break
		}
		if apierrors.IsNotFound(getAccountInfoErr) {
			retryErr := clusterClient.Get(ctx, client.ObjectKey{Name: "account"}, &accountInfo)
			if retryErr == nil {
				break
			}
			if apierrors.IsNotFound(retryErr) {
				log.Debug().
					Str("candidateClusterID", candidateClusterID.String()).
					Msg("AccountInfo not found in candidate cluster")
				continue
			}
			log.Warn().
				Err(retryErr).
				Str("candidateClusterID", candidateClusterID.String()).
				Msg("failed to get AccountInfo from candidate cluster on retry")
			continue
		}

		log.Warn().
			Err(getAccountInfoErr).
			Str("candidateClusterID", candidateClusterID.String()).
			Msg("failed to get AccountInfo from candidate cluster")
	}

	return &accountInfo
}

// Returns the AccountInfo for the given resource if it is an Account or Organization, otherwise returns nil.
func (s *IndexableResourceWatcherSubroutine) getAccountInfo(ctx context.Context, workspacePath string, gvk schema.GroupVersionKind, resource *unstructured.Unstructured) (*corev1alpha1.AccountInfo, error) {
	if gvk.Group == searchv1alpha1.GroupName && (gvk.Kind == searchv1alpha1.AccountKind || gvk.Kind == searchv1alpha1.OrganizationKind) {
		// account and organization are both FGA account objects with the AccountInfo
		// in their own child workspace, use a direct lookup based on the current workspace path
		accountWorkspacePath := workspacePath + ":" + resource.GetName()
		ai, pathErr := s.getAccountInfoFromWorkspacePath(ctx, accountWorkspacePath)
		if pathErr != nil {
			return nil, fmt.Errorf("account info not found at path %q: %w", accountWorkspacePath, pathErr)
		}
		return ai, nil
	}
	return nil, nil
}

func getSearchIndex(ctx context.Context, orgsClient client.Client, orgID string, pluralResource string, indexPrefix string) (*searchv1alpha1.SearchIndex, error) {
	searchIndex := &searchv1alpha1.SearchIndex{}
	name := buildCanonicalIndexName(indexPrefix, orgID, pluralResource)
	if err := orgsClient.Get(ctx, types.NamespacedName{Name: name}, searchIndex); err != nil {
		return nil, fmt.Errorf("failed to get SearchIndex %q: %w", name, err)
	}
	return searchIndex, nil
}

// extractCustomFields copies only the top-level fields listed in defaultFields
// from the unstructured resource object. Fields not present in the resource are skipped.
func extractCustomFields(resource *unstructured.Unstructured, defaultFields []string) map[string]any {
	if len(defaultFields) == 0 {
		return nil
	}
	out := make(map[string]any, len(defaultFields))
	for _, field := range defaultFields {
		if v, ok := resource.Object[field]; ok {
			out[field] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractConfiguredFields resolves field paths from the unstructured resource object.
// Dotted paths are traversed as nested maps.
func extractConfiguredFields(resource *unstructured.Unstructured, fieldPaths []string) map[string]any {
	if len(fieldPaths) == 0 {
		return nil
	}

	out := make(map[string]any, len(fieldPaths))
	for _, fieldPath := range fieldPaths {
		if value, ok := lookupFieldPath(resource.Object, fieldPath); ok {
			out[fieldPath] = value
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func lookupFieldPath(obj map[string]any, fieldPath string) (any, bool) {
	segments := opensearchSplitFieldPath(fieldPath)
	if len(segments) == 0 {
		return nil, false
	}

	var current any = obj
	for _, segment := range segments {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[segment]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

func buildDocumentSource(doc *opensearch.ResourceDocument, configuredFields map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal resource document: %w", err)
	}

	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("unmarshal resource document: %w", err)
	}

	for fieldPath, value := range configuredFields {
		if err := setFieldPath(source, fieldPath, value); err != nil {
			return nil, err
		}
	}

	return source, nil
}

func setFieldPath(target map[string]any, fieldPath string, value any) error {
	segments := opensearchSplitFieldPath(fieldPath)
	if len(segments) == 0 {
		return nil
	}

	current := target
	for i, segment := range segments {
		isLeaf := i == len(segments)-1
		if isLeaf {
			current[segment] = value
			return nil
		}

		next, exists := current[segment]
		if !exists || next == nil {
			nextMap := map[string]any{}
			current[segment] = nextMap
			current = nextMap
			continue
		}

		nextMap, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("field path %q conflicts with non-object segment %q", fieldPath, segment)
		}
		current = nextMap
	}

	return nil
}

func opensearchSplitFieldPath(fieldPath string) []string {
	rawSegments := strings.Split(strings.TrimSpace(fieldPath), ".")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}

func (s *IndexableResourceWatcherSubroutine) generateDocumentID(
	resource *unstructured.Unstructured,
	clusterName multicluster.ClusterName,
) string {
	namespace := resource.GetNamespace()
	if namespace == "" {
		namespace = "_cluster"
	}

	return fmt.Sprintf("%s-%s-%s-%s",
		clusterName,
		namespace,
		resource.GetKind(),
		resource.GetName(),
	)
}

func buildPayload(resource *unstructured.Unstructured) (string, string, error) {
	raw := resource.DeepCopy().Object
	if metadata, ok := raw["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return "", "", err
	}

	yamlBytes, err := yaml.Marshal(raw)
	if err != nil {
		yamlBytes = jsonBytes
	}

	return string(jsonBytes), string(yamlBytes), nil
}

func mapResourceToFGAObject(group, kind string, clusterID multicluster.ClusterName, accountInfo *corev1alpha1.AccountInfo) (fgaGroup, fgaKind string, fgaClusterID multicluster.ClusterName) {
	fgaGroup = group
	fgaKind = kind
	fgaClusterID = clusterID

	isAccount := group == searchv1alpha1.GroupName && kind == searchv1alpha1.AccountKind
	isOrganization := group == searchv1alpha1.GroupName && kind == searchv1alpha1.OrganizationKind
	isWorkspace := group == "tenancy.kcp.io" && kind == "Workspace"
	if isAccount || isWorkspace || isOrganization {
		fgaGroup = searchv1alpha1.GroupName
		fgaKind = searchv1alpha1.AccountKind
		if accountInfo != nil {
			switch {
			case isOrganization:
				if accountInfo.Spec.Organization.OriginClusterId != "" {
					fgaClusterID = multicluster.ClusterName(accountInfo.Spec.Organization.OriginClusterId)
				}
			case isAccount, isWorkspace:
				if accountInfo.Spec.Account.OriginClusterId != "" {
					fgaClusterID = multicluster.ClusterName(accountInfo.Spec.Account.OriginClusterId)
				}
			}
		}
	}

	return fgaGroup, fgaKind, fgaClusterID
}

func resolveAccountInfoLookupClusters(resource *unstructured.Unstructured, contextClusterID, resourceClusterID multicluster.ClusterName) []multicluster.ClusterName {
	seen := sets.New[multicluster.ClusterName]()
	candidates := []multicluster.ClusterName{resourceClusterID, contextClusterID, resolveSpecClusterID(resource)}
	result := []multicluster.ClusterName{}

	// Notably, it's important to keep the order of candidates, as we're building up a lookup order,
	// otherwise this would just be `return sets.List(sets.New(candidates))`.
	for _, candidate := range candidates {
		if !seen.Has(candidate) {
			seen.Insert(candidate)
			result = append(result, candidate)
		}
	}

	return result
}

func resolveSpecClusterID(resource *unstructured.Unstructured) multicluster.ClusterName {
	if resource == nil {
		return ""
	}
	spec, ok := resource.Object["spec"].(map[string]any)
	if !ok {
		return ""
	}
	raw, ok := spec["cluster"]
	if !ok || raw == nil {
		return ""
	}
	clusterID, ok := raw.(string)
	if !ok {
		return ""
	}

	return multicluster.ClusterName(strings.TrimSpace(clusterID))
}

// getAccountInfoFromWorkspacePath builds a workspace-scoped REST client from the base kcp
// config and fetches the singleton AccountInfo named "account" from that workspace.
func (s *IndexableResourceWatcherSubroutine) getAccountInfoFromWorkspacePath(ctx context.Context, accountWorkspacePath string) (*corev1alpha1.AccountInfo, error) {
	cl, err := buildWorkspaceScopedClient(s.rootCfg, s.mgr.GetLocalManager().GetScheme(), accountWorkspacePath)
	if err != nil {
		return nil, fmt.Errorf("create scoped client for %q: %w", accountWorkspacePath, err)
	}

	accountInfo := &corev1alpha1.AccountInfo{}
	if err := cl.Get(ctx, client.ObjectKey{Name: "account"}, accountInfo); err != nil {
		return nil, fmt.Errorf("get AccountInfo from %q: %w", accountWorkspacePath, err)
	}

	return accountInfo, nil
}

func (s *IndexableResourceWatcherSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	resource := instance.(*unstructured.Unstructured)

	clusterID, workspacePath, err := getWorkspaceClusterAndPath(ctx, s.mgr)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	orgName, err := extractOrgFromPath(workspacePath)
	if err != nil {
		log.Debug().Msg("Not in an org workspace, skipping")
		return ctrl.Result{}, nil
	}

	orgID, err := getOrgClusterID(ctx, s.orgsClient, orgName)
	if err != nil {
		log.Debug().Err(err).Msg("Workspace not found, will retry")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	consumerCluster, err := s.mgr.GetCluster(ctx, clusterID)
	if err != nil {
		log.Debug().Err(err).Msg("could not get consumer cluster, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	m, err := consumerCluster.GetRESTMapper().RESTMapping(resource.GroupVersionKind().GroupKind())
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve plural resource via RESTMapper, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	pluralResource := m.Resource.Resource

	searchIndex, err := getSearchIndex(ctx, s.orgsClient, orgID, pluralResource, s.indexPrefix)
	if err != nil {
		log.Debug().Err(err).Msg("could not get SearchIndex, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if searchIndex.Status.IndexName == "" {
		log.Warn().Str("orgID", orgID).Msg("SearchIndex has no IndexName, cannot delete document")
		return ctrl.Result{}, nil
	}
	indexName := searchIndex.Status.IndexName

	resourceClusterID := resolveResourceClusterID(resource, clusterID)
	docID := s.generateDocumentID(resource, resourceClusterID)
	if err := s.osClient.DeleteDocument(ctx, indexName, docID); err != nil {
		log.Error().Err(err).Msg("failed to delete document from OpenSearch")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	log.Info().
		Str("docID", docID).
		Str("index", indexName).
		Msg("deleted document from index")

	return ctrl.Result{}, nil
}

func buildFGAObjectName(group, kind, clusterID, name, namespace string) string {
	var namespacePtr *string
	if namespace != "" {
		namespacePtr = &namespace
	}

	// TODO rebac-auth-webhook uses singular resource names as the canonical basis for
	// OpenFGA object types. For our current resources, lowercase Kind matches the
	// singular form while keeping output stable.
	return fgamodel.BuildObjectName(group, strings.ToLower(kind), clusterID, name, namespacePtr)
}

func resolveResourceClusterID(resource *unstructured.Unstructured, fallbackClusterID multicluster.ClusterName) multicluster.ClusterName {
	if v := resource.GetAnnotations()[kcpClusterAnnotation]; strings.TrimSpace(v) != "" {
		return multicluster.ClusterName(v)
	}

	return fallbackClusterID
}

func addParentPermissions(doc *opensearch.ResourceDocument, tuples []*openfgav1.TupleKey) {
	for _, tuple := range tuples {
		if tuple == nil {
			continue
		}

		doc.AddPermission(tuple.User, tuple.Relation, tuple.Object)
	}
}
