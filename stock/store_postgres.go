package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
	pb "github.com/timour/order-microservices/common/api"
)

// PostgresStore implementiert Store Interface mit PostgreSQL
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore erstellt eine neue PostgreSQL Store Instanz
func NewPostgresStore(connectionString string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

// Close schließt die Datenbankverbindung
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// GetItem ruft ein einzelnes Item aus der Datenbank ab
func (s *PostgresStore) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	var item pb.Item

	query := `SELECT id, name, price_id, quantity FROM items WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.Name,
		&item.PriceID,
		&item.Quantity,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	return &item, nil
}

// GetItems ruft mehrere Items aus der Datenbank ab
// Wenn ids leer ist, werden ALLE Items zurückgegeben
func (s *PostgresStore) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	var rows *sql.Rows
	var err error

	// If no IDs specified, return ALL items
	if len(ids) == 0 {
		query := `SELECT id, name, price_id, quantity FROM items ORDER BY id`
		rows, err = s.db.QueryContext(ctx, query)
	} else {
		// Build query with placeholders for specific IDs
		query := `SELECT id, name, price_id, quantity FROM items WHERE id = ANY($1)`
		rows, err = s.db.QueryContext(ctx, query, pq.Array(ids))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	var items []*pb.Item
	for rows.Next() {
		var item pb.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.PriceID, &item.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return items, nil
}

// UpdateQuantity aktualisiert die Quantity eines Items (für spätere Features)
func (s *PostgresStore) UpdateQuantity(ctx context.Context, id string, quantity int32) error {
	query := `UPDATE items SET quantity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	result, err := s.db.ExecContext(ctx, query, quantity, id)
	if err != nil {
		return fmt.Errorf("failed to update quantity: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("item not found")
	}

	return nil
}

// DecrementQuantity reduziert die Quantity eines Items (für Order Processing)
func (s *PostgresStore) DecrementQuantity(ctx context.Context, id string, amount int32) error {
	query := `
		UPDATE items
		SET quantity = quantity - $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND quantity >= $1
	`
	result, err := s.db.ExecContext(ctx, query, amount, id)
	if err != nil {
		return fmt.Errorf("failed to decrement quantity: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("insufficient stock or item not found")
	}

	return nil
}
