package subroutine

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/util"
	"go.platform-mesh.io/subroutines"
	"google.golang.org/protobuf/encoding/protojson"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

const (
	schemaVersion = "1.2"
)

var (
	privilegedGroupVersions = []string{"rbac.authorization.k8s.io/v1"}
	groupVersions           = []string{"authentication.k8s.io/v1", "authorization.k8s.io/v1", "v1", "apis.kcp.io/v1alpha1", "ui.platform-mesh.io/v1alpha1"}

	privilegedTemplate = template.Must(template.New("model").Parse(`module internal_core_types_{{ .Name }}

{{ if eq .Scope "Cluster" }}
extend type core_platform-mesh_io_account
	relations
		define create_{{ .Group }}_{{ .Name }}: owner
		define list_{{ .Group }}_{{ .Name }}: member
		define watch_{{ .Group }}_{{ .Name }}: member
{{ end }}

{{ if eq .Scope "Namespaced" }}
extend type core_namespace
	relations
		define create_{{ .Group }}_{{ .Name }}: owner
		define list_{{ .Group }}_{{ .Name }}: member
		define watch_{{ .Group }}_{{ .Name }}: member
{{ end }}

type {{ .Group }}_{{ .Singular }}
	relations
		define parent: [{{ if eq .Scope "Namespaced" }}core_namespace{{ else }}core_platform-mesh_io_account{{ end }}]
		define member: [role#assignee] or owner or member from parent
		define owner: [role#assignee] or owner from parent

		define get: member
		define update: owner
		define delete: owner
		define patch: owner
		define watch: member
		define bind: owner
		define escalate: owner

		define manage_iam_roles: owner
		define get_iam_roles: member
		define get_iam_users: member
`))
)

type NewDiscoveryClientFunc func(cfg *rest.Config) discovery.DiscoveryInterface

type authorizationModelSubroutine struct {
	fga                    openfgav1.OpenFGAServiceClient
	mgr                    mcmanager.Manager
	lister                 iclient.Lister
	newDiscoveryClientFunc NewDiscoveryClientFunc
}

func NewAuthorizationModelSubroutine(fga openfgav1.OpenFGAServiceClient, mgr mcmanager.Manager, lister iclient.Lister, newDiscoveryClientFunc NewDiscoveryClientFunc, log *logger.Logger) *authorizationModelSubroutine {
	return &authorizationModelSubroutine{
		fga:                    fga,
		mgr:                    mgr,
		lister:                 lister,
		newDiscoveryClientFunc: newDiscoveryClientFunc,
	}
}

var _ subroutines.Processor = &authorizationModelSubroutine{}

func (a *authorizationModelSubroutine) GetName() string { return "AuthorizationModel" }

func getRelatedAuthorizationModels(ctx context.Context, lister iclient.Lister, store *corev1alpha1.Store) (corev1alpha1.AuthorizationModelList, error) {
	storeClusterKey, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return corev1alpha1.AuthorizationModelList{}, fmt.Errorf("unable to get cluster key from context")
	}

	allAuthorizationModels := corev1alpha1.AuthorizationModelList{}
	if err := lister.List(ctx, &allAuthorizationModels); err != nil {
		return corev1alpha1.AuthorizationModelList{}, err
	}

	var extendingModules corev1alpha1.AuthorizationModelList
	for _, model := range allAuthorizationModels.Items {
		if model.Spec.StoreRef.Name != store.Name || model.Spec.StoreRef.Cluster != string(storeClusterKey) {
			continue
		}

		extendingModules.Items = append(extendingModules.Items, model)
	}
	return extendingModules, nil
}

func (a *authorizationModelSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	store := obj.(*corev1alpha1.Store)

	extendingModules, err := getRelatedAuthorizationModels(ctx, a.lister, store)
	if err != nil {
		log.Error().Err(err).Msg("unable to get related authorization models")
		return subroutines.OK(), err
	}

	moduleFiles := []language.ModuleFile{{
		Name:     fmt.Sprintf("%s.fga", client.ObjectKeyFromObject(store)),
		Contents: store.Spec.CoreModule,
	}}
	for _, module := range extendingModules.Items {
		moduleFiles = append(moduleFiles, language.ModuleFile{
			Name:     fmt.Sprintf("%s.fga", client.ObjectKeyFromObject(&module)),
			Contents: module.Spec.Model,
		})
	}

	if store.Name != "orgs" {
		cfg := rest.CopyConfig(a.mgr.GetLocalManager().GetConfig())

		parsed, err := url.Parse(cfg.Host)
		if err != nil {
			log.Error().Err(err).Msg("unable to parse host from config")
			return subroutines.OK(), err
		}

		parsed.Path, err = url.JoinPath("clusters", fmt.Sprintf("root:orgs:%s", store.Name))
		if err != nil {
			log.Error().Err(err).Msg("unable to join path")
			return subroutines.OK(), err
		}

		cfg.Host = parsed.String()

		discoveryClient := a.newDiscoveryClientFunc(cfg)

		coreModules, err := discoverAndRender(discoveryClient, modelTpl, groupVersions)
		if err != nil {
			return subroutines.OK(), err
		}
		moduleFiles = append(moduleFiles, coreModules...)

		privilegedModules, err := discoverAndRender(discoveryClient, privilegedTemplate, privilegedGroupVersions)
		if err != nil {
			return subroutines.OK(), err
		}
		moduleFiles = append(moduleFiles, privilegedModules...)
	}

	authorizationModel, err := language.TransformModuleFilesToModel(moduleFiles, schemaVersion)
	if err != nil {
		log.Error().Err(err).Msg("unable to transform module files to model")
		return subroutines.OK(), err
	}

	if store.Status.AuthorizationModelID != "" {
		res, err := a.fga.ReadAuthorizationModel(ctx, &openfgav1.ReadAuthorizationModelRequest{
			StoreId: store.Status.StoreID,
			Id:      store.Status.AuthorizationModelID,
		})
		if err != nil {
			log.Error().Err(err).Msg("unable to read authorization model")
			return subroutines.OK(), err
		}

		res.AuthorizationModel.Id = ""

		currentRaw, err := protojson.Marshal(res.AuthorizationModel)
		if err != nil {
			log.Error().Err(err).Msg("unable to marshal current model")
			return subroutines.OK(), err
		}

		desiredRaw, err := protojson.Marshal(authorizationModel)
		if err != nil {
			log.Error().Err(err).Msg("unable to marshal desired model")
			return subroutines.OK(), err
		}

		if string(currentRaw) == string(desiredRaw) {
			return subroutines.OK(), nil
		}

	}

	res, err := a.fga.WriteAuthorizationModel(ctx, &openfgav1.WriteAuthorizationModelRequest{
		StoreId:         store.Status.StoreID,
		TypeDefinitions: authorizationModel.TypeDefinitions,
		SchemaVersion:   schemaVersion,
		Conditions:      authorizationModel.Conditions,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to write authorization model")
		return subroutines.OK(), err
	}

	store.Status.AuthorizationModelID = res.AuthorizationModelId

	return subroutines.OK(), nil
}

func processAPIResourceIntoModel(resource metav1.APIResource, tpl *template.Template) (bytes.Buffer, error) {

	scope := apiextensionsv1.ClusterScoped
	if resource.Namespaced {
		scope = apiextensionsv1.NamespaceScoped
	}

	group := "core"
	if resource.Group != "" {
		group = util.CapGroupToRelationLength(schema.GroupVersionResource{Group: resource.Group, Resource: resource.Name}, 50)
	}

	var buffer bytes.Buffer
	err := tpl.Execute(&buffer, modelInput{
		Name:     resource.Name,
		Group:    strings.ReplaceAll(group, ".", "_"), // TODO: group name length capping
		Singular: resource.SingularName,
		Scope:    string(scope),
	})
	if err != nil {
		return buffer, err
	}

	return buffer, nil
}

func discoverAndRender(dc discovery.DiscoveryInterface, tpl *template.Template, groupVersions []string) ([]language.ModuleFile, error) {
	var files []language.ModuleFile
	for _, gv := range groupVersions {
		resourceList, err := dc.ServerResourcesForGroupVersion(gv)
		if err != nil {
			return nil, fmt.Errorf("discover resources for %s: %w", gv, err)
		}

		parsedGV, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("parse group version %s: %w", resourceList.GroupVersion, err)
		}

		for _, apiRes := range resourceList.APIResources {
			if strings.Contains(apiRes.Name, "/") { // skip subresources
				continue
			}

			if parsedGV.Group != "" && apiRes.Group == "" {
				apiRes.Group = parsedGV.Group
			}

			buf, err := processAPIResourceIntoModel(apiRes, tpl)
			if err != nil {
				return nil, fmt.Errorf("process api resource %s in %s: %w", apiRes.Name, gv, err)
			}

			files = append(files, language.ModuleFile{
				Name:     fmt.Sprintf("internal_core_types_%s.fga", apiRes.Name),
				Contents: buf.String(),
			})
		}
	}
	return files, nil
}
