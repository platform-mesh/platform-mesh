package fga

import (
	"context"
	"fmt"
	"time"

	"github.com/jellydator/ttlcache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/golang-commons/logger"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// StoreIDGetter should return the OpenFGA store ID for a store name.
type StoreIDGetter interface {
	Get(ctx context.Context, storeName string) (string, error)
}

// CachingStoreIDGetter maps store names to IDs by listing stores in OpenFGA but keeps
// a local cache to avoid frequent list calls.
type CachingStoreIDGetter struct {
	cache  *ttlcache.Cache[string, string]
	loader *storeIDLoader
	logger *logger.Logger
}

func NewCachingStoreIDGetter(fga openfgav1.OpenFGAServiceClient, ttl time.Duration, loadCtx context.Context, log *logger.Logger) *CachingStoreIDGetter {
	loader := &storeIDLoader{fga: fga, loadCtx: loadCtx}

	cache := ttlcache.New(
		ttlcache.WithTTL[string, string](ttl),
		ttlcache.WithLoader(loader),
	)
	cache.OnInsertion(func(_ context.Context, item *ttlcache.Item[string, string]) {
		log.Debug().
			Str("store", item.Key()).
			Str("id", item.Value()).
			Msg("StoreID cache inserted item")
	})
	cache.OnUpdate(func(_ context.Context, item *ttlcache.Item[string, string]) {
		log.Debug().
			Str("store", item.Key()).
			Str("id", item.Value()).
			Msg("StoreID cache updated item")
	})
	cache.OnEviction(func(_ context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, string]) {
		log.Debug().
			Str("store", item.Key()).
			Str("id", item.Value()).
			Str("reason", fmt.Sprint(reason)).
			Msg("StoreID cache evicted item")
	})

	return &CachingStoreIDGetter{
		cache:  cache,
		loader: loader,
		logger: log,
	}
}

// Get returns the store ID for the given store name.
func (m *CachingStoreIDGetter) Get(ctx context.Context, storeName string) (string, error) {
	item := m.cache.Get(storeName)
	if err := m.loader.Err(); err != nil {
		return "", fmt.Errorf("populating cache: %w", err)
	}

	if item != nil {
		return item.Value(), nil
	}

	return "", fmt.Errorf("store %q not found", storeName)
}

type storeIDLoader struct {
	fga       openfgav1.OpenFGAServiceClient
	loadErrer error
	loadCtx   context.Context
}

// Load lists all stores from OpenFGA, adds them to the cache, and returns the
// requested store's item or nil if not found. Caller is supposed to check
// Err(). Implements ttlcache.Loader.
func (l *storeIDLoader) Load(c *ttlcache.Cache[string, string], storeName string) *ttlcache.Item[string, string] {
	var continuationToken string
	var wantedItem *ttlcache.Item[string, string]

	for {
		resp, err := l.fga.ListStores(l.loadCtx, &openfgav1.ListStoresRequest{
			PageSize:          wrapperspb.Int32(100),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			l.loadErrer = fmt.Errorf("listing Stores in OpenFGA: %w", err)
			return nil
		}

		for _, store := range resp.GetStores() {
			if item := c.Set(store.GetName(), store.GetId(), ttlcache.DefaultTTL); item.Key() == storeName {
				wantedItem = item
			}
		}

		continuationToken = resp.GetContinuationToken()
		if continuationToken == "" {
			break
		}
	}

	return wantedItem
}

// Err returns the last error occured during Load. See [0] for why it works like
// this.
// [0] https://github.com/jellydator/ttlcache/issues/74#issuecomment-1133012806
func (l *storeIDLoader) Err() error {
	return l.loadErrer
}

var _ StoreIDGetter = (*CachingStoreIDGetter)(nil)
