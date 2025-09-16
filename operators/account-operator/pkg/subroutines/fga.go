package subroutines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/fga/helpers"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

type FGASubroutine struct {
	fgaClient       openfgav1.OpenFGAServiceClient
	client          client.Client
	objectType      string
	parentRelation  string
	creatorRelation string
	limiter         workqueue.TypedRateLimiter[ClusteredName]
}

func NewFGASubroutine(cl client.Client, fgaClient openfgav1.OpenFGAServiceClient, creatorRelation, parentRealtion, objectType string) *FGASubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &FGASubroutine{
		client:          cl,
		fgaClient:       fgaClient,
		creatorRelation: creatorRelation,
		parentRelation:  parentRealtion,
		objectType:      objectType,
		limiter:         exp,
	}
}

func (e *FGASubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	account := ro.(*v1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)

	log := logger.LoadLoggerFromContext(ctx)
	log.Debug().Msg("Starting creator subroutine process() function")

	accountWorkspace, err := retrieveWorkspace(ctx, account, e.client, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		next := e.limiter.When(cn)
		return ctrl.Result{RequeueAfter: next}, nil
	}

	// Prepare context to work in workspace
	wsCtx := kontext.WithCluster(ctx, logicalcluster.Name(accountWorkspace.Spec.Cluster))

	accountInfo, err := e.getAccountInfo(wsCtx)
	if err != nil {
		log.Error().Err(err).Msg("Couldn't get Store Id")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if accountInfo.Spec.FGA.Store.Id == "" {
		log.Error().Msg("FGA Store Id is empty")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("FGA Store Id is empty"), true, true)
	}

	if accountInfo.Spec.Account.GeneratedClusterId == "" {
		log.Error().Msg("account cluster id is empty")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("account cluster id is empty"), true, true)
	}

	if account.Spec.Type != v1alpha1.AccountTypeOrg && accountInfo.Spec.ParentAccount.GeneratedClusterId == "" {
		log.Error().Msg("parent account cluster id is empty")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("parent account cluster id is empty"), true, true)
	}

	writes := []*openfgav1.TupleKey{}

	// Parent Name
	if account.Spec.Type != v1alpha1.AccountTypeOrg {
		parentAccountName := accountInfo.Spec.ParentAccount.Name

		// Determine parent account to create parent relation
		writes = append(writes, &openfgav1.TupleKey{
			User:     fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.ParentAccount.OriginClusterId, parentAccountName),
			Relation: e.parentRelation,
			Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.GetName()),
		})
	}

	// Assign creator to the account
	creatorTuplesWritten := meta.IsStatusConditionTrue(account.Status.Conditions, fmt.Sprintf("%s_Ready", e.GetName()))
	if account.Spec.Creator != nil && !creatorTuplesWritten {
		if valid := validateCreator(*account.Spec.Creator); !valid {
			log.Error().Err(err).Str("creator", *account.Spec.Creator).Msg("creator string is in the protected service account prefix range")
			return ctrl.Result{}, errors.NewOperatorError(err, false, false)
		}
		creator := formatUser(*account.Spec.Creator)

		writes = append(writes, &openfgav1.TupleKey{
			User:     fmt.Sprintf("user:%s", creator),
			Relation: "assignee",
			Object:   fmt.Sprintf("role:%s/%s/%s/owner", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
		})

		writes = append(writes, &openfgav1.TupleKey{
			User:     fmt.Sprintf("role:%s/%s/%s/owner#assignee", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
			Relation: e.creatorRelation,
			Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
		})
	}

	for _, writeTuple := range writes {
		_, err = e.fgaClient.Write(ctx, &openfgav1.WriteRequest{
			StoreId: accountInfo.Spec.FGA.Store.Id,
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{writeTuple},
			},
		})

		if helpers.IsDuplicateWriteError(err) {
			log.Info().Err(err).Msg("Open FGA writeTuple failed due to invalid input (possible duplicate)")
			err = nil
		}

		if err != nil {
			log.Error().Err(err).Msg("Open FGA writeTuple failed")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	e.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (e *FGASubroutine) Finalize(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	account := runtimeObj.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	// Skip fga account finalization for organizations because the store is removed completely
	if account.Spec.Type != v1alpha1.AccountTypeOrg {
		accountInfo, err := e.getAccountInfo(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Couldn't get Store Id")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		if accountInfo.Spec.FGA.Store.Id == "" {
			log.Error().Msg("FGA Store Id is empty")
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("FGA Store Id is empty"), true, true)
		}

		deletes := []*openfgav1.TupleKeyWithoutCondition{}
		if account.Spec.Type != v1alpha1.AccountTypeOrg {
			parentAccountName := accountInfo.Spec.Account.Name

			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.ParentAccount.OriginClusterId, parentAccountName),
				Relation: e.parentRelation,
				Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.GetName()),
			})
		}

		if account.Spec.Creator != nil {
			creator := formatUser(*account.Spec.Creator)
			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("user:%s", creator),
				Relation: "assignee",
				Object:   fmt.Sprintf("role:%s/%s/%s/owner", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
			})

			deletes = append(deletes, &openfgav1.TupleKeyWithoutCondition{
				User:     fmt.Sprintf("role:%s/%s/%s/owner#assignee", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
				Relation: e.creatorRelation,
				Object:   fmt.Sprintf("%s:%s/%s", e.objectType, accountInfo.Spec.Account.OriginClusterId, account.Name),
			})
		}

		for _, deleteTuple := range deletes {

			_, err = e.fgaClient.Write(ctx, &openfgav1.WriteRequest{
				StoreId: accountInfo.Spec.FGA.Store.Id,
				Deletes: &openfgav1.WriteRequestDeletes{
					TupleKeys: []*openfgav1.TupleKeyWithoutCondition{deleteTuple},
				},
			})

			if helpers.IsDuplicateWriteError(err) {
				log.Info().Err(err).Msg("Open FGA write failed due to invalid input (possibly trying to deleteTuple nonexisting entry)")
				err = nil
			}

			if err != nil {
				log.Error().Err(err).Msg("Open FGA write failed")
				return ctrl.Result{}, errors.NewOperatorError(err, true, true)
			}

		}
	}

	return ctrl.Result{}, nil
}

func (e *FGASubroutine) getAccountInfo(ctx context.Context) (*v1alpha1.AccountInfo, error) {
	// Get AccountInfo For Project
	accountInfo := &v1alpha1.AccountInfo{}
	err := e.client.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, accountInfo)
	if err != nil {
		return nil, err
	}
	return accountInfo, nil
}

func (e *FGASubroutine) GetName() string { return "FGASubroutine" }

func (e *FGASubroutine) Finalizers() []string { return []string{"account.core.platform-mesh.io/fga"} }

var saRegex = regexp.MustCompile(`^system:serviceaccount:[^:]*:[^:]*$`)

// formatUser formats the user string to be used in the FGA write request
// it replaces colons for users conforming to the kubernetes service account pattern with dots.
func formatUser(user string) string {
	if saRegex.MatchString(user) {
		return strings.ReplaceAll(user, ":", ".")
	}
	return user
}

// validateCreator validates the creator string to ensure if it is not in the service account prefix range
func validateCreator(creator string) bool {
	return !strings.HasPrefix(creator, "system.serviceaccount")
}
