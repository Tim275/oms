package main

import (
	"context"
	"errors"

	"github.com/timour/order-microservices/common/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrOrderNotFound = errors.New("order not found")
)

type store struct {
	collection *mongo.Collection
}

func NewStore(client *mongo.Client) *store {
	// Database: "orders", Collection: "orders"
	collection := client.Database("orders").Collection("orders")
	return &store{
		collection: collection,
	}
}

func (s *store) Create(ctx context.Context, order *api.Order) (primitive.ObjectID, error) {
	// Let MongoDB generate unique _id - no custom "id" field!
	// This is the senior's approach for guaranteed uniqueness
	doc := bson.M{
		"customerID":   order.CustomerId,
		"status":       order.Status,
		"items":        order.Items,
		"paymentLink":  order.PaymentLink,
	}
	result, err := s.collection.InsertOne(ctx, doc)
	if err != nil {
		return primitive.NilObjectID, err
	}

	// Return the MongoDB-generated _id
	return result.InsertedID.(primitive.ObjectID), nil
}

func (s *store) Update(ctx context.Context, orderID string, order *api.Order) error {
	// Convert hex string to ObjectID - senior's approach
	oID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return err
	}

	// Build update document for non-empty fields
	update := bson.M{}

	if order.Status != "" {
		update["status"] = order.Status
	}
	if order.PaymentLink != "" {
		update["paymentLink"] = order.PaymentLink
	}

	if len(update) == 0 {
		return nil // Nothing to update
	}

	// Filter by _id (always unique!) instead of custom "id" field
	filter := bson.M{"_id": oID}
	result, err := s.collection.UpdateOne(ctx, filter, bson.M{"$set": update})
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrOrderNotFound
	}

	return nil
}

func (s *store) Get(ctx context.Context, orderID string) (*api.Order, error) {
	// Convert hex string to ObjectID
	oID, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return nil, err
	}

	// Decode into bson.M first to avoid protobuf field name mismatch
	var doc bson.M
	filter := bson.M{"_id": oID}
	err = s.collection.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}

	// Extract _id and convert to hex string
	var id string
	var createdAt string
	if oid, ok := doc["_id"].(primitive.ObjectID); ok {
		id = oid.Hex()
		createdAt = oid.Timestamp().Format("2006-01-02T15:04:05Z07:00")
	}

	// Manually map to protobuf struct with correct field names
	order := &api.Order{
		Id:          id,
		CustomerId:  getString(doc, "customerID"),
		Status:      getString(doc, "status"),
		PaymentLink: getString(doc, "paymentLink"),
		CreatedAt:   createdAt,
	}

	// Map items if present
	if itemsRaw, ok := doc["items"].(bson.A); ok {
		items := make([]*api.Item, 0, len(itemsRaw))
		for _, itemRaw := range itemsRaw {
			if itemDoc, ok := itemRaw.(bson.M); ok {
				items = append(items, &api.Item{
					ID:       getString(itemDoc, "id"),
					Name:     getString(itemDoc, "name"),
					Quantity: getInt32(itemDoc, "quantity"),
					PriceID:  getString(itemDoc, "priceid"),
				})
			}
		}
		order.Items = items
	}

	return order, nil
}

func (s *store) GetByStatus(ctx context.Context, status string) ([]*api.Order, error) {
	// Query MongoDB collection for all orders with given status
	filter := bson.M{"status": status}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	// Iterate through cursor and build order list
	var orders []*api.Order
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}

		// Extract _id and convert to hex string
		var id string
		var createdAt string
		if oid, ok := doc["_id"].(primitive.ObjectID); ok {
			id = oid.Hex()
			createdAt = oid.Timestamp().Format("2006-01-02T15:04:05Z07:00")
		}

		// Manually map to protobuf struct
		order := &api.Order{
			Id:          id,
			CustomerId:  getString(doc, "customerID"),
			Status:      getString(doc, "status"),
			PaymentLink: getString(doc, "paymentLink"),
			CreatedAt:   createdAt,
		}

		// Map items if present
		if itemsRaw, ok := doc["items"].(bson.A); ok {
			items := make([]*api.Item, 0, len(itemsRaw))
			for _, itemRaw := range itemsRaw {
				if itemDoc, ok := itemRaw.(bson.M); ok {
					items = append(items, &api.Item{
						ID:       getString(itemDoc, "id"),
						Name:     getString(itemDoc, "name"),
						Quantity: getInt32(itemDoc, "quantity"),
						PriceID:  getString(itemDoc, "priceid"),
					})
				}
			}
			order.Items = items
		}

		orders = append(orders, order)
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return orders, nil
}

// Helper functions for safe type conversion
func getString(m bson.M, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getInt32(m bson.M, key string) int32 {
	if val, ok := m[key].(int32); ok {
		return val
	}
	if val, ok := m[key].(int); ok {
		return int32(val)
	}
	return 0
}
