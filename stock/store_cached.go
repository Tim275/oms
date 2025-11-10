package main

import (
	"context"
	"log"

	pb "github.com/timour/order-microservices/common/api"
)

// CachedStore wraps PostgresStore with Redis Cache-Aside pattern
type CachedStore struct {
	store *PostgresStore
	cache *ItemCache
}

// NewCachedStore creates a new cached store
func NewCachedStore(store *PostgresStore, cache *ItemCache) *CachedStore {
	return &CachedStore{
		store: store,
		cache: cache,
	}
}

// GetItem implements Cache-Aside pattern for single item retrieval
func (s *CachedStore) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	// 1. Check cache first
	cachedItem, err := s.cache.GetItem(ctx, id)
	if err != nil {
		log.Printf("⚠️  Cache error (will query DB): %v", err)
	} else if cachedItem != nil {
		log.Printf("🎯 Cache HIT: Item %s", id)
		return cachedItem, nil
	}

	log.Printf("❌ Cache MISS: Item %s - Querying PostgreSQL", id)

	// 2. Cache miss - query PostgreSQL
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Populate cache (best-effort, don't fail if cache write fails)
	if err := s.cache.SetItem(ctx, item); err != nil {
		log.Printf("⚠️  Failed to populate cache for item %s: %v", id, err)
	} else {
		log.Printf("💾 Cache populated: Item %s", id)
	}

	return item, nil
}

// GetItems implements Cache-Aside pattern for batch retrieval
// Wenn ids leer ist, werden ALLE Items zurückgegeben (no caching for "get all")
func (s *CachedStore) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	// If no IDs specified, bypass cache and return ALL items from DB
	if len(ids) == 0 {
		log.Printf("📋 GetItems: No IDs specified, fetching ALL items from DB (bypassing cache)")
		return s.store.GetItems(ctx, ids)
	}

	// 1. Try to get all items from cache using batch MGET
	cachedItems, err := s.cache.GetItems(ctx, ids)
	if err != nil {
		log.Printf("⚠️  Cache error (will query DB): %v", err)
		cachedItems = make(map[string]*pb.Item) // Treat as cache miss
	}

	// 2. Identify cache misses
	missedIDs := []string{}
	for _, id := range ids {
		if _, found := cachedItems[id]; !found {
			missedIDs = append(missedIDs, id)
		}
	}

	log.Printf("📊 Cache Stats: %d hits, %d misses (total: %d items)",
		len(cachedItems), len(missedIDs), len(ids))

	// 3. If all items are cached, return early
	if len(missedIDs) == 0 {
		log.Printf("🎯 Full cache HIT: All %d items from cache", len(ids))
		items := make([]*pb.Item, 0, len(ids))
		for _, id := range ids {
			items = append(items, cachedItems[id])
		}
		return items, nil
	}

	// 4. Query PostgreSQL for cache misses
	log.Printf("❌ Partial cache MISS: Querying PostgreSQL for %d items", len(missedIDs))
	dbItems, err := s.store.GetItems(ctx, missedIDs)
	if err != nil {
		return nil, err
	}

	// 5. Populate cache with items from DB (best-effort)
	for _, item := range dbItems {
		if err := s.cache.SetItem(ctx, item); err != nil {
			log.Printf("⚠️  Failed to populate cache for item %s: %v", item.ID, err)
		}
	}
	if len(dbItems) > 0 {
		log.Printf("💾 Cache populated: %d items", len(dbItems))
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
		log.Printf("⚠️  Failed to invalidate cache for item %s: %v", id, err)
	} else {
		log.Printf("🗑️  Cache invalidated: Item %s (quantity changed)", id)
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
