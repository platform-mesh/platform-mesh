package fga

import (
	"context"
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/metrics"
)

// AuthorizationModelIDLatest is to explicitely acknowledge that no ID means
// latest.
const AuthorizationModelIDLatest = ""

// TupleManager wraps around FGA attributes to write and delete sets of tuples.
type TupleManager struct {
	client               openfgav1.OpenFGAServiceClient
	storeID              string
	authorizationModelID string
	logger               logger.Logger
}

type TupleFilter func(t corev1alpha1.Tuple) bool

func NewTupleManager(client openfgav1.OpenFGAServiceClient, storeID, authorizationModelID string, log *logger.Logger) *TupleManager {
	return &TupleManager{
		client:               client,
		storeID:              storeID,
		authorizationModelID: authorizationModelID,
		logger:               *log.ComponentLogger("tuple_manager").MustChildLoggerWithAttributes("store_id", storeID, "authorization_model", authorizationModelID),
	}
}

// Apply writes a given set of tuples within a single transaction and ignores
// duplicate writes.
func (m *TupleManager) Apply(ctx context.Context, tuples []corev1alpha1.Tuple) error {
	if len(tuples) == 0 {
		return nil
	}

	tupleKeys := make([]*openfgav1.TupleKey, 0, len(tuples))
	for _, t := range tuples {
		tupleKeys = append(tupleKeys, &openfgav1.TupleKey{
			Object:   t.Object,
			Relation: t.Relation,
			User:     t.User,
		})
	}

	_, err := m.client.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              m.storeID,
		AuthorizationModelId: m.authorizationModelID,
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   tupleKeys,
			OnDuplicate: "ignore",
		},
	})
	if err != nil {
		metrics.FGAOperations.WithLabelValues("apply", "error").Inc()
		return err
	}

	m.logger.Debug().Int("count", len(tuples)).Msg("Ensured tuples")
	metrics.FGAOperations.WithLabelValues("apply", "success").Inc()
	return nil
}

// Delete deletes a given set of tuples within a single transaction and ignores
// duplicate deletions.
func (m *TupleManager) Delete(ctx context.Context, tuples []corev1alpha1.Tuple) error {
	if len(tuples) == 0 {
		return nil
	}

	tupleKeys := make([]*openfgav1.TupleKeyWithoutCondition, 0, len(tuples))
	for _, t := range tuples {
		tupleKeys = append(tupleKeys, &openfgav1.TupleKeyWithoutCondition{
			Object:   t.Object,
			Relation: t.Relation,
			User:     t.User,
		})
	}

	_, err := m.client.Write(ctx, &openfgav1.WriteRequest{
		StoreId:              m.storeID,
		AuthorizationModelId: m.authorizationModelID,
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: tupleKeys,
			OnMissing: "ignore",
		},
	})
	if err != nil {
		metrics.FGAOperations.WithLabelValues("delete", "error").Inc()
		return err
	}

	m.logger.Debug().Int("count", len(tuples)).Msg("Deleted tuples")
	metrics.FGAOperations.WithLabelValues("delete", "success").Inc()
	return nil
}

// ListWithFilter gets all tuples in the store and returns a list of all tuples
// that match the given filter.
func (m *TupleManager) ListWithFilter(ctx context.Context, filter TupleFilter) ([]corev1alpha1.Tuple, error) {
	if filter == nil {
		return nil, fmt.Errorf("filter function cannot be nil")
	}

	var result []corev1alpha1.Tuple
	var continuationToken string
	for {
		resp, err := m.client.Read(ctx, &openfgav1.ReadRequest{
			StoreId:           m.storeID,
			TupleKey:          nil, // nil returns all tuples
			ContinuationToken: continuationToken,
		})
		if err != nil {
			metrics.FGAOperations.WithLabelValues("list", "error").Inc()
			return nil, err
		}

		for _, t := range resp.Tuples {
			if t.Key == nil {
				continue
			}
			tuple := corev1alpha1.Tuple{
				Object:   t.Key.Object,
				Relation: t.Key.Relation,
				User:     t.Key.User,
			}
			if filter(tuple) {
				result = append(result, tuple)
			}
		}

		continuationToken = resp.ContinuationToken
		if continuationToken == "" {
			break
		}
	}

	metrics.FGAOperations.WithLabelValues("list", "success").Inc()
	return result, nil
}

// ListWithKey reads tuples from the store filtered by the given
// ReadRequestTupleKey.
func (m *TupleManager) ListWithKey(ctx context.Context, key *openfgav1.ReadRequestTupleKey) ([]corev1alpha1.Tuple, error) {
	var result []corev1alpha1.Tuple
	var continuationToken string
	for {
		resp, err := m.client.Read(ctx, &openfgav1.ReadRequest{
			StoreId:           m.storeID,
			TupleKey:          key,
			ContinuationToken: continuationToken,
		})
		if err != nil {
			metrics.FGAOperations.WithLabelValues("list", "error").Inc()
			return nil, err
		}

		for _, t := range resp.Tuples {
			if t.Key == nil {
				continue
			}
			result = append(result, corev1alpha1.Tuple{
				Object:   t.Key.Object,
				Relation: t.Key.Relation,
				User:     t.Key.User,
			})
		}

		continuationToken = resp.ContinuationToken
		if continuationToken == "" {
			break
		}
	}

	metrics.FGAOperations.WithLabelValues("list", "success").Inc()
	return result, nil
}
