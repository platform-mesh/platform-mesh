package subroutine

import (
	"context"
	"fmt"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/openmfp/fga-operator/api/v1alpha1"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/errors"
	"github.com/openmfp/golang-commons/logger"
	"google.golang.org/protobuf/encoding/protojson"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const schemaVersion = "1.2"

type authorizationModelSubroutine struct {
	fga          openfgav1.OpenFGAServiceClient
	k8s          client.Client
	lcClientFunc NewLogicalClusterClientFunc
}

func NewAuthorizationModelSubroutine(fga openfgav1.OpenFGAServiceClient, k8s client.Client, lcClientFunc NewLogicalClusterClientFunc) *authorizationModelSubroutine {
	return &authorizationModelSubroutine{
		fga:          fga,
		k8s:          k8s,
		lcClientFunc: lcClientFunc,
	}
}

var _ lifecycle.Subroutine = &authorizationModelSubroutine{}

func (a *authorizationModelSubroutine) Finalizers() []string { return nil }

func (a *authorizationModelSubroutine) GetName() string { return "AuthorizationModel" }

func (a *authorizationModelSubroutine) Finalize(ctx context.Context, instance lifecycle.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

type NewLogicalClusterClientFunc func(clusterKey logicalcluster.Name) (client.Client, error)

func getRelatedAuthorizationModels(ctx context.Context, k8s client.Client, store *v1alpha1.Store, lcCLientFunc NewLogicalClusterClientFunc) (v1alpha1.AuthorizationModelList, error) {

	storeClusterKey, ok := kontext.ClusterFrom(ctx)
	if !ok {
		return v1alpha1.AuthorizationModelList{}, fmt.Errorf("unable to get cluster key from context")
	}

	lcClient, err := lcCLientFunc(storeClusterKey)
	if err != nil {
		return v1alpha1.AuthorizationModelList{}, err
	}

	var lc kcpcorev1alpha1.LogicalCluster
	err = lcClient.Get(ctx, client.ObjectKey{Name: "cluster"}, &lc)
	if err != nil {
		return v1alpha1.AuthorizationModelList{}, err
	}

	storeWorkspacePath := lc.Annotations["kcp.io/path"]

	allCtx := kontext.WithCluster(ctx, "")
	allAuthorizationModels := v1alpha1.AuthorizationModelList{}

	if err := k8s.List(allCtx, &allAuthorizationModels); err != nil {
		return v1alpha1.AuthorizationModelList{}, err
	}

	var extendingModules v1alpha1.AuthorizationModelList
	for _, model := range allAuthorizationModels.Items {
		if model.Spec.StoreRef.Name != store.Name || model.Spec.StoreRef.Path != storeWorkspacePath {
			continue
		}

		extendingModules.Items = append(extendingModules.Items, model)
	}

	return extendingModules, nil
}

func (a *authorizationModelSubroutine) Process(ctx context.Context, instance lifecycle.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	store := instance.(*v1alpha1.Store)

	extendingModules, err := getRelatedAuthorizationModels(ctx, a.k8s, store, a.lcClientFunc)
	if err != nil {
		log.Error().Err(err).Msg("unable to get related authorization models")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
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

	authorizationModel, err := language.TransformModuleFilesToModel(moduleFiles, schemaVersion)
	if err != nil {
		log.Error().Err(err).Msg("unable to transform module files to model")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	if store.Status.AuthorizationModelID != "" {
		res, err := a.fga.ReadAuthorizationModel(ctx, &openfgav1.ReadAuthorizationModelRequest{
			StoreId: store.Status.StoreID,
			Id:      store.Status.AuthorizationModelID,
		})
		if err != nil {
			// TODO: if its not found we could continue with just writing the model again
			log.Error().Err(err).Msg("unable to read authorization model")
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		// the following ignore comments are due to the fact, that its incredibly hard to setup a specific condition where one of the parsing methods would fail

		currentRaw, err := protojson.Marshal(res.AuthorizationModel)
		if err != nil { // coverage-ignore
			log.Error().Err(err).Msg("unable to marshal current model")
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		current, err := language.TransformJSONStringToDSL(string(currentRaw))
		if err != nil { // coverage-ignore
			log.Error().Err(err).Msg("unable to transform current model to dsl")
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		desiredRaw, err := protojson.Marshal(authorizationModel)
		if err != nil { // coverage-ignore
			log.Error().Err(err).Msg("unable to marshal desired model")
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		desired, err := language.TransformJSONStringToDSL(string(desiredRaw))
		if err != nil { // coverage-ignore
			log.Error().Err(err).Msg("unable to transform desired model to dsl")
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		if *current == *desired {
			return ctrl.Result{}, nil
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
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	store.Status.AuthorizationModelID = res.AuthorizationModelId

	return ctrl.Result{}, nil
}
