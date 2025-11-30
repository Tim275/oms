package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// OrderCounter - MongoDB-based atomic counter for Order Numbers
// Warum MongoDB statt Redis?
// → Wir nutzen bereits MongoDB für Orders
// → Atomic operations mit findAndModify
// → Persists across restarts
// → Works with multiple service instances
type OrderCounter struct {
	collection *mongo.Collection
}

// NewOrderCounter creates a new order counter
func NewOrderCounter(db *mongo.Database) *OrderCounter {
	return &OrderCounter{
		collection: db.Collection("counters"),
	}
}

// GetNextOrderNumber - Atomic increment und return
// Flow:
// 1. Find document with _id="order_counter"
// 2. Increment "seq" field by 1
// 3. Return NEW value
//
// ⭐ ATOMIC: Thread-safe, works with multiple instances!
// → Keine Race Conditions
// → Garantiert eindeutige Order Numbers
func (c *OrderCounter) GetNextOrderNumber(ctx context.Context) (int32, error) {
	// FindOneAndUpdate ist ATOMIC!
	// → Findet + Updated + Returned in einer Operation
	// → MongoDB garantiert: Kein anderer Thread kann dazwischen funken
	filter := bson.M{"_id": "order_counter"}
	update := bson.M{
		"$inc": bson.M{"seq": 1}, // Increment by 1
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).                          // Erstellt Document falls nicht existiert
		SetReturnDocument(options.After)          // Return UPDATED value (nicht old value!)

	var result struct {
		Seq int32 `bson:"seq"`
	}

	err := c.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&result)
	if err != nil {
		return 0, fmt.Errorf("failed to get next order number: %w", err)
	}

	return result.Seq, nil
}
