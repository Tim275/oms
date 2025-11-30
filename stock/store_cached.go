package main

import (
	"context"

	pb "github.com/timour/order-microservices/common/api"
	"go.uber.org/zap"
)

// CachedStore wraps PostgresStore with Redis Cache-Aside pattern
type CachedStore struct {
	store  *PostgresStore
	cache  *ItemCache
	logger *zap.Logger
}

// NewCachedStore creates a new cached store
func NewCachedStore(store *PostgresStore, cache *ItemCache, logger *zap.Logger) *CachedStore {
	return &CachedStore{
		store:  store,
		cache:  cache,
		logger: logger,
	}
}

// GetItem implements Cache-Aside pattern for single item retrieval
func (s *CachedStore) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	// 1. Check cache first
	cachedItem, err := s.cache.GetItem(ctx, id)
	if err != nil {
		s.logger.Warn("cache error (will query DB)", zap.Error(err), zap.String("item_id", id))
	} else if cachedItem != nil {
		s.logger.Info("cache HIT", zap.String("item_id", id))
		return cachedItem, nil
	}

	s.logger.Info("cache MISS - querying PostgreSQL", zap.String("item_id", id))

	// 2. Cache miss - query PostgreSQL
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Populate cache (best-effort, don't fail if cache write fails)
	if err := s.cache.SetItem(ctx, item); err != nil {
		s.logger.Warn("failed to populate cache", zap.String("item_id", id), zap.Error(err))
	} else {
		s.logger.Info("cache populated", zap.String("item_id", id))
	}

	return item, nil
}

// GetItems implements Cache-Aside pattern for batch retrieval
// Wenn ids leer ist, werden ALLE Items zurückgegeben (no caching for "get all")
func (s *CachedStore) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	// If no IDs specified, bypass cache and return ALL items from DB
	if len(ids) == 0 {
		s.logger.Info("GetItems: fetching ALL items from DB (bypassing cache)")
		return s.store.GetItems(ctx, ids)
	}

	// 1. Try to get all items from cache using batch MGET
	cachedItems, err := s.cache.GetItems(ctx, ids)
	if err != nil {
		s.logger.Warn("cache error (will query DB)", zap.Error(err))
		cachedItems = make(map[string]*pb.Item) // Treat as cache miss
	}

	// 2. Identify cache misses
	missedIDs := []string{}
	for _, id := range ids {
		if _, found := cachedItems[id]; !found {
			missedIDs = append(missedIDs, id)
		}
	}

	s.logger.Info("cache stats",
		zap.Int("hits", len(cachedItems)),
		zap.Int("misses", len(missedIDs)),
		zap.Int("total", len(ids)))

	// 3. If all items are cached, return early
	if len(missedIDs) == 0 {
		s.logger.Info("full cache HIT", zap.Int("count", len(ids)))
		items := make([]*pb.Item, 0, len(ids))
		for _, id := range ids {
			items = append(items, cachedItems[id])
		}
		return items, nil
	}

	// 4. Query PostgreSQL for cache misses
	s.logger.Info("partial cache MISS - querying PostgreSQL", zap.Int("count", len(missedIDs)))
	dbItems, err := s.store.GetItems(ctx, missedIDs)
	if err != nil {
		return nil, err
	}

	// 5. Populate cache with items from DB (best-effort)
	for _, item := range dbItems {
		if err := s.cache.SetItem(ctx, item); err != nil {
			s.logger.Warn("failed to populate cache", zap.String("item_id", item.ID), zap.Error(err))
		}
	}
	if len(dbItems) > 0 {
		s.logger.Info("cache populated", zap.Int("count", len(dbItems)))
	}

	// 6. Combine cached items + DB items
	allItems := make([]*pb.Item, 0, len(ids))
	for _, id := range ids {
		if cachedItem, found := cachedItems[id]; found {
			allItems = append(allItems, cachedItem)
		} else {
			// Find in dbItems
			for _, dbItem := range dbItems {
				if dbItem.ID == id {
					allItems = append(allItems, dbItem)
					break
				}
			}
		}
	}

	return allItems, nil
}

// DecrementQuantity updates PostgreSQL and invalidates cache
func (s *CachedStore) DecrementQuantity(ctx context.Context, id string, amount int32) error {
	// 1. Update PostgreSQL first
	if err := s.store.DecrementQuantity(ctx, id, amount); err != nil {
		return err
	}

	// 2. Invalidate cache entry (best-effort)
	if err := s.cache.InvalidateItem(ctx, id); err != nil {
		s.logger.Warn("failed to invalidate cache", zap.String("item_id", id), zap.Error(err))
	} else {
		s.logger.Info("cache invalidated (quantity changed)", zap.String("item_id", id))
	}

	return nil
}

// =========================================================
// Reservation Methods - Delegate to underlying store
// Reservations don't benefit from caching
// =========================================================

func (s *CachedStore) ReserveStock(ctx context.Context, orderID string, items []*pb.Item) (string, error) {
	return s.store.ReserveStock(ctx, orderID, items)
}

func (s *CachedStore) ConfirmReservation(ctx context.Context, orderID string) error {
	return s.store.ConfirmReservation(ctx, orderID)
}

func (s *CachedStore) ReleaseReservation(ctx context.Context, orderID string) error {
	return s.store.ReleaseReservation(ctx, orderID)
}
