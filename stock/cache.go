package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/redis/go-redis/v9"
)

// ItemCache implements Cache-Aside pattern for menu items
type ItemCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewItemCache creates a new Redis cache client
func NewItemCache(addr string, ttl time.Duration) (*ItemCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "", // no password
		DB:       0,  // default DB
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &ItemCache{
		client: client,
		ttl:    ttl,
	}, nil
}

// Close closes the Redis connection
func (c *ItemCache) Close() error {
	return c.client.Close()
}

// GetItem retrieves an item from cache
func (c *ItemCache) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	key := fmt.Sprintf("item:%s", id)

	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		// Cache miss
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	var item pb.Item
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal item: %w", err)
	}

	return &item, nil
}

// SetItem stores an item in cache
func (c *ItemCache) SetItem(ctx context.Context, item *pb.Item) error {
	key := fmt.Sprintf("item:%s", item.ID)

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %w", err)
	}

	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}

	return nil
}

// GetItems retrieves multiple items from cache
func (c *ItemCache) GetItems(ctx context.Context, ids []string) (map[string]*pb.Item, error) {
	if len(ids) == 0 {
		return make(map[string]*pb.Item), nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = fmt.Sprintf("item:%s", id)
	}

	results, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget error: %w", err)
	}

	items := make(map[string]*pb.Item)
	for i, result := range results {
		if result == nil {
			continue // Cache miss for this item
		}

		var item pb.Item
		data, ok := result.(string)
		if !ok {
			continue
		}

		if err := json.Unmarshal([]byte(data), &item); err != nil {
			continue
		}

		items[ids[i]] = &item
	}

	return items, nil
}

// InvalidateItem removes an item from cache
func (c *ItemCache) InvalidateItem(ctx context.Context, id string) error {
	key := fmt.Sprintf("item:%s", id)
	return c.client.Del(ctx, key).Err()
}
